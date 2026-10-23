import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI, type ChatMessage } from "@/lib/api";

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
  ThreadPanel: ({ parentMessage, threadMessages, onSendReply, onClose }: {
    parentMessage: ChatMessage | null;
    threadMessages: ChatMessage[];
    onSendReply: (content: string) => void;
    onClose: () => void;
  }) => (
    <div data-testid="thread-panel">
      <span data-testid="thread-parent">{parentMessage?.id}</span>
      <div data-testid="thread-replies">
        {threadMessages.map((message) => <p key={message.id}>{message.content}</p>)}
      </div>
      <button onClick={() => onSendReply("reply from thread panel")}>Send thread reply</button>
      <button onClick={onClose}>Close thread</button>
    </div>
  ),
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
      sendLoadPrivateHistory: vi.fn(),
      sendListPrivateThreads: vi.fn(),
      cancelBotRun: vi.fn().mockReturnValue(true),
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

function threadFixture(parentId: string, privateConversation = false): ChatMessage[] {
  const timestamp = Date.now() - 60_000;
  return [
    { id: parentId, username: privateConversation ? "TokenBot" : "alice", content: `${parentId} parent`, timestamp,
      ...(privateConversation ? { to: "testuser" } : { room_id: "room-1" }) },
    { id: `${parentId}-reply`, username: "testuser", content: `${parentId} reply`, timestamp: timestamp + 1,
      thread_id: parentId, ...(privateConversation ? { to: "TokenBot" } : { room_id: "room-1" }) },
  ];
}

async function openThread(parentId: string) {
  const button = await waitFor(() => {
    const element = document.getElementById(`msg-${parentId}`)?.querySelector("[data-visual='message-bubble-thread']");
    expect(element).toBeTruthy();
    return element!;
  });
  fireEvent.click(button);
  await screen.findByTestId("thread-panel");
  expect(screen.getByTestId("thread-parent")).toHaveTextContent(parentId);
  expect(chatAPI.requestThreadMessages).toHaveBeenLastCalledWith(parentId);
}

function receiveThread(parentId: string, content: string) {
  act(() => {
    (chatAPI as unknown as { dispatch: (event: string, data: unknown) => void }).dispatch("thread_messages", {
      type: "thread_messages",
      parent_message_id: parentId,
      messages: [{ id: `${parentId}-response`, username: "alice", content, timestamp: Date.now(), thread_id: parentId }],
    });
  });
}

describe("ChatLayout lightweight chat contract", () => {
  beforeEach(() => {
    localStorageMock.clear();
    localStorageMock.setItem("tokendance:lang", "zh-CN");
    wsMocks.disconnect.mockClear();
    wsMocks.sendMessage.mockClear();
    wsMocks.markRead.mockClear();
    vi.mocked(chatAPI.sendThreadReply).mockClear();
    vi.mocked(chatAPI.requestThreadMessages).mockClear();
    vi.mocked(chatAPI.cancelBotRun).mockClear();
    vi.mocked(chatAPI.sendLoadPrivateHistory).mockClear();
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
      privateBotMessages: [],
      isGuest: false,
      replyTo: null,
      unreadByConversation: {},
      userStatusList: [],
      userProfiles: {},
      pinnedMessages: [],
      botRuns: {},
      closedBotRuns: {},
      typingUsers: [],
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

  it("shows the assistant icon in @ candidates and inserts the canonical human handle", () => {
    useChatStore.setState({ userProfiles: {
      alice: { username: "alice", display_name: "Alice Example", avatar_url: "https://example.com/alice.png", bio: "", status: "online", last_seen: 1, created_at: 1 },
    } });
    const imageComplete = vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
    const imageWidth = vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(24);
    try {
      renderChatLayout();
      const input = screen.getByRole("textbox");
      fireEvent.change(input, { target: { value: "@", selectionStart: 1 } });
      const assistant = screen.getByRole("option", { name: "TokenBot" });
      expect(screen.queryByRole("option", { name: "testuser" })).toBeNull();
      expect(assistant.querySelector("svg")).toBeTruthy();
      expect(assistant).not.toHaveTextContent(/^TTokenBot$/);
      const human = screen.getByRole("option", { name: "Alice Example" });
      expect(human.querySelector('img')).toHaveAttribute("src", "https://example.com/alice.png");
      expect(human.querySelector('[data-slot="avatar-fallback"]')).toBeNull();
      fireEvent.click(human);
      expect(input).toHaveValue("@alice ");
    } finally { imageComplete.mockRestore(); imageWidth.mockRestore(); }
  });

  it("uses one public composer stop, prioritizes the viewer's running request and waits for acknowledgement", () => {
    serverConfigState.value = { capabilities: { run_control: true } };
    const run = { type: "bot_run" as const, run_id: "queued-own", message_id: "queued-message", stream_id: "queued-own", trigger_message_id: "question", requested_by: "testuser", private: false, status: "queued" as const };
    useChatStore.getState().receiveBotRun(run);
    useChatStore.getState().receiveBotRun({ ...run, run_id: "peer", message_id: "peer-message", requested_by: "alice", status: "running" });
    useChatStore.getState().receiveBotRun({ ...run, run_id: "running-own", message_id: "running-message", status: "running" });
    renderChatLayout();
    const stop = document.querySelector<HTMLButtonElement>('[data-visual="composer-stop"]')!;
    expect(stop).toBeEnabled();
    expect(document.querySelector('[data-visual="bot-run-strip"]')).toBeNull();
    fireEvent.click(stop);
    expect(chatAPI.cancelBotRun).toHaveBeenCalledExactlyOnceWith("running-own");
    expect(stop).toBeDisabled();
    fireEvent.click(stop);
    expect(chatAPI.cancelBotRun).toHaveBeenCalledTimes(1);
    act(() => useChatStore.getState().receiveBotRun({ ...run, run_id: "running-own", message_id: "running-message", status: "cancelled" }));
    expect(stop).toBeEnabled();
    fireEvent.click(stop);
    expect(chatAPI.cancelBotRun).toHaveBeenLastCalledWith("queued-own");
  });

  it("keeps public Enter sends working while the viewer has an assistant request in progress", async () => {
    serverConfigState.value = { capabilities: { run_control: true } };
    useChatStore.getState().receiveBotRun({ type: "bot_run", run_id: "own", message_id: "own-message", stream_id: "own", trigger_message_id: "question", requested_by: "testuser", private: false, status: "running" });
    renderChatLayout();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "ordinary public message" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
    await waitFor(() => expect(wsMocks.sendMessage).toHaveBeenCalledWith("ordinary public message"));
    expect(document.querySelector('[data-visual="composer-stop"]')).toBeTruthy();
  });

  it("omits the guest's canonical handle from @ suggestions even with a custom display name", () => {
    useChatStore.setState({ username: "guest_self", isGuest: true, onlineUsers: ["guest_self", "alice"], userProfiles: {
      guest_self: { username: "guest_self", display_name: "My guest label", avatar_url: "", bio: "", status: "online", last_seen: 1, created_at: 1 },
    } });
    renderChatLayout();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "@", selectionStart: 1 } });
    expect(screen.queryByRole("option", { name: "My guest label" })).toBeNull();
    expect(screen.getByRole("option", { name: "alice" })).toBeVisible();
  });

  it("shares the guest graphic across mention, message and member rail", () => {
    const original = window.matchMedia;
    window.matchMedia = vi.fn((query) => ({ ...original(query), matches: query === "(min-width: 1368px)" }));
    useChatStore.setState({ onlineUsers: ["testuser", "guest_peer"], messages: [
      { id: "guest-msg", username: "guest_peer", content: "Guest message", timestamp: 1 },
    ] });
    try {
      const { container } = renderChatLayout();
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "@", selectionStart: 1 } });
      const graphic = screen.getByRole("option", { name: "guest_peer" }).querySelector('[data-visual="guest-avatar"]')!;
      const message = container.querySelector('[role="log"] [data-visual="guest-avatar"]')!;
      const member = container.querySelector('[data-visual="chat-info-rail"] [data-visual="guest-avatar"]')!;
      expect(message.innerHTML).toBe(graphic.innerHTML);
      expect(member.innerHTML).toBe(graphic.innerHTML);
    } finally { window.matchMedia = original; }
  });

  it("replaces a fresh mention range before an earlier selection's animation frame can move the cursor", () => {
    const frames: FrameRequestCallback[] = [];
    const animationFrame = vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => { frames.push(callback); return frames.length; });
    try {
      renderChatLayout();
      const input = screen.getByRole("textbox") as HTMLTextAreaElement;
      fireEvent.change(input, { target: { value: "@", selectionStart: 1 } });
      fireEvent.keyDown(input, { key: "ArrowDown" });
      fireEvent.keyDown(input, { key: "Enter" });
      expect(input).toHaveValue("@TokenBot ");
      fireEvent.change(input, { target: { value: "@a", selectionStart: 2 } });
      fireEvent.click(screen.getByRole("option", { name: "alice" }));
      expect(input).toHaveValue("@alice ");
      fireEvent.change(input, { target: { value: "newer input", selectionStart: 2 } });
      act(() => frames.forEach((frame) => frame(0)));
      expect(input).toHaveValue("newer input");
      expect(input.selectionStart).toBe(2);
      expect(wsMocks.sendMessage).not.toHaveBeenCalled();
    } finally { animationFrame.mockRestore(); }
  });

  it("clears stop on disconnection and never stops another public viewer's run", () => {
    serverConfigState.value = { capabilities: { run_control: true } };
    const run = { type: "bot_run" as const, run_id: "run", message_id: "run-message", stream_id: "run", trigger_message_id: "question", requested_by: "alice", private: false, status: "running" as const };
    useChatStore.getState().receiveBotRun(run);
    renderChatLayout();
    expect(document.querySelector('[data-visual="composer-stop"]')).toBeNull();
    act(() => {
      useChatStore.getState().receiveBotRun({ ...run, run_id: "own", message_id: "own-message", requested_by: "testuser" });
      useChatStore.getState().setConnected(false);
    });
    expect(document.querySelector('[data-visual="composer-stop"]')).toBeNull();
    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(chatAPI.cancelBotRun).not.toHaveBeenCalled();
  });

  it("does not expose composer stop when the server lacks run control", () => {
    useChatStore.getState().receiveBotRun({ type: "bot_run", run_id: "run", message_id: "run-message", stream_id: "run", trigger_message_id: "question", requested_by: "testuser", private: false, status: "running" });
    renderChatLayout();
    expect(document.querySelector('[data-visual="composer-stop"]')).toBeNull();
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

  it("retries a failed private send inside the private conversation", async () => {
    useChatStore.setState({
      privateBotMessages: [{
        id: "optimistic_retry",
        client_message_id: "optimistic_retry",
        username: "testuser",
        content: "重试一下",
        timestamp: Date.now(),
        private: true,
        failed: true,
        failureReason: "rate limited",
      }],
    });
    renderChatLayout();
    fireEvent.click(screen.getByText("TokenBot"));

    fireEvent.click(await screen.findByRole("button", { name: "重试" }));

    expect(wsMocks.sendMessage).toHaveBeenCalledWith("重试一下", "TokenBot");
    expect(useChatStore.getState().privateBotMessages.some((m) => m.id === "optimistic_retry")).toBe(false);
  });

  it.each([false, true])("routes thread replies to their public/private conversation (private=%s)", async (privateConversation) => {
    const parentId = privateConversation ? "private-parent" : "public-parent";
    const messages = threadFixture(parentId, privateConversation);
    if (privateConversation) {
      // Private history arrives over the WS (load_private → private_history);
      // the store is the boundary the component actually reads.
      useChatStore.setState({ privateBotMessages: messages });
    } else {
      useChatStore.setState({ messages, historyLoaded: true });
    }
    renderChatLayout();
    if (privateConversation) fireEvent.click(screen.getByText("TokenBot"));

    await openThread(parentId);
    fireEvent.click(screen.getByRole("button", { name: "Send thread reply" }));

    expect(chatAPI.sendThreadReply).toHaveBeenCalledExactlyOnceWith(
      parentId, "reply from thread panel", privateConversation ? "TokenBot" : undefined,
    );
  });

  it("clears the thread panel and replies when switching between public and private conversations", async () => {
    useChatStore.setState({ messages: threadFixture("public-parent"), historyLoaded: true });
    useChatStore.setState({ privateBotMessages: threadFixture("private-parent", true) });
    renderChatLayout();
    await openThread("public-parent");
    receiveThread("public-parent", "public thread detail");
    expect(screen.getByTestId("thread-replies")).toHaveTextContent("public thread detail");

    fireEvent.click(screen.getByText("TokenBot"));
    expect(screen.queryByTestId("thread-panel")).toBeNull();
    receiveThread("public-parent", "late public reply");
    expect(screen.queryByTestId("thread-panel")).toBeNull();
    await openThread("private-parent");
    expect(screen.getByTestId("thread-replies")).toBeEmptyDOMElement();
    receiveThread("private-parent", "private thread detail");
    expect(screen.getByTestId("thread-replies")).toHaveTextContent("private thread detail");

    fireEvent.click(screen.getByRole("button", { name: /公共聊天/ }));
    expect(screen.queryByTestId("thread-panel")).toBeNull();
    receiveThread("private-parent", "late private reply");
    expect(screen.queryByTestId("thread-panel")).toBeNull();
    await openThread("public-parent");
    expect(screen.getByTestId("thread-replies")).toBeEmptyDOMElement();
    expect(screen.queryByText("private thread detail")).toBeNull();
  });

  it("ignores a late response for an old thread after another thread is opened or closed", async () => {
    useChatStore.setState({ messages: [...threadFixture("thread-a"), ...threadFixture("thread-b")], historyLoaded: true });
    renderChatLayout();
    await openThread("thread-a");
    receiveThread("thread-a", "first thread detail");
    expect(screen.getByTestId("thread-replies")).toHaveTextContent("first thread detail");

    await openThread("thread-b");
    expect(screen.getByTestId("thread-replies")).toBeEmptyDOMElement();
    receiveThread("thread-a", "stale first thread detail");
    expect(screen.getByTestId("thread-replies")).toBeEmptyDOMElement();
    receiveThread("thread-b", "current thread detail");
    expect(screen.getByTestId("thread-replies")).toHaveTextContent("current thread detail");
    receiveThread("thread-a", "another stale reply");
    expect(screen.getByTestId("thread-replies")).toHaveTextContent("current thread detail");
    expect(screen.queryByText("another stale reply")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Close thread" }));
    receiveThread("thread-b", "reply after closing");
    expect(screen.queryByTestId("thread-panel")).toBeNull();
    await openThread("thread-b");
    expect(screen.getByTestId("thread-replies")).toBeEmptyDOMElement();
  });

  it("keeps utility menu without IM actions", () => {
    renderChatLayout();
    fireEvent.click(screen.getByLabelText("更多"));

    expect(screen.getByText("打开设置")).toBeTruthy();
    expect(screen.getByText("导出为 JSON")).toBeTruthy();
    expect(screen.getByText("导出为文本")).toBeTruthy();
    expect(screen.queryByText("English")).toBeNull();
    expect(screen.queryByText("离开聊天")).toBeNull();
    expect(screen.queryByText("创建群组")).toBeNull();
    expect(screen.queryByText("添加好友")).toBeNull();
  });

  it("opens conversation information from the compact utility menu", () => {
    renderChatLayout();
    fireEvent.click(screen.getByLabelText("更多"));
    fireEvent.click(screen.getByRole("menuitem", { name: "显示或隐藏对话信息" }));

    expect(screen.getByRole("complementary", { name: "聊天信息" })).toBeTruthy();
    expect(screen.getByRole("tabpanel")).toBeTruthy();
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
