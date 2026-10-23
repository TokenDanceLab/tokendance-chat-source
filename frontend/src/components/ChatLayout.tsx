import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Download,
  Hash,
  Menu,
  MoreHorizontal,
  PanelRight,
  Pin,
  Search,
  Settings2,
  UserPlus,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { ChatThread } from "./assistant-ui/chat-thread";
import { ChatInfoRail } from "./ChatInfoRail";
import { ConversationSearch } from "./ConversationSearch";
import { LightChatSidebar, type ChatSpace } from "./LightChatSidebar";
import { SearchBar } from "./SearchBar";
import { SettingsPanel } from "./SettingsPanel";
import { ParticipantAvatar } from "./ParticipantAvatar";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useConnectionSettled } from "@/hooks/useConnectionSettled";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import {
  chatAPI,
  logout as logoutChat,
  logoutFromAccount,
} from "@/lib/api";
import type { ChatMessage } from "@/lib/api";
import { assistants, modelDisplayName, tokenBot } from "@/lib/assistantRegistry";
import { CHAT_EVENTS } from "@/lib/events";
import { STORAGE_KEYS } from "@/lib/storage";
import { cn, scrollBehavior } from "@/lib/utils";
import {
  applyTheme,
  getStoredTheme,
  THEME_STORAGE_KEY,
  type Theme,
} from "@/lib/theme";
import { useChatStore, type LegacyChatInput } from "@/stores/chatStore";
import { ownActiveRun } from "@/stores/botRunState";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";

const ImageLightbox = lazy(() =>
  import("@/components/ImageLightbox").then((m) => ({ default: m.ImageLightbox })),
);
const ThreadPanel = lazy(() =>
  import("@/components/ThreadPanel").then((m) => ({ default: m.ThreadPanel })),
);

const defaultAssistant = tokenBot;

export function ChatLayout() {
  const { t, lang, setLang } = useTranslation();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [mobileInfoOpen, setMobileInfoOpen] = useState(false);
  const [activeSpace, setActiveSpace] = useState<ChatSpace>(() => {
    // Reopening the app used to drop the user back into the public room even
    // if they were in a private conversation; remember the last space.
    const saved = localStorage.getItem("tokendance:active-space");
    return saved && saved !== "public" ? (saved as ChatSpace) : "public";
  });
  const [assistantId, setAssistantId] = useState(
    () => localStorage.getItem("tokendance:assistant-id") ?? defaultAssistant.id,
  );
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [theme, setTheme] = useState<Theme>(getStoredTheme);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(
    () => localStorage.getItem("tokendance:sidebar-collapsed") === "1",
  );
  const [infoOpen, setInfoOpen] = useState(
    () => localStorage.getItem("tokendance:info-rail") !== "0",
  );
  const [wideViewport, setWideViewport] = useState(
    () => typeof window.matchMedia === "function" &&
      window.matchMedia("(min-width: 1368px)").matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const query = window.matchMedia("(min-width: 1368px)");
    const update = () => setWideViewport(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((value) => !value);
  }, []);
  useEffect(() => {
    localStorage.setItem("tokendance:sidebar-collapsed", sidebarCollapsed ? "1" : "0");
  }, [sidebarCollapsed]);
  const closeInfoRail = useCallback(() => {
    setInfoOpen(false);
    localStorage.setItem("tokendance:info-rail", "0");
  }, []);
  const toggleInfoRail = useCallback(() => {
    setInfoOpen((value) => !value);
  }, []);
  useEffect(() => {
    localStorage.setItem("tokendance:info-rail", infoOpen ? "1" : "0");
  }, [infoOpen]);
  const [threadParent, setThreadParent] = useState<ChatMessage | null>(null);
  const [threadMessages, setThreadMessages] = useState<ChatMessage[]>([]);
  const [conversationSearchOpen, setConversationSearchOpen] = useState(false);
  const [searchHighlight, setSearchHighlight] = useState("");
  const [transcriptScrolled, setTranscriptScrolled] = useState(false);
  const [reconnectAttempt, setReconnectAttempt] = useState<number | null>(null);
  const [reconnectFailed, setReconnectFailed] = useState(false);
  const mainRef = useRef<HTMLDivElement>(null);
  const [keyboardPadding, setKeyboardPadding] = useState(0);

  // Select only the slices this shell actually renders. A whole-store
  // subscription re-rendered the sidebar, header and transcript for every
  // streaming token.
  const reset = useChatStore((s) => s.reset);
  const setCurrentChat = useChatStore((s) => s.setCurrentChat);
  const setReplyTo = useChatStore((s) => s.setReplyTo);
  const clearConversationUnread = useChatStore((s) => s.clearConversationUnread);
  const setActiveConversationKey = useChatStore((s) => s.setActiveConversationKey);
  const latestMention = useChatStore((s) => s.latestMention);
  const setLatestMention = useChatStore((s) => s.setLatestMention);
  const pinnedMessages = useChatStore((s) => s.pinnedMessages);
  const connected = useChatStore((s) => s.connected);
  // Chrome keeps the last state through a short blip; controls still use the
  // real value so nothing is sent into a dead socket.
  const settledConnected = useConnectionSettled(connected);
  const setConnected = useChatStore((s) => s.setConnected);
  const lightboxImage = useChatStore((s) => s.lightboxImage);
  const username = useChatStore((s) => s.username);
  const isGuest = useChatStore((s) => s.isGuest);
  const onlineUsers = useChatStore((s) => s.onlineUsers);
  const currentChat = useChatStore((s) => s.currentChat as LegacyChatInput);
  const { disconnect, sendMessage, markRead } = useWebSocket();

  const activeAssistant = useMemo(
    () => assistants.find((assistant) => assistant.id === assistantId) ?? defaultAssistant,
    [assistantId],
  );
  const assistantMode = activeSpace !== "public";
  const activePrivateThread = useChatStore((s) => s.activePrivateThread);
  const setActivePrivateThread = useChatStore((s) => s.setActivePrivateThread);
  // Reopen the lane the user was last in for this assistant. Without this a
  // refresh silently dropped a side chat and showed the main chat instead.
  const laneStorageKey = `tokendance:active-lane:${activeAssistant.name}`;
  useEffect(() => {
    if (!assistantMode) return;
    const saved = localStorage.getItem(laneStorageKey);
    if (saved) setActivePrivateThread(saved);
  }, [assistantMode, laneStorageKey, setActivePrivateThread]);
  useEffect(() => {
    if (!assistantMode) return;
    if (activePrivateThread) localStorage.setItem(laneStorageKey, activePrivateThread);
    else localStorage.removeItem(laneStorageKey);
  }, [assistantMode, activePrivateThread, laneStorageKey]);
  const conversationKey = assistantMode
    ? activePrivateThread ? `dm-${activeAssistant.name}:${activePrivateThread}` : `dm-${activeAssistant.name}`
    : "public";
  const serverConfig = useServerConfig();
  const botRuns = useChatStore((s) => s.botRuns);
  // The same composer stop works in either space, never for a peer's request.
  const activeAssistantRun = useMemo(
    () => ownActiveRun(botRuns, username, assistantMode),
    [assistantMode, botRuns, username],
  );
  const cancelAssistantRun = useCallback(() => {
    if (!activeAssistantRun || activeAssistantRun.cancelPending || !connected ||
      serverConfig?.capabilities?.run_control !== true) return;
    if (chatAPI.cancelBotRun(activeAssistantRun.run_id)) {
      useChatStore.getState().setRunCancelPending(activeAssistantRun.run_id, true);
    } else {
      toast.error(t("system.disconnected"));
    }
  }, [activeAssistantRun, connected, serverConfig?.capabilities?.run_control, t]);

  useEffect(() => chatAPI.on("error", (event) => {
    const key = event.code === "RUN_UNAVAILABLE" ? "botRun.unavailable"
      : event.code === "RUN_QUEUE_FULL" ? "botRun.queueFull"
      : event.code === "BOT_BUDGET_EXCEEDED" ? "botRun.budgetExceeded" : null;
    if (key) toast.error(t(key));
  }), [t]);

  useEffect(() => {
    if (!assistantMode || !connected) return;
    // Private history loads over the WebSocket so guest identities (no HTTP
    // session) get their thread back after a refresh too. The lane-aware store
    // keeps the previous lane's messages out of view while the answer loads.
    chatAPI.sendLoadPrivateHistory(activeAssistant.name, activePrivateThread);
    chatAPI.sendListPrivateThreads(activeAssistant.name);
  }, [assistantMode, activeAssistant.name, activePrivateThread, username, connected]);

  useEffect(() => {
    if (currentChat.type !== "public") setCurrentChat({ type: "public" });
  }, [currentChat.type, setCurrentChat]);

  useEffect(() => {
    applyTheme(theme);
    localStorage.setItem(THEME_STORAGE_KEY, theme);
    if (theme !== "system") return;
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = () => applyTheme("system");
    query.addEventListener("change", handleChange);
    return () => query.removeEventListener("change", handleChange);
  }, [theme]);

  useEffect(() => {
    if (activeSpace === "public") localStorage.removeItem("tokendance:active-space");
    else localStorage.setItem("tokendance:active-space", activeSpace);
  }, [activeSpace]);
  useEffect(() => {
    localStorage.setItem("tokendance:assistant-id", assistantId);
  }, [assistantId]);
  useEffect(() => {
    setActiveConversationKey(conversationKey);
    clearConversationUnread(conversationKey);
    if (!assistantMode) markRead();
  }, [assistantMode, conversationKey, setActiveConversationKey, clearConversationUnread, markRead]);

  useEffect(() => {
    const handleVisibility = () => {
      if (document.visibilityState !== "visible") return;
      clearConversationUnread(conversationKey);
      if (!assistantMode) markRead();
    };
    document.addEventListener("visibilitychange", handleVisibility);
    return () => document.removeEventListener("visibilitychange", handleVisibility);
  }, [assistantMode, conversationKey, clearConversationUnread, markRead]);

  useEffect(() => {
    const handleResize = () => {
      if (!window.visualViewport || !mainRef.current) return;
      const diff = window.innerHeight - window.visualViewport.height;
      setKeyboardPadding(window.innerWidth < 768 && diff > 100 ? diff : 0);
    };
    window.visualViewport?.addEventListener("resize", handleResize);
    window.visualViewport?.addEventListener("scroll", handleResize);
    return () => {
      window.visualViewport?.removeEventListener("resize", handleResize);
      window.visualViewport?.removeEventListener("scroll", handleResize);
    };
  }, []);

  useEffect(() => {
    if (window.innerWidth < 768) setSidebarOpen(false);
    setConversationSearchOpen(false);
    setSearchHighlight("");
    setTranscriptScrolled(false);
  }, [activeSpace, assistantId]);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ scrolled?: boolean }>).detail;
      setTranscriptScrolled(Boolean(detail?.scrolled));
    };
    window.addEventListener(CHAT_EVENTS.transcriptScrolled, handler);
    return () => window.removeEventListener(CHAT_EVENTS.transcriptScrolled, handler);
  }, []);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const mod = event.ctrlKey || event.metaKey;
      if (mod && event.key === "f") {
        event.preventDefault();
        setConversationSearchOpen((prev) => {
          if (prev) window.dispatchEvent(new CustomEvent(CHAT_EVENTS.focusConversationSearch));
          return !prev;
        });
      }
      if (event.key === "Escape") {
        if (useChatStore.getState().replyTo) {
          event.preventDefault();
          setReplyTo(null);
        }
        setSettingsOpen(false);
        setThreadParent(null);
        setThreadMessages([]);
        window.dispatchEvent(new CustomEvent(CHAT_EVENTS.exitSelectMode));
        window.dispatchEvent(new CustomEvent(CHAT_EVENTS.closeEmojiPicker));
        setSidebarOpen(false);
        setConversationSearchOpen(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [setReplyTo]);

  useEffect(() => {
    const unsubReconnecting = chatAPI.on("reconnecting", (msg) => {
      setReconnectAttempt((msg as unknown as { attempt: number }).attempt);
      setReconnectFailed(false);
    });
    const unsubReconnected = chatAPI.on("reconnected", () => {
      setReconnectAttempt(null);
      setReconnectFailed(false);
      setConnected(true);
    });
    const unsubReconnectFailed = chatAPI.on("reconnect_failed", () => {
      setReconnectAttempt(null);
      setReconnectFailed(true);
    });
    return () => {
      unsubReconnecting();
      unsubReconnected();
      unsubReconnectFailed();
    };
  }, [setConnected]);

  useEffect(() => {
    const unsub = chatAPI.on(
      "thread_messages",
      (msg: { type: string; parent_message_id?: string; messages?: ChatMessage[] }) => {
        if (threadParent && msg.parent_message_id === threadParent.id && msg.messages) {
          setThreadMessages(msg.messages);
        }
      },
    );
    return () => unsub();
  }, [threadParent]);

  const clearBrowserSessionState = useCallback(() => {
    localStorage.removeItem(STORAGE_KEYS.auth);
    localStorage.removeItem(STORAGE_KEYS.username);
  }, []);

  const handleDisconnect = useCallback(async () => {
    if (!isGuest) {
      try {
        await logoutChat();
      } catch {
        toast.error(t("auth.logoutFailed"));
        return;
      }
    }
    disconnect();
    reset();
    clearBrowserSessionState();
  }, [clearBrowserSessionState, disconnect, isGuest, reset, t]);

  const handleAccountLogout = useCallback(async () => {
    try {
      const redirectURL = await logoutFromAccount();
      disconnect();
      reset();
      clearBrowserSessionState();
      window.location.assign(redirectURL);
    } catch {
      toast.error(t("auth.accountLogoutFailed"));
    }
  }, [clearBrowserSessionState, disconnect, reset, t]);

  const handleSpaceSelect = useCallback(
    (space: ChatSpace) => {
      setThreadParent(null);
      setThreadMessages([]);
      setReplyTo(null);
      setActiveSpace(space);
      setCurrentChat({ type: "public" });
      if (space !== "public") setAssistantId(space);
      setSidebarOpen(false);
    },
    [setCurrentChat, setReplyTo],
  );

  const sendHandler = useCallback(
    (content: string) => {
      if (assistantMode) sendMessage(content, activeAssistant.name);
      else sendMessage(content);
    },
    [assistantMode, activeAssistant.name, sendMessage],
  );

  const handleExport = useCallback(
    async (format: "json" | "text") => {
      try {
        const blob = await chatAPI.exportChat("public", format);
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        const ext = format === "json" ? "json" : "txt";
        const now = new Date().toISOString().slice(0, 10);
        a.download = `chat_export_public_${now}.${ext}`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
        toast.success(t("export.exportSuccess"));
      } catch {
        toast.error(t("export.exportError"));
      }
    },
    [t],
  );

  const handleDelete = useCallback((messageId: string) => {
    chatAPI.deleteMessage(messageId);
  }, []);

  const handleRetryMessage = useCallback(
    (message: ChatMessage) => {
      // The rejected draft never reached the server; drop the local row and
      // resend through the conversation it was written in, not whichever view
      // happens to be open now.
      useChatStore.getState().discardMessage(message.id);
      if (message.private) sendMessage(message.content, message.to || activeAssistant.name);
      else sendMessage(message.content);
    },
    [activeAssistant.name, sendMessage],
  );

  const handleOpenThread = useCallback((message: ChatMessage) => {
    setThreadParent(message);
    setThreadMessages([]);
    chatAPI.requestThreadMessages(message.id);
  }, []);

  const handleCloseThread = useCallback(() => {
    setThreadParent(null);
    setThreadMessages([]);
  }, []);

  const handleSendThreadReply = useCallback(
    (content: string) => {
      if (threadParent) {
        chatAPI.sendThreadReply(threadParent.id, content, assistantMode ? activeAssistant.name : undefined);
      }
    },
    [threadParent, assistantMode, activeAssistant.name],
  );

  const handleReply = useCallback(
    (message: ChatMessage) => setReplyTo(message),
    [setReplyTo],
  );

  const headerTitle = assistantMode ? activeAssistant.name : t("chat.roomName");
  const modelLabel = modelDisplayName(serverConfig?.model || activeAssistant.model.id);
  const headerSubtitle = assistantMode
    ? `${t("chat.dmLabel")} · ${modelLabel}${serverConfig?.capabilities?.web_search ? ` · ${t("chat.webSearchAvailable")}` : ""}`
    : t("chat.subtitle");

  const reconnectLabel = reconnectFailed
    ? t("system.reconnectFailed")
    : t("system.reconnecting", { attempt: String((reconnectAttempt ?? 0) + 1) });

  // Muse-style share entry: native share sheet when available, otherwise a
  // copied room link. No server-side invite records are created.
  const handleInvite = useCallback(async () => {
    const url = `${window.location.origin}/`;
    const text = t("chat.inviteText");
    const coarsePointer =
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(pointer: coarse)").matches;
    if (coarsePointer && typeof navigator.share === "function") {
      try {
        await navigator.share({ title: "TokenDance Chat", text, url });
      } catch {
        /* user cancelled the share sheet */
      }
      return;
    }
    try {
      await navigator.clipboard.writeText(url);
      toast.success(t("chat.inviteCopied"));
    } catch {
      toast.error(t("chat.inviteFailed"));
    }
  }, [t]);

  const moreMenu = (
    <DropdownMenu open={moreOpen} onOpenChange={setMoreOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-11 rounded-full text-muted-foreground lg:size-9"
          aria-label={t("more.label")}
          onClick={() => setMoreOpen(true)}
        >
          <MoreHorizontal className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="td-chat-menu w-52 rounded-lg p-1">
        <DropdownMenuLabel className="text-label font-normal text-muted-foreground">
          TokenDanceChat
        </DropdownMenuLabel>
        <DropdownMenuItem
          className="min-h-11 rounded-md min-[1368px]:hidden"
          onSelect={() => setMobileInfoOpen(true)}
        >
          <PanelRight className="size-4" />
          {t("a11y.toggleInfo")}
        </DropdownMenuItem>
        {!assistantMode && (
          <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => void handleInvite()}>
            <UserPlus className="size-4" />
            {t("chat.invite")}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => setSettingsOpen(true)}>
          <Settings2 className="size-4" />
          {t("settings.openSettings")}
        </DropdownMenuItem>
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => void handleExport("json")}>
          <Download className="size-4" />
          {t("export.exportJson")}
        </DropdownMenuItem>
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => void handleExport("text")}>
          <Download className="size-4" />
          {t("export.exportText")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return (
    <div ref={mainRef} className="flex h-screen-mobile overflow-hidden bg-background">
      <div className="hidden h-full shrink-0 lg:flex">
        <LightChatSidebar
          activeSpace={activeSpace}
          activeAssistantId={assistantId}
          onSelectSpace={handleSpaceSelect}
          onOpenSettings={() => setSettingsOpen(true)}
          onLeave={() => void handleDisconnect()}
          onAccountLogout={
            username && !isGuest && serverConfig?.oidc_enabled
              ? () => void handleAccountLogout()
              : undefined
          }
          collapsed={sidebarCollapsed}
          onToggleCollapse={toggleSidebar}
        />
      </div>

      <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
        <SheetContent side="left" className="w-[240px] gap-0 p-0" showCloseButton={false}>
          <SheetTitle className="sr-only">{t("chat.publicChat")}</SheetTitle>
          <LightChatSidebar
            activeSpace={activeSpace}
            activeAssistantId={assistantId}
            onSelectSpace={handleSpaceSelect}
            onClose={() => setSidebarOpen(false)}
            onOpenSettings={() => {
              setSidebarOpen(false);
              setSettingsOpen(true);
            }}
            onLeave={() => void handleDisconnect()}
            onAccountLogout={
              username && !isGuest && serverConfig?.oidc_enabled
                ? () => void handleAccountLogout()
                : undefined
            }
          />
        </SheetContent>
      </Sheet>

      <Sheet open={mobileInfoOpen} onOpenChange={setMobileInfoOpen}>
        <SheetContent
          side="right"
          className="gap-0 p-0 data-[side=right]:w-[min(360px,calc(100vw-1rem))]"
          showCloseButton={false}
        >
          <SheetTitle className="sr-only">
            {assistantMode ? t("info.title") : t("info.publicTitle")}
          </SheetTitle>
          <ChatInfoRail
            privateChat={assistantMode}
            assistant={activeAssistant}
            modelLabel={modelLabel}
            onClose={() => setMobileInfoOpen(false)}
            embedded
          />
        </SheetContent>
      </Sheet>

      <main
        className="relative flex min-w-0 flex-1 overflow-hidden bg-card dark:bg-background"
        style={{ paddingBottom: keyboardPadding }}
      >
        <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
        <header
          className={cn(
            "z-20 shrink-0 transition-shadow duration-200",
            wideViewport ? "bg-transparent" : "bg-card dark:bg-background",
            !wideViewport && transcriptScrolled && "shadow-[inset_0_-1px_0_var(--border)]",
          )}
        >
          <div
            className={cn(
              "flex w-full items-center gap-2.5 px-4 sm:px-6",
              wideViewport ? "h-12" : "mx-auto h-14 max-w-3xl",
            )}
            data-visual="chat-toolbar"
          >
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-11 shrink-0 text-muted-foreground lg:hidden"
              aria-label={t("a11y.openSidebar")}
              onClick={() => setSidebarOpen(true)}
            >
              <Menu className="size-5" />
            </Button>

            <span
              className={cn(
                "flex size-8 shrink-0 items-center justify-center",
                wideViewport ? "hidden" : !assistantMode && "hidden sm:flex",
              )}
              aria-hidden="true"
            >
              {assistantMode ? (
                <ParticipantAvatar username={activeAssistant.name} assistant={activeAssistant} size="sm" />
              ) : (
                <Hash className="size-4 text-muted-foreground" />
              )}
            </span>

            <div className={cn("min-w-0 flex-1", wideViewport && "sr-only")}>
              <div className="flex min-w-0 items-center gap-2">
                <h1
                  className="truncate text-sm font-semibold tracking-[-0.01em]"
                  data-visual="chat-title"
                >
                  {headerTitle}
                </h1>
              </div>
              <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    settledConnected
                      ? "bg-success"
                      : reconnectFailed
                        ? "bg-destructive"
                        : "bg-warning",
                  )}
                  title={reconnectLabel}
                />
                <span className="truncate">
                  {settledConnected ? headerSubtitle : t("sidebar.connecting")}
                </span>
                {!assistantMode && onlineUsers.length > 0 && (
                  <span className="hidden shrink-0 text-muted-foreground/70 sm:inline">
                    · {t("chat.onlineCount", { count: onlineUsers.length })}
                  </span>
                )}
              </div>
            </div>
            {wideViewport && <div className="flex-1" aria-hidden="true" />}

            {wideViewport && !assistantMode && (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => void handleInvite()}
                className="h-8 shrink-0 gap-1.5 rounded-full bg-card px-3 text-xs shadow-sm"
              >
                <UserPlus className="size-3.5" aria-hidden="true" />
                {t("chat.invite")}
              </Button>
            )}
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={toggleInfoRail}
              aria-label={t("a11y.toggleInfo")}
              aria-pressed={infoOpen}
              className="hidden size-9 shrink-0 text-muted-foreground min-[1368px]:inline-flex"
            >
              <PanelRight className="size-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => setConversationSearchOpen((prev) => !prev)}
              aria-label={t("search.inConversation")}
              title={`${t("search.inConversation")} (Ctrl+F)`}
              className="size-11 shrink-0 text-muted-foreground lg:size-9"
            >
              <Search className="size-4" />
            </Button>
            <div data-more-menu>{moreMenu}</div>
          </div>
        </header>

        {latestMention && (
          <div className="flex items-center gap-3 border-b bg-muted/40 px-4 py-2 text-xs">
            <span className="min-w-0 flex-1 truncate text-foreground/80">
              <span className="font-medium">{latestMention.from}</span>{" "}
              {t("mention.mentionedYou")}:{" "}
              <span className="text-muted-foreground">{latestMention.content}</span>
            </span>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="h-11 sm:h-6"
              onClick={() => {
                setLatestMention(null);
                if (latestMention.messageId) {
                  window.dispatchEvent(
                    new CustomEvent(CHAT_EVENTS.scrollToMessage, {
                      detail: { id: latestMention.messageId },
                    }),
                  );
                }
              }}
            >
              {t("mention.view")}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              className="size-11 text-muted-foreground sm:size-6"
              onClick={() => setLatestMention(null)}
              aria-label={t("mention.dismiss")}
            >
              <X className="size-3" />
            </Button>
          </div>
        )}

        {pinnedMessages.length > 0 && (
          <div className="no-scrollbar flex items-center gap-2 overflow-x-auto border-b bg-muted/30 px-4 py-1.5">
            <Pin className="size-3 shrink-0 text-muted-foreground" />
            {pinnedMessages.map((message) => (
              <Button
                key={message.id}
                type="button"
                variant="secondary"
                size="xs"
                className="h-11 max-w-[220px] shrink-0 text-label font-normal text-muted-foreground sm:h-6"
                onClick={() => {
                  const el = document.getElementById(`msg-${message.id}`);
                  if (!el) return;
                  el.scrollIntoView({ behavior: scrollBehavior(), block: "center" });
                  el.classList.add("bg-primary/[0.06]");
                  setTimeout(() => el.classList.remove("bg-primary/[0.06]"), 2000);
                }}
              >
                <span className="truncate">
                  {message.username}: {(message.content || "").slice(0, 40)}
                  {(message.content || "").length > 40 ? "..." : ""}
                </span>
              </Button>
            ))}
          </div>
        )}

        <ConversationSearch
          open={conversationSearchOpen}
          onClose={() => {
            setConversationSearchOpen(false);
            setSearchHighlight("");
          }}
          onHighlightChange={setSearchHighlight}
        />

        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
          <ChatThread
            usePrivateMessages={assistantMode}
            conversationKey={assistantMode ? `dm-${activeAssistant.name}` : "public"}
            assistantContext={
              assistantMode ? { assistant: activeAssistant, modelLabel } : null
            }
            connected={connected}
            maxMessageLength={serverConfig?.max_message_length}
            onSend={sendHandler}
            isRunning={Boolean(activeAssistantRun)}
            cancelPending={activeAssistantRun?.cancelPending === true}
            onCancelRun={
              activeAssistantRun && serverConfig?.capabilities?.run_control === true
                ? cancelAssistantRun
                : undefined
            }
            onReply={handleReply}
            onDelete={handleDelete}
            onRetryMessage={handleRetryMessage}
            onOpenThread={handleOpenThread}
            highlight={searchHighlight}
            onScrolledChange={setTranscriptScrolled}
          />
        </div>
        </div>
        {infoOpen && wideViewport && (
          <ChatInfoRail
            privateChat={assistantMode}
            assistant={activeAssistant}
            modelLabel={modelLabel}
            onClose={closeInfoRail}
          />
        )}
      </main>

      <Suspense fallback={null}>
        {threadParent && (
          <ThreadPanel
            parentMessage={threadParent}
            threadMessages={threadMessages}
            onClose={handleCloseThread}
            onSendReply={handleSendThreadReply}
          />
        )}
      </Suspense>

      {settingsOpen && (
        <SettingsPanel
          onClose={() => setSettingsOpen(false)}
          theme={theme}
          onThemeChange={setTheme}
          lang={lang}
          onLangChange={setLang}
        />
      )}
      <SearchBar />

      {lightboxImage && (
        <Suspense fallback={null}>
          <ImageLightbox
            imageUrl={lightboxImage}
            onClose={() => useChatStore.getState().setLightboxImage(null)}
          />
        </Suspense>
      )}
    </div>
  );
}
