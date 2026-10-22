import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { SettingsPanel } from "@/components/SettingsPanel";
import { mockI18n } from "@/test-utils";

const { mockSendSetNotificationPrefs, mockSendUnblock, storeState } = vi.hoisted(() => ({
  mockSendSetNotificationPrefs: vi.fn(),
  mockSendUnblock: vi.fn(),
  storeState: {
    notificationPrefs: {} as Record<string, { mutedUntil: number; showPreview: boolean }>,
    mutedConversations: [] as string[],
    blockedUsers: [] as string[],
    removeBlockedUser: vi.fn(),
    username: "alice",
    connected: true,
    onlineUsers: [] as string[],
    userProfiles: {} as Record<string, { display_name?: string }>,
  },
}));

vi.mock("@/i18n/context", () => ({
  useTranslation: () => mockI18n({
    "settings.notificationPrefs": "通知偏好设置",
    "settings.mutedConversations": "已静音的对话",
    "settings.noMutedConversations": "暂无静音的对话",
    "settings.showPreview": "消息预览",
    "settings.previewOn": "预览已开启",
    "settings.previewOff": "预览已关闭",
    "settings.unmute": "取消静音",
    "chat.publicChat": "公共聊天",
    "settings.openSettings": "设置",
    "settings.title": "设置",
    "settings.accountSection": "账号",
    "settings.accountGuest": "游客模式",
    "settings.accountMember": "已登录",
    "settings.language": "语言",
    "settings.appearance": "外观",
    "settings.themeLight": "浅色",
    "settings.themeDark": "深色",
    "settings.themeSystem": "跟随系统",
    "settings.assistantSection": "助手",
    "settings.showReasoning": "显示思考过程",
    "settings.webSearchStatus": "网页搜索",
    "settings.capabilityOn": "已启用",
    "settings.capabilityOff": "未启用",
    "settings.notifications": "通知",
    "settings.privateNotificationHint": "仅提示有新回复，不显示私聊正文",
    "settings.about": "关于",
    "settings.blockedUsers": "已屏蔽用户",
    "settings.unblock": "解除屏蔽",
  }),
}));

vi.mock("@/stores/chatStore", () => ({
  useChatStore: vi.fn((selector?: (s: unknown) => unknown) => {
    return selector ? selector(storeState) : storeState;
  }),
}));

vi.mock("@/lib/api", () => ({
  chatAPI: {
    sendSetNotificationPrefs: mockSendSetNotificationPrefs,
    sendUnblock: mockSendUnblock,
  },
}));

vi.mock("@/hooks/useServerConfig", () => ({
  useServerConfig: () => ({ model: "deepseek-v4.1-flash", capabilities: { web_search: true } }),
}));

describe("SettingsPanel", () => {
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    storeState.notificationPrefs = {};
    storeState.mutedConversations = [];
    storeState.blockedUsers = [];
    storeState.username = "alice";
    storeState.onlineUsers = [];
    storeState.userProfiles = {};
  });

  it("renders notification preferences header", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.getByText("设置")).toBeTruthy();
  });

  it("renders close button and calls onClose when clicked", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    // Find the X button in the header
    const buttons = screen.getAllByRole("button");
    const closeBtn = buttons.find((b) => b.querySelector(".lucide-x"));
    expect(closeBtn).toBeTruthy();
    fireEvent.click(closeBtn!);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("renders the assistant settings section", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.getByText("助手")).toBeTruthy();
    expect(screen.getByText("显示思考过程")).toBeTruthy();
  });

  it("shows empty state when no muted conversations", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.getByText("暂无静音的对话")).toBeTruthy();
  });

  it("hides show preview section header when no preferences are set", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.queryByText("消息预览")).toBeNull();
  });

  it("renders show preview section header when a preference is set", () => {
    storeState.notificationPrefs = { public: { mutedUntil: 0, showPreview: true } };
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.getByText("消息预览")).toBeTruthy();
  });

  it("renders a modal overlay and closes on Escape", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    const backdrop = document.querySelector("[data-slot='sheet-overlay']");
    expect(backdrop).toBeTruthy();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("renders muted conversations with unmute button", () => {
    storeState.mutedConversations = ["public"];
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    // Public chat should appear as muted
    expect(screen.getByText("公共聊天")).toBeTruthy();
    // Should have unmute button
    const unmuteButtons = screen.getAllByTitle("取消静音");
    expect(unmuteButtons.length).toBeGreaterThan(0);
  });

  it("clicking unmute button calls sendSetNotificationPrefs", () => {
    storeState.mutedConversations = ["public"];
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    const unmuteBtn = screen.getByTitle("取消静音");
    fireEvent.click(unmuteBtn);
    expect(mockSendSetNotificationPrefs).toHaveBeenCalledWith("public", 0, true);
  });

  it("lists blocked users and unblocks them", () => {
    storeState.blockedUsers = ["spammer"];
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    expect(screen.getByText("已屏蔽用户")).toBeTruthy();
    expect(screen.getByText("spammer")).toBeTruthy();
    fireEvent.click(screen.getByText("解除屏蔽"));
    expect(mockSendUnblock).toHaveBeenCalledWith("spammer");
    expect(storeState.removeBlockedUser).toHaveBeenCalledWith("spammer");
  });

  it("mutes private assistant notifications without enabling message previews", () => {
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    const toggle = screen.getByRole("switch", { name: "TokenBot 通知" });
    expect(toggle).toHaveAttribute("aria-checked", "true");
    fireEvent.click(toggle);
    expect(mockSendSetNotificationPrefs).toHaveBeenCalledWith("dm-TokenBot", Number.MAX_SAFE_INTEGER, false);
    expect(screen.getByText("仅提示有新回复，不显示私聊正文")).toBeTruthy();
  });

  it("restores private notifications while keeping previews disabled", () => {
    storeState.notificationPrefs = { "dm-TokenBot": { mutedUntil: Number.MAX_SAFE_INTEGER, showPreview: true } };
    render(<SettingsPanel onClose={onClose} theme="light" onThemeChange={vi.fn()} lang="zh-CN" onLangChange={vi.fn()} />);
    const toggle = screen.getByRole("switch", { name: "TokenBot 通知" });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByText("暂无静音的对话")).toBeNull();
    fireEvent.click(toggle);
    expect(mockSendSetNotificationPrefs).toHaveBeenCalledWith("dm-TokenBot", 0, false);
  });
});
