import {
  lazy,
  Suspense,
  useCallback,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import {
  AssistantRuntimeProvider,
  ComposerPrimitive,
  MessagePrimitive,
  SuggestionPrimitive,
  ThreadPrimitive,
  useAui,
  useAuiState,
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { ArrowDown, ArrowUp, Smile } from "lucide-react";
import { BotRunStrip } from "@/components/BotRunStrip";
import { isStreamingMessage, publicMessage } from "@/stores/botRunState";
import { MessageBubble } from "@/components/MessageBubble";
import { AssistantIcon } from "@/components/AssistantIcon";
import { Button } from "@/components/ui/button";
import { useTranslation } from "@/i18n/context";
import { assistants, type AssistantDefinition } from "@/lib/assistantRegistry";
import type { ChatMessage } from "@/lib/api";
import { systemMessageText, withVisibleReplyPreview } from "@/lib/messagePresentation";
import { cn, formatDate } from "@/lib/utils";
import { useChatStore } from "@/stores/chatStore";

const EmojiPicker = lazy(() =>
  import("@/components/EmojiPicker").then((m) => ({ default: m.EmojiPicker })),
);

const GROUP_WINDOW_MS = 2 * 60 * 1000;
const MAX_RENDERED_MESSAGES = 200;

type MessageGroupPosition = "single" | "first" | "middle" | "last";

interface PreparedMessage {
  message: ChatMessage;
  isOwn: boolean;
  groupPosition: MessageGroupPosition;
  hideAvatar: boolean;
  hideUsername: boolean;
  forceShowTimestamp: boolean;
  isNew: boolean;
  showDate: boolean;
  replyCount: number;
}

interface AssistantContext {
  assistant: AssistantDefinition;
  modelLabel: string;
}

interface ChatThreadProps {
  messages?: ChatMessage[];
  conversationKey: string;
  assistantContext?: AssistantContext | null;
  connected: boolean;
  maxMessageLength?: number;
  onSend: (content: string) => void;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onOpenThread?: (message: ChatMessage) => void;
  highlight?: string;
  onScrolledChange?: (scrolled: boolean) => void;
}

function prepareMessages(messages: ChatMessage[], username: string): PreparedMessage[] {
  const visibleById = new Map(messages.map((message) => [message.id, message]));
  messages = messages.map((message) => withVisibleReplyPreview(message, visibleById));
  const replyCounts = new Map<string, number>();
  for (const message of messages) {
    if (message.reply_to_id) {
      replyCounts.set(message.reply_to_id, (replyCounts.get(message.reply_to_id) ?? 0) + 1);
    }
    if (message.thread_id) {
      replyCounts.set(message.thread_id, (replyCounts.get(message.thread_id) ?? 0) + 1);
    }
  }

  return messages.map((message, index) => {
    const previous = index > 0 ? messages[index - 1] : null;
    const next = index + 1 < messages.length ? messages[index + 1] : null;
    const continuesGroup =
      previous?.username === message.username &&
      message.username !== "system" &&
      !message.deleted &&
      message.timestamp - previous.timestamp < GROUP_WINDOW_MS;
    const nextContinuesGroup =
      next?.username === message.username &&
      !next.deleted &&
      next.timestamp - message.timestamp < GROUP_WINDOW_MS;
    const groupPosition: MessageGroupPosition = !continuesGroup
      ? nextContinuesGroup
        ? "first"
        : "single"
      : nextContinuesGroup
        ? "middle"
        : "last";

    return {
      message,
      isOwn: message.username === username,
      groupPosition,
      hideAvatar: continuesGroup,
      hideUsername: continuesGroup,
      forceShowTimestamp: !nextContinuesGroup,
      isNew: message.id.startsWith("optimistic_") || message.timestamp > Date.now() - 2000,
      showDate:
        !previous ||
        new Date(previous.timestamp).toDateString() !==
          new Date(message.timestamp).toDateString(),
      replyCount: replyCounts.get(message.id) ?? 0,
    };
  });
}

function messageText(message: ThreadMessageLike): string {
  if (typeof message.content === "string") return message.content;
  return message.content
    .filter((part) => part.type === "text")
    .map((part) => ("text" in part ? part.text : ""))
    .join("");
}

/** Maps TokenBot parts onto assistant-ui's thread message parts. */
function toRuntimeContent(message: ChatMessage): ThreadMessageLike["content"] {
  if (!message.parts?.length) return message.content;
  // Source presentation belongs to MessageBubble; keep it out of copy/text actions.
  return message.parts.filter((part) => part.type !== "source").map((part) => {
    if (part.type === "reasoning") {
      return { type: "reasoning" as const, text: part.text ?? "" };
    }
    if (part.type === "tool-call") {
      return {
        type: "tool-call" as const,
        toolCallId: part.tool_call_id ?? `tool-${message.id}`,
        toolName: part.tool_name ?? "tool",
        argsText: part.arguments ?? "",
        ...(part.result !== undefined ? { result: part.result } : {}),
        isError: part.status === "error",
      };
    }
    return { type: "text" as const, text: part.text ?? "" };
  });
}

function ChatMessageRow({
  preparedById,
  onReply,
  onDelete,
  onOpenThread,
  highlight,
  onlineUsers,
  emojiPreprocess,
}: {
  preparedById: Map<string, PreparedMessage>;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onOpenThread?: (message: ChatMessage) => void;
  highlight?: string;
  onlineUsers: string[];
  emojiPreprocess: { emojiMap: Map<string, string>; pattern: RegExp } | null;
}) {
  const { t } = useTranslation();
  const messageId = useAuiState(
    (state) =>
      (state.message.metadata.custom as { messageId?: string } | undefined)?.messageId,
  );
  const role = useAuiState((state) => state.message.role);
  const prepared = messageId ? preparedById.get(messageId) : undefined;
  if (!prepared) return null;

  return (
    <MessagePrimitive.Root data-role={role} className="min-w-0">
      {prepared.message.username === "system" ? (
        <div className="flex justify-center py-2">
          <span className="text-xs text-muted-foreground">
            {systemMessageText(prepared.message.content, t)}
          </span>
        </div>
      ) : (
        <>
          {prepared.showDate && (
            <div className="flex justify-center py-4">
              <span className="text-label text-muted-foreground/60">
                {formatDate(prepared.message.timestamp, t)}
              </span>
            </div>
          )}
          <MessageBubble
            message={prepared.message}
            isOwn={prepared.isOwn}
            hideAvatar={prepared.hideAvatar}
            hideUsername={prepared.hideUsername}
            forceShowTimestamp={prepared.forceShowTimestamp}
            isGrouped={prepared.groupPosition !== "single"}
            groupPosition={prepared.groupPosition}
            isNew={prepared.isNew}
            replyCount={prepared.replyCount}
            onReply={onReply}
            onDelete={onDelete}
            onOpenThread={onOpenThread}
            highlight={highlight}
            onlineUsers={onlineUsers}
            emojiPreprocess={emojiPreprocess}
          />
        </>
      )}
    </MessagePrimitive.Root>
  );
}

function MentionPopover({
  candidates,
  activeIndex,
  onSelect,
}: {
  candidates: string[];
  activeIndex: number;
  onSelect: (username: string) => void;
}) {
  const { t } = useTranslation();
  if (candidates.length === 0) return null;
  return (
    <div
      role="listbox"
      aria-label={t("input.mentionSuggestions")}
      className="absolute bottom-full left-2 right-2 z-30 mb-2 max-h-56 overflow-y-auto rounded-xl border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {candidates.map((candidate, index) => (
        <button
          key={candidate}
          type="button"
          role="option"
          aria-selected={index === activeIndex}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => onSelect(candidate)}
          className={cn(
            "flex h-9 w-full items-center gap-2 rounded-lg px-2.5 text-left text-sm",
            index === activeIndex
              ? "bg-accent text-accent-foreground"
              : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
          )}
        >
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted text-micro font-medium">
            {candidate.charAt(0).toUpperCase()}
          </span>
          <span className="truncate">{candidate}</span>
        </button>
      ))}
    </div>
  );
}

function ChatComposer({
  assistantContext,
  connected,
  maxMessageLength,
  publicRoom,
}: {
  assistantContext?: AssistantContext | null;
  connected: boolean;
  maxMessageLength: number;
  publicRoom: boolean;
}) {
  const { t } = useTranslation();
  const composer = useAui().composer;
  const text = useAuiState((state) => state.composer.text);
  const canSend = useAuiState((state) => state.composer.canSend);
  const [mentionQuery, setMentionQuery] = useState<{
    query: string;
    start: number;
    end: number;
  } | null>(null);
  const [mentionIndex, setMentionIndex] = useState(0);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const onlineUsers = useChatStore((state) => state.onlineUsers);

  const mentionCandidates = useMemo(() => {
    if (!publicRoom || !mentionQuery) return [];
    const query = mentionQuery.query.toLowerCase();
    const users = [...onlineUsers, ...assistants.map((assistant) => assistant.name)];
    return [...new Set(users)]
      .filter((user) => user.toLowerCase().startsWith(query))
      .slice(0, 6);
  }, [mentionQuery, onlineUsers, publicRoom]);

  const updateMention = useCallback((value: string, cursor: number) => {
    const beforeCursor = value.slice(0, cursor);
    const match = beforeCursor.match(/(?:^|\s)@([\p{L}\p{N}_]*)$/u);
    if (!match) {
      setMentionQuery(null);
      setMentionIndex(0);
      return;
    }
    const start = cursor - match[1].length - 1;
    setMentionQuery({ query: match[1], start, end: cursor });
    setMentionIndex(0);
  }, []);

  const insertMention = useCallback(
    (username: string) => {
      if (!mentionQuery) return;
      const value = inputRef.current?.value ?? text;
      const next = `${value.slice(0, mentionQuery.start)}@${username} ${value.slice(mentionQuery.end)}`;
      composer.setText(next);
      setMentionQuery(null);
      setMentionIndex(0);
      requestAnimationFrame(() => {
        const input = inputRef.current;
        if (!input) return;
        input.focus();
        const cursor = mentionQuery.start + username.length + 2;
        input.setSelectionRange(cursor, cursor);
      });
    },
    [composer, mentionQuery, text],
  );

  const handleInputChange = useCallback(
    (event: React.ChangeEvent<HTMLTextAreaElement>) => {
      updateMention(event.currentTarget.value, event.currentTarget.selectionStart);
    },
    [updateMention],
  );

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTextAreaElement>) => {
      if (!mentionQuery || mentionCandidates.length === 0) return;
      if (event.key === "ArrowDown") {
        event.preventDefault();
        event.stopPropagation();
        setMentionIndex((index) => (index + 1) % mentionCandidates.length);
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        event.stopPropagation();
        setMentionIndex(
          (index) => (index - 1 + mentionCandidates.length) % mentionCandidates.length,
        );
      } else if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        event.stopPropagation();
        insertMention(mentionCandidates[mentionIndex]);
      } else if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        setMentionQuery(null);
      }
    },
    [insertMention, mentionCandidates, mentionIndex, mentionQuery],
  );

  const placeholder = assistantContext
    ? t("input.assistantPlaceholder", { name: assistantContext.assistant.name })
    : t("input.placeholder");

  return (
    <ComposerPrimitive.Root
      className="relative flex w-full flex-col"
      data-testid="chat-input"
      data-visual="composer-card"
    >
      <BotRunStrip privateChat={!publicRoom} connected={connected} />
      <div
        data-slot="composer-shell"
        data-visual="composer-body"
        className="relative flex w-full cursor-text flex-col gap-2 rounded-2xl border border-foreground/10 bg-muted/30 p-2 transition-[border-color] focus-within:border-foreground/25"
      >
        <MentionPopover
          candidates={mentionCandidates}
          activeIndex={mentionIndex}
          onSelect={insertMention}
        />

        <ComposerPrimitive.Input
          ref={inputRef}
          data-visual="composer-textarea"
          onChange={handleInputChange}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          aria-label={placeholder}
          maxLength={maxMessageLength}
          rows={1}
          autoFocus
          enterKeyHint="send"
          className="max-h-48 min-h-10 w-full resize-none bg-transparent px-2.5 py-1 text-base leading-6 outline-none placeholder:text-muted-foreground/60"
        />

        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => setEmojiOpen((open) => !open)}
              aria-label={t("emoji.openPicker")}
              data-visual="composer-emoji-button"
              className="size-7 rounded-full text-muted-foreground"
            >
              <Smile className="size-4" />
            </Button>
            {assistantContext ? (
              <div
                data-visual="composer-ai-context"
                className="flex min-w-0 items-center gap-1.5 px-1.5 text-xs text-muted-foreground"
              >
                <AssistantIcon assistant={assistantContext.assistant} size="sm" />
                <span className="truncate font-medium text-foreground">
                  {assistantContext.assistant.name}
                </span>
                <span className="truncate">{assistantContext.modelLabel}</span>
              </div>
            ) : publicRoom ? (
              <span className="truncate px-1.5 text-xs text-muted-foreground/60">
                {t("input.mentionHint")}
              </span>
            ) : null}
          </div>

          <div className="flex shrink-0 items-center gap-2">
            {text.length > 0 && (
              <span
                className={cn(
                  "text-[11px] tabular-nums",
                  text.length > maxMessageLength * 0.9
                    ? "text-destructive/70"
                    : "text-muted-foreground/50",
                )}
              >
                {t("input.characters", {
                  current: text.length,
                  max: maxMessageLength,
                })}
              </span>
            )}
            <ComposerPrimitive.Send asChild>
              <Button
                type="submit"
                size="icon"
                disabled={!connected || !canSend}
                aria-label={t("input.send")}
                data-visual="composer-send"
                className="size-7 rounded-full"
              >
                <ArrowUp className="size-4" />
              </Button>
            </ComposerPrimitive.Send>
          </div>
        </div>
      </div>

      {emojiOpen && (
        <Suspense fallback={null}>
          <EmojiPicker
            onSelect={(emoji) => {
              const value = inputRef.current?.value ?? text;
              composer.setText(value + emoji);
              requestAnimationFrame(() => inputRef.current?.focus());
            }}
            onClose={() => setEmojiOpen(false)}
          />
        </Suspense>
      )}
    </ComposerPrimitive.Root>
  );
}

function ThreadWelcome() {
  const { t } = useTranslation();
  return (
    <div className="mb-6 flex animate-in flex-col items-center px-2 text-center duration-200 fade-in slide-in-from-bottom-1">
      <h2 className="text-balance text-2xl font-medium tracking-tight sm:text-3xl">
        {t("transcript.welcomeAssistant")}
      </h2>
      <p className="mt-2.5 max-w-md text-pretty text-sm leading-6 text-muted-foreground">
        {t("transcript.welcomeAssistantHint")}
      </p>
    </div>
  );
}

function ThreadSuggestions() {
  return (
    <div className="flex animate-in flex-wrap items-center justify-center gap-2 duration-200 fade-in slide-in-from-bottom-2">
      <ThreadPrimitive.Suggestions>
        {() => (
          <SuggestionPrimitive.Trigger send asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 rounded-full bg-background px-3 text-xs font-normal shadow-none"
            >
              <SuggestionPrimitive.Title />
            </Button>
          </SuggestionPrimitive.Trigger>
        )}
      </ThreadPrimitive.Suggestions>
    </div>
  );
}

function ThreadSurface({
  messages,
  publicRoom,
  assistantContext,
  connected,
  maxMessageLength = 2000,
  onScrolledChange,
  onReply,
  onDelete,
  onOpenThread,
  highlight,
}: Pick<
  ChatThreadProps,
  | "messages"
  | "assistantContext"
  | "connected"
  | "maxMessageLength"
  | "onScrolledChange"
  | "onReply"
  | "onDelete"
  | "onOpenThread"
  | "highlight"
> & { publicRoom: boolean }) {
  const { t } = useTranslation();
  const { username, typingUsers, customEmojis, onlineUsers } = useChatStore();
  const prepared = useMemo(
    () => prepareMessages((messages ?? []).slice(-MAX_RENDERED_MESSAGES), username),
    [messages, username],
  );
  const preparedById = useMemo(
    () => new Map(prepared.map((item) => [item.message.id, item])),
    [prepared],
  );
  const isEmpty = prepared.length === 0;
  const historyLoaded = useChatStore((state) => state.historyLoaded);
  const emojiPreprocess = useMemo(() => {
    if (customEmojis.length === 0) return null;
    const emojiMap = new Map(customEmojis.map((emoji) => [emoji.name, emoji.url]));
    const names = [...emojiMap.keys()].sort((a, b) => b.length - a.length);
    const escaped = names.map((name) => name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
    return {
      emojiMap,
      pattern: new RegExp(`:(${escaped.join("|")}):`, "g"),
    };
  }, [customEmojis]);

  // Only an overlapping footer inside the viewport contributes a scroll inset.
  // The public-room dock is a sibling, so its height is already excluded.
  const ComposerFooter = publicRoom ? "div" : ThreadPrimitive.ViewportFooter;
  const composerFooter = (
    <ComposerFooter
      data-visual={publicRoom ? "room-composer-dock" : "assistant-composer-footer"}
      className={cn(
        "relative flex flex-col gap-4 overflow-visible bg-card pb-4 pb-safe",
        publicRoom
          ? "mx-auto w-full max-w-4xl shrink-0 px-4 pt-2 sm:px-6"
          : isEmpty ? "mt-2" : "sticky bottom-0 rounded-t-2xl pt-2",
      )}
    >
      <ThreadPrimitive.ScrollToBottom asChild>
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={t("a11y.scrollToBottom")}
          className="absolute -top-12 z-10 size-9 self-center rounded-full bg-background shadow-sm disabled:invisible"
        >
          <ArrowDown className="size-4" />
        </Button>
      </ThreadPrimitive.ScrollToBottom>
      <ChatComposer
        assistantContext={assistantContext}
        connected={connected}
        maxMessageLength={maxMessageLength}
        publicRoom={publicRoom}
      />
      {!publicRoom && isEmpty && <ThreadSuggestions />}
    </ComposerFooter>
  );

  return (
    <ThreadPrimitive.Root
      className="flex h-full min-h-0 flex-col"
      data-visual={publicRoom ? "room-timeline" : "assistant-thread"}
    >
      <ThreadPrimitive.Viewport
        role="log"
        aria-label={t("chat.roomName")}
        autoScroll
        turnAnchor={publicRoom ? "bottom" : "top"}
        onScroll={(event) => onScrolledChange?.(event.currentTarget.scrollTop > 4)}
        className="relative flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto scroll-smooth scrollbar-thin"
      >
        <div
          className={cn(
            "mx-auto flex w-full max-w-4xl flex-1 flex-col px-4 pt-4 sm:px-6",
            publicRoom ? "justify-start" : isEmpty ? "justify-center" : "justify-end",
          )}
        >
          {isEmpty && (publicRoom ? (
            <p className="py-6 text-center text-xs text-muted-foreground" role="status">
              {historyLoaded ? t("transcript.emptyTitle") : t("transcript.loading")}
            </p>
          ) : <ThreadWelcome />)}

          <div className={cn("flex flex-col gap-0", !isEmpty && "mb-6")}>
            <ThreadPrimitive.Messages>
              {() => (
                <ChatMessageRow
                  preparedById={preparedById}
                  onReply={onReply}
                  onDelete={onDelete}
                  onOpenThread={onOpenThread}
                  highlight={highlight}
                  onlineUsers={onlineUsers}
                  emojiPreprocess={emojiPreprocess}
                />
              )}
            </ThreadPrimitive.Messages>
          </div>

          {publicRoom && typingUsers.length > 0 && (
            <div className="py-3 text-xs text-muted-foreground">
              <span className="animate-pulse">
                {t("transcript.typing", {
                  names: typingUsers.slice(0, 3).join("、"),
                })}
              </span>
            </div>
          )}

          {!publicRoom && composerFooter}
        </div>
      </ThreadPrimitive.Viewport>
      {publicRoom && composerFooter}
    </ThreadPrimitive.Root>
  );
}

export function ChatThread({
  messages,
  conversationKey,
  assistantContext,
  connected,
  maxMessageLength = 2000,
  onSend,
  onScrolledChange,
  onReply,
  onDelete,
  onOpenThread,
  highlight,
}: ChatThreadProps) {
  const { t } = useTranslation();
  const publicRoom = !assistantContext;
  const username = useChatStore((state) => state.username);
  const storeMessages = useChatStore((state) => state.messages);
  const visibleMessages = useMemo(
    () => (messages ?? storeMessages).slice(-MAX_RENDERED_MESSAGES).map((message) => publicRoom ? publicMessage(message) : message),
    [messages, storeMessages, publicRoom],
  );
  const runtimeMessages = useMemo<ThreadMessageLike[]>(
    () =>
      visibleMessages.map((message) => ({
        id: message.id,
        role: message.username === username ? "user" : "assistant",
        content: toRuntimeContent(message),
        createdAt: new Date(message.timestamp),
        ...(message.username === username
          ? {}
          : {
              status: isStreamingMessage(message)
                ? ({ type: "running" } as const)
                : ({ type: "complete", reason: "stop" } as const),
            }),
        metadata: { custom: { messageId: message.id } },
      })),
    [username, visibleMessages],
  );

  const suggestions = useMemo(
    () =>
      publicRoom
        ? []
        : [
            {
              title: t("transcript.suggestCapabilities"),
              prompt: t("transcript.suggestCapabilities"),
            },
            {
              title: t("transcript.suggestCode"),
              prompt: t("transcript.suggestCode"),
            },
            {
              title: t("transcript.suggestSummary"),
              prompt: t("transcript.suggestSummary"),
            },
          ],
    [publicRoom, t],
  );

  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages: runtimeMessages,
    convertMessage: (message) => message,
    isRunning: false,
    isDisabled: !connected,
    isSendDisabled: !connected,
    onNew: async (message) => {
      const content = messageText(message).trim();
      if (content) onSend(content);
    },
    suggestions,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime} key={conversationKey}>
      <ThreadSurface
        messages={visibleMessages}
        publicRoom={publicRoom}
        assistantContext={assistantContext}
        connected={connected}
        maxMessageLength={maxMessageLength}
        onScrolledChange={onScrolledChange}
        onReply={onReply}
        onDelete={onDelete}
        onOpenThread={onOpenThread}
        highlight={highlight}
      />
    </AssistantRuntimeProvider>
  );
}
