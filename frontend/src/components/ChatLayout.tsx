import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Download,
  Globe,
  Hash,
  Menu,
  Monitor,
  Moon,
  MoreHorizontal,
  Pin,
  Search,
  Sun,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { ChatThread } from "./assistant-ui/chat-thread";
import { ConversationSearch } from "./ConversationSearch";
import { LightChatSidebar, type ChatSpace } from "./LightChatSidebar";
import { SearchBar } from "./SearchBar";
import { SettingsPanel } from "./SettingsPanel";
import { AssistantIcon } from "./AssistantIcon";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import type { Language } from "@/i18n/translations";
import {
  chatAPI,
  logout as logoutChat,
  logoutFromAccount,
} from "@/lib/api";
import type { ChatMessage } from "@/lib/api";
import { assistants, modelDisplayName, tokenBot } from "@/lib/assistantRegistry";
import { CHAT_EVENTS } from "@/lib/events";
import { STORAGE_KEYS } from "@/lib/storage";
import { cn } from "@/lib/utils";
import {
  applyTheme,
  getStoredTheme,
  nextTheme,
  THEME_STORAGE_KEY,
  type Theme,
} from "@/lib/theme";
import { useChatStore, type LegacyChatInput } from "@/stores/chatStore";
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
  const [activeSpace, setActiveSpace] = useState<ChatSpace>("public");
  const [assistantId, setAssistantId] = useState(defaultAssistant.id);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [theme, setTheme] = useState<Theme>(getStoredTheme);
  const [threadParent, setThreadParent] = useState<ChatMessage | null>(null);
  const [threadMessages, setThreadMessages] = useState<ChatMessage[]>([]);
  const [conversationSearchOpen, setConversationSearchOpen] = useState(false);
  const [searchHighlight, setSearchHighlight] = useState("");
  const [transcriptScrolled, setTranscriptScrolled] = useState(false);
  const [reconnectAttempt, setReconnectAttempt] = useState<number | null>(null);
  const [reconnectFailed, setReconnectFailed] = useState(false);
  const mainRef = useRef<HTMLDivElement>(null);
  const [keyboardPadding, setKeyboardPadding] = useState(0);

  const {
    reset,
    setCurrentChat,
    setReplyTo,
    clearConversationUnread,
    latestMention,
    setLatestMention,
    pinnedMessages,
    connected,
    setConnected,
    lightboxImage,
    username,
    isGuest,
    setShowAuthModal,
    privateBotMessages,
    setPrivateBotHistory,
    onlineUsers,
  } = useChatStore();
  const currentChat = useChatStore((s) => s.currentChat as LegacyChatInput);
  const unauthenticated = !username;
  const { disconnect, sendMessage, markRead } = useWebSocket();

  const activeAssistant = useMemo(
    () => assistants.find((assistant) => assistant.id === assistantId) ?? defaultAssistant,
    [assistantId],
  );
  const assistantMode = activeSpace !== "public";
  const serverConfig = useServerConfig();

  useEffect(() => chatAPI.on("error", (event) => {
    const key = event.code === "RUN_UNAVAILABLE" ? "botRun.unavailable"
      : event.code === "RUN_QUEUE_FULL" ? "botRun.queueFull"
      : event.code === "BOT_BUDGET_EXCEEDED" ? "botRun.budgetExceeded" : null;
    if (key) toast.error(t(key));
  }), [t]);

  useEffect(() => {
    if (!assistantMode) return;
    if (isGuest) return;
    let cancelled = false;
    chatAPI.fetchMessagesBetween(activeAssistant.name).then((msgs) => {
      if (!cancelled) setPrivateBotHistory(msgs);
    });
    return () => {
      cancelled = true;
    };
  }, [assistantMode, activeAssistant.name, isGuest, username, connected, setPrivateBotHistory]);

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
    clearConversationUnread("public");
    markRead();
  }, [activeSpace, clearConversationUnread, markRead]);

  useEffect(() => {
    const handleVisibility = () => {
      if (document.visibilityState !== "visible" || activeSpace !== "public") return;
      clearConversationUnread("public");
      markRead();
    };
    document.addEventListener("visibilitychange", handleVisibility);
    return () => document.removeEventListener("visibilitychange", handleVisibility);
  }, [activeSpace, clearConversationUnread, markRead]);

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

  const toggleLang = useCallback(() => {
    const next: Language = lang === "zh-CN" ? "en-US" : "zh-CN";
    setLang(next);
  }, [lang, setLang]);

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
      setActiveSpace(space);
      setCurrentChat({ type: "public" });
      if (space !== "public") setAssistantId(space);
      setSidebarOpen(false);
    },
    [setCurrentChat],
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

  const cycleTheme = () => setTheme((previous) => nextTheme(previous));

  const ThemeIcon = theme === "dark" ? Moon : theme === "system" ? Monitor : Sun;
  const themeLabels: Record<string, string> = {
    light: t("settings.themeLight"),
    dark: t("settings.themeDark"),
    system: t("settings.themeSystem"),
  };

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
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={toggleLang}>
          <Globe className="size-4" />
          {t("lang.switchTo")}
        </DropdownMenuItem>
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={cycleTheme}>
          <ThemeIcon className="size-4" />
          {themeLabels[theme]}
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
        />
      </div>

      <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
        <SheetContent side="left" className="w-[288px] gap-0 p-0" showCloseButton={false}>
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

      <main
        className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-card"
        style={{ paddingBottom: keyboardPadding }}
      >
        <header
          className={cn(
            "z-20 shrink-0 bg-card transition-shadow duration-200",
            transcriptScrolled && "shadow-[inset_0_-1px_0_var(--border)]",
          )}
        >
          <div className="mx-auto flex h-14 w-full max-w-4xl items-center gap-2.5 px-4 sm:px-6">
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
                !assistantMode && "hidden sm:flex",
              )}
              aria-hidden="true"
            >
              {assistantMode ? (
                <AssistantIcon assistant={activeAssistant} size="sm" />
              ) : (
                <Hash className="size-4 text-muted-foreground" />
              )}
            </span>

            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <h1
                  className="truncate text-sm font-semibold tracking-[-0.01em]"
                  data-visual="chat-title"
                >
                  {headerTitle}
                </h1>
                {assistantMode && (
                  <span className="shrink-0 text-label text-muted-foreground">AI</span>
                )}
              </div>
              <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    connected
                      ? "bg-success"
                      : reconnectFailed
                        ? "bg-destructive"
                        : "bg-warning",
                  )}
                  title={reconnectLabel}
                />
                <span className="truncate">
                  {connected ? headerSubtitle : t("sidebar.connecting")}
                </span>
                {!assistantMode && onlineUsers.length > 0 && (
                  <span className="hidden shrink-0 text-muted-foreground/70 sm:inline">
                    · {t("chat.onlineCount", { count: onlineUsers.length })}
                  </span>
                )}
              </div>
            </div>

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

        {unauthenticated && (
          <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2">
            <p className="min-w-0 flex-1 truncate text-label text-muted-foreground">
              {t("chat.guestWarning")}
            </p>
            <Button
              type="button"
              variant="outline"
              size="xs"
              className="h-7 shrink-0 bg-card text-label"
              onClick={() => setShowAuthModal(true)}
            >
              {t("join.buttonJoin")}
            </Button>
          </div>
        )}

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
              className="h-6"
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
              className="text-muted-foreground"
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
                className="h-6 max-w-[220px] shrink-0 text-label font-normal text-muted-foreground"
                onClick={() => {
                  const el = document.getElementById(`msg-${message.id}`);
                  if (!el) return;
                  el.scrollIntoView({ behavior: "smooth", block: "center" });
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
            messages={assistantMode ? privateBotMessages : undefined}
            conversationKey={assistantMode ? `dm-${activeAssistant.name}` : "public"}
            assistantContext={
              assistantMode ? { assistant: activeAssistant, modelLabel } : null
            }
            connected={connected}
            maxMessageLength={serverConfig?.max_message_length}
            onSend={sendHandler}
            onReply={handleReply}
            onDelete={handleDelete}
            onOpenThread={handleOpenThread}
            highlight={searchHighlight}
            onScrolledChange={setTranscriptScrolled}
          />
        </div>
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

      {settingsOpen && <SettingsPanel onClose={() => setSettingsOpen(false)} />}
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
