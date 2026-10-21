import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI } from "@/lib/api";

const wsMocks = vi.hoisted(() => ({
  disconnect: vi.fn(),
  sendMessage: vi.fn(),
  markRead: vi.fn(),
}));

const authMocks = vi.hoisted(() => ({
  logout: vi.fn(),
  logoutFromAccount: vi.fn(),
}));

const serverConfigState = vi.hoisted(() => ({
  value: null as any,
}));

vi.mock("@/hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connect: vi.fn(),
    disconnect: wsMocks.disconnect,
    sendMessage: wsMocks.sendMessage,
    markRead: wsMocks.markRead,
    sendReaction: vi.fn(),
    sendMessageEdit: vi.fn(),
  }),
}));

vi.mock("@/hooks/useServerConfig", () => ({
  useServerConfig: () => serverConfigState.value,
}));

vi.mock("@/lib/sound", () => ({
  playSentSound: vi.fn(),
  playMessageSound: vi.fn(),
  playMentionSound: vi.fn(),
  playOnlineSound: vi.fn(),
  playOfflineSound: vi.fn(),
  playReactionSound: vi.fn(),
  isSoundEnabled: vi.fn(() => true),
  setSoundEnabled: vi.fn(),
}));

vi.mock("@/components/ThreadPanel", () => ({
  ThreadPanel: () => <div data-testid="thread-panel" />,
}));

vi.mock("@/lib/api", () => {
  const handlers: Map<string, Set<(msg: unknown) => void>> = new Map();
  return {
    chatAPI: {
      on: vi.fn((event: string, handler: (msg: unknown) => void) => {
        if (!handlers.has(event)) handlers.set(event, new Set());
        handlers.get(event)!.add(handler);
        return () => { handlers.get(event)?.delete(handler); };
      }),
      dispatch: (event: string, data: unknown) => {
        handlers.get(event)?.forEach((handler) => handler(data));
      },
      sendTypingStart: vi.fn(),
      sendTypingStop: vi.fn(),
      sendMessage: vi.fn(),
      sendMessageEdit: vi.fn(),
      sendThreadReply: vi.fn(),
      requestThreadMessages: vi.fn(),
      sendPinMessage: vi.fn(),
      deleteMessage: vi.fn(),
      exportChat: vi.fn().mockResolvedValue(new Blob(["[]"], { type: "application/json" })),
      fetchLinkPreview: vi.fn(),
      fetchServerConfig: vi.fn().mockResolvedValue(null),
      fetchMessagesBetween: vi.fn().mockResolvedValue([]),
    },
    logout: authMocks.logout,
    logoutFromAccount: authMocks.logoutFromAccount,
    ChatError: class ChatError extends Error {
      code: string;
      constructor(code: string, message: string) {
        super(message);
        this.code = code;
      }
    },
    ErrorCode: { TIMEOUT: "TIMEOUT", CLOSED: "CLOSED", CANNOT_CONNECT: "CANNOT_CONNECT" },
  };
});

const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value; },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
  };
})();
Object.defineProperty(window, "localStorage", { value: localStorageMock });

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
});

Object.defineProperty(window, "visualViewport", {
  writable: true,
  value: {
    height: 800,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  },
});

Element.prototype.scrollTo = vi.fn() as unknown as typeof Element.prototype.scrollTo;

import { ChatLayout } from "@/components/ChatLayout";

function renderChatLayout() {
  return render(
    <I18nProvider>
      <ChatLayout />
    </I18nProvider>,
  );
}

function typeAndSend(text: string) {
  const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
  fireEvent.change(textarea, { target: { value: text } });
  const sendButton = document.querySelector<HTMLButtonElement>("[data-visual='composer-send']");
  expect(sendButton).toBeTruthy();
  fireEvent.click(sendButton!);
}

describe("ChatLayout lightweight chat contract", () => {
  beforeEach(() => {
    localStorageMock.clear();
    localStorageMock.setItem("tokendance:lang", "zh-CN");
    wsMocks.disconnect.mockClear();
    wsMocks.sendMessage.mockClear();
    wsMocks.markRead.mockClear();
    authMocks.logout.mockReset();
    authMocks.logout.mockResolvedValue(undefined);
    authMocks.logoutFromAccount.mockReset();
    authMocks.logoutFromAccount.mockResolvedValue(
      "https://account.tokendancelab.com/oauth/end-session?client_id=chat",
    );
    serverConfigState.value = null;
    useChatStore.setState({
      username: "testuser",
      connected: true,
      onlineUsers: ["testuser", "alice"],
      currentChat: { type: "public" },
      messages: [],
      unreadByConversation: {},
      userStatusList: [],
      userProfiles: {},
      pinnedMessages: [],
    });
  });

  it("renders only public room plus TokenBot entry", () => {
    renderChatLayout();

    expect(screen.getAllByText("公共聊天").length).toBeGreaterThan(0);
    expect(screen.getByText("TokenBot")).toBeTruthy();

    expect(screen.queryByText("好友")).toBeNull();
    expect(screen.queryByText("群组")).toBeNull();
    expect(screen.queryByText("私信")).toBeNull();
    expect(screen.queryByText("接受")).toBeNull();
    expect(screen.queryByText("拒绝")).toBeNull();
    expect(screen.queryByLabelText("语音通话")).toBeNull();
    expect(screen.queryByLabelText("视频通话")).toBeNull();
    expect(screen.queryByTestId("video-call")).toBeNull();
  });

  it("coerces stale DM/group state back to public chat", () => {
    useChatStore.setState({ currentChat: { type: "dm", username: "alice" } as any });
    renderChatLayout();

    expect(screen.getAllByText("公共聊天").length).toBeGreaterThan(0);
    expect(useChatStore.getState().currentChat).toEqual({ type: "public" });
  });

  it("opens the public room as a timeline without AI onboarding", () => {
    useChatStore.setState({ historyLoaded: true });
    renderChatLayout();
    expect(document.querySelector("[data-visual='room-timeline']")).toBeTruthy();
    expect(screen.getByText("暂无消息")).toBeTruthy();
    expect(screen.queryByText("今天想聊点什么？")).toBeNull();
    expect(screen.queryByRole("button", { name: "你能做什么？" })).toBeNull();
    const dock = document.querySelector("[data-visual='room-composer-dock']");
    expect(dock).toBeTruthy();
    expect(screen.getByRole("log").contains(dock)).toBe(false);
  });

  it("selects TokenBot context and sends a private 1:1 message", () => {
    renderChatLayout();
    fireEvent.click(screen.getByText("TokenBot"));

    expect(screen.getAllByText("TokenBot").length).toBeGreaterThan(0);
    const contextChip = document.querySelector("[data-visual='composer-ai-context']");
    expect(contextChip).toBeTruthy();
    expect(contextChip?.textContent).toContain("TokenBot");
    expect(document.querySelector("[data-visual='ai-chat-workbench']")).toBeNull();

    typeAndSend("总结一下");

    // Private channel: message is addressed to the bot (to: TokenBot), never
    // prefixed with @mention or broadcast to the public room.
    expect(wsMocks.sendMessage).toHaveBeenCalledWith("总结一下", "TokenBot");
  });

  it("keeps plain public sends unprefixed", () => {
    renderChatLayout();

    typeAndSend("大家好");

    expect(wsMocks.sendMessage).toHaveBeenCalledWith("大家好");
  });

  it("keeps utility menu without IM actions", () => {
    renderChatLayout();
    fireEvent.click(screen.getByLabelText("更多"));

    expect(screen.getByText("English")).toBeTruthy();
    expect(screen.getByText("导出为 JSON")).toBeTruthy();
    expect(screen.getByText("导出为文本")).toBeTruthy();
    expect(screen.queryByText("打开设置")).toBeNull();
    expect(screen.queryByText("离开聊天")).toBeNull();
    expect(screen.queryByText("创建群组")).toBeNull();
    expect(screen.queryByText("添加好友")).toBeNull();
  });

  it("keeps account actions in the sidebar account menu", () => {
    renderChatLayout();
    const trigger = screen.getByLabelText("账户菜单");
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(trigger);

    expect(screen.getByRole("menuitem", { name: "打开设置" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "离开聊天" })).toBeTruthy();
  });

  it("keeps leaving Chat separate from signing out of Account", async () => {
    serverConfigState.value = { oidc_enabled: true };
    renderChatLayout();
    const trigger = screen.getByLabelText("账户菜单");
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(trigger);

    expect(screen.getByRole("menuitem", { name: "离开聊天" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "退出 TokenDance Account" })).toBeTruthy();

    fireEvent.click(screen.getByRole("menuitem", { name: "离开聊天" }));
    await waitFor(() => expect(authMocks.logout).toHaveBeenCalledOnce());
    await waitFor(() => expect(wsMocks.disconnect).toHaveBeenCalled());
    expect(authMocks.logoutFromAccount).not.toHaveBeenCalled();

    authMocks.logout.mockClear();
    wsMocks.disconnect.mockClear();
    authMocks.logoutFromAccount.mockRejectedValueOnce(new Error("test navigation stop"));
    act(() => {
      useChatStore.setState({ username: "testuser", connected: true, isGuest: false });
    });
    await waitFor(() => expect(trigger.getAttribute("data-state")).toBe("closed"));
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("menuitem", { name: "退出 TokenDance Account" }));
    await waitFor(() => expect(authMocks.logoutFromAccount).toHaveBeenCalledOnce());
    expect(authMocks.logout).not.toHaveBeenCalled();
  });

  it("uses the signed-in Account profile for the current account identity", () => {
    useChatStore.setState({
      userProfiles: {
        testuser: {
          username: "testuser",
          display_name: "Rina Chen",
          avatar_url: "https://img.tokendancelab.com/avatars/rina/256.webp",
          bio: "",
          status: "",
          last_seen: Date.now(),
          created_at: 0,
        },
      },
    });

    renderChatLayout();

    expect(screen.getByText("Rina Chen")).toBeTruthy();
    expect(screen.getByRole("img", { name: "Rina Chen" })).toHaveAttribute(
      "src",
      "https://img.tokendancelab.com/avatars/rina/256.webp",
    );
  });

  it("shows reconnect state in the compact header", async () => {
    useChatStore.setState({ connected: false });
    renderChatLayout();

    await act(async () => {
      (chatAPI as unknown as { dispatch: (event: string, data: unknown) => void }).dispatch("reconnecting", {
        type: "reconnecting",
        attempt: 1,
      });
    });

    expect(screen.getAllByTitle("正在重新连接 (第 2 次)...").length).toBeGreaterThan(0);
  });
});
