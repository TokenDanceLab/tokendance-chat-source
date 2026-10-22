import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ChatInfoRail } from "@/components/ChatInfoRail";
import { assistants } from "@/lib/assistantRegistry";
import { mockI18n } from "@/test-utils";

const { storeState } = vi.hoisted(() => ({
  storeState: {
    username: "alice",
    onlineUsers: [] as string[],
    userProfiles: {} as Record<string, { display_name?: string; avatar_url?: string }>,
    userStatusList: [] as Array<{ username: string; status: string }>,
    botRuns: {} as Record<string, Record<string, unknown>>,
    messages: [] as Array<Record<string, unknown>>,
    privateBotMessages: [] as Array<Record<string, unknown>>,
  },
}));

vi.mock("@/i18n/context", () => ({
  useTranslation: () =>
    mockI18n({
      "info.title": "对话信息",
      "info.publicTitle": "群聊信息",
      "info.members": "成员",
      "info.activity": "动态",
      "info.agent": "助手",
      "info.recentSources": "来源",
      "info.thread": "对话",
      "info.noMembers": "暂时只有你在线",
      "info.noActivity": "还没有可显示的活动",
      "info.today": "今天",
      "info.yesterday": "昨天",
      "info.noSources": "本段对话还没有引用来源",
      "info.running": "正在回复",
      "info.replyingYou": "正在回复你的请求",
      "info.replyingTo": "正在回复 {{name}}",
      "info.messages": "消息",
      "info.threadHint": "统计本次私聊的消息数",
      "info.webSearch": "可搜索网页",
      "info.ambient": "主动参与",
      "info.timeTools": "查询时间",
      "info.memory": "对话记忆",
      "botRun.completed": "已完成",
      "botRun.failed": "失败",
      "botRun.cancelled": "已停止",
      "a11y.closeInfo": "关闭对话信息",
    }),
}));

vi.mock("@/stores/chatStore", () => ({
  useChatStore: vi.fn((selector?: (s: unknown) => unknown) =>
    selector ? selector(storeState) : storeState,
  ),
}));

vi.mock("@/hooks/useServerConfig", () => ({
  useServerConfig: () => ({
    model: "deepseek-v4.1-flash",
    capabilities: { web_search: true, ambient: true },
  }),
}));

const tokenBot = assistants[0];

const sourcePart = {
  type: "source",
  source_id: "s1",
  title: "Example result",
  url: "https://example.com/page",
  status: "fetched",
  cited: true,
};

const privateMessages = [
  { id: "m1", username: "alice", content: "hi", timestamp: 1 },
  { id: "m2", username: tokenBot.name, content: "hello", timestamp: 2, parts: [sourcePart] },
];

const botRuns = {
  run1: {
    run_id: "run1",
    private: true,
    status: "completed",
    requested_by: "alice",
    trigger_message_id: "m1",
    timestamp: 3,
  },
  run2: {
    run_id: "run2",
    private: false,
    status: "running",
    requested_by: "bob",
    timestamp: 4,
  },
};

describe("ChatInfoRail", () => {
  beforeEach(() => {
    storeState.username = "alice";
    storeState.onlineUsers = ["alice", "bob", tokenBot.name];
    storeState.userProfiles = { bob: { display_name: "Bob" } };
    storeState.userStatusList = [];
    storeState.botRuns = botRuns;
    storeState.messages = [];
    storeState.privateBotMessages = privateMessages;
  });

  it("defaults to the member tab in a public room and lists the agent and humans", () => {
    render(
      <ChatInfoRail privateChat={false} assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    expect(screen.getByText("群聊信息")).toBeTruthy();
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((tab) => tab.textContent)).toEqual(["成员", "动态", "助手"]);
    expect(tabs[0].getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("Bob")).toBeTruthy();
    expect(screen.getByText(tokenBot.name)).toBeTruthy();
  });

  it("uses a different tab set in a private chat", () => {
    render(
      <ChatInfoRail privateChat assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    expect(screen.getByText("对话信息")).toBeTruthy();
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((tab) => tab.textContent)).toEqual(["动态", "来源", "对话"]);
    expect(tabs[0].getAttribute("aria-selected")).toBe("true");
    expect(screen.queryByText("Bob")).toBeNull();
    // Only the requester's own private runs are listed, not public runs.
    expect(screen.getByText("已完成")).toBeTruthy();
    expect(screen.queryByText("正在回复")).toBeNull();
  });

  it("groups recent activity by day", () => {
    const now = Date.now();
    storeState.botRuns = {
      today: {
        run_id: "today",
        private: true,
        status: "completed",
        requested_by: "alice",
        trigger_message_id: "missing-after-reconnect",
        trigger_preview: "快照里的请求",
        timestamp: now,
      },
      yesterday: {
        run_id: "yesterday",
        private: true,
        status: "completed",
        requested_by: "alice",
        trigger_message_id: "m1",
        timestamp: now - 24 * 60 * 60 * 1000,
      },
    };
    render(
      <ChatInfoRail privateChat assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    expect(screen.getByText("今天")).toBeTruthy();
    expect(screen.getByText("昨天")).toBeTruthy();
    expect(screen.getByText("快照里的请求")).toBeTruthy();
    expect(screen.getAllByText("已完成")).toHaveLength(2);
  });

  it("falls back to the first tab when the rail switches mode while mounted", () => {
    const { rerender } = render(
      <ChatInfoRail privateChat={false} assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    // The member tab is selected; its key does not exist in private mode.
    expect(screen.getByText("Bob")).toBeTruthy();
    rerender(
      <ChatInfoRail privateChat assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    const tabs = screen.getAllByRole("tab");
    expect(tabs[0].textContent).toBe("动态");
    expect(tabs[0].getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("已完成")).toBeTruthy();
  });

  it("uses roving keyboard focus and exposes tab panels", () => {
    render(
      <ChatInfoRail privateChat={false} assistant={tokenBot} modelLabel="m" onClose={vi.fn()} />,
    );
    const tabs = screen.getAllByRole("tab");
    expect(tabs[0].getAttribute("tabindex")).toBe("0");
    expect(tabs[1].getAttribute("tabindex")).toBe("-1");
    expect(tabs[0].getAttribute("aria-controls")).toBe(
      screen.getByRole("tabpanel").getAttribute("id"),
    );

    fireEvent.keyDown(tabs[0], { key: "ArrowRight" });

    expect(tabs[1].getAttribute("aria-selected")).toBe("true");
    expect(tabs[1].getAttribute("tabindex")).toBe("0");
    expect(document.activeElement).toBe(tabs[1]);
    expect(screen.getByRole("tabpanel").getAttribute("aria-labelledby")).toBe(tabs[1].id);
  });
});
