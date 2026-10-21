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
import { ArrowDown, ArrowUp, Image as ImageIcon, Plus, Smile, Square } from "lucide-react";
import { toast } from "sonner";
import { BotRunStrip } from "@/components/BotRunStrip";
import { isStreamingMessage } from "@/stores/botRunState";
import { MessageBubble } from "@/components/MessageBubble";
import { AssistantIcon } from "@/components/AssistantIcon";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Spinner } from "@/components/ui/spinner";
import { useTranslation } from "@/i18n/context";
import { assistants, type AssistantDefinition } from "@/lib/assistantRegistry";
import { chatAPI, type ChatMessage } from "@/lib/api";
import { systemMessageText, withVisibleReplyPreview } from "@/lib/messagePresentation";
import { cn, formatDate, formatTime } from "@/lib/utils";
import { useChatStore } from "@/stores/chatStore";

const EmojiPicker = lazy(() =>
  import("@/components/EmojiPicker").then((m) => ({ default: m.EmojiPicker })),
);

const GROUP_WINDOW_MS = 2 * 60 * 1000;
/** Muse-style time markers break a quiet 1:1 thread without a stamp per row. */
const TIME_MARKER_GAP_MS = 5 * 60 * 1000;
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
  showTime: boolean;
  replyCount: number;
}

interface AssistantContext {
  assistant: AssistantDefinition;
  modelLabel: string;
}

interface ChatThreadProps {
  messages?: ChatMessage[];
  /** Subscribe to the private assistant transcript when no `messages` prop is given. */
  usePrivateMessages?: boolean;
  conversationKey: string;
  assistantContext?: AssistantContext | null;
  connected: boolean;
  maxMessageLength?: number;
  onSend: (content: string) => void;
  /** True while this conversation's assistant run is active. */
  isRunning?: boolean;
  /** Cancels the current run; enables the composer's cancel primitive. */
  onCancelRun?: () => void;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onRetryMessage?: (message: ChatMessage) => void;
  onOpenThread?: (message: ChatMessage) => void;
  highlight?: string;
  onScrolledChange?: (scrolled: boolean) => void;
}

export function prepareMessages(messages: ChatMessage[], username: string, minimal = false): PreparedMessage[] {
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

    const newDay = !previous ||
      new Date(previous.timestamp).toDateString() !== new Date(message.timestamp).toDateString();

    return {
      message,
      isOwn: message.username === username,
      groupPosition,
      // In the private assistant thread identity chrome is noise: the only
      // two voices are "you" and the assistant, told apart by alignment.
      hideAvatar: minimal || continuesGroup,
      hideUsername: minimal || continuesGroup,
      forceShowTimestamp: !minimal && !nextContinuesGroup,
      isNew: message.id.startsWith("optimistic_") || message.timestamp > Date.now() - 2000,
      showDate: !previous || newDay,
      showTime: !newDay && (!previous || message.timestamp - previous.timestamp > TIME_MARKER_GAP_MS),
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
  minimal,
  onReply,
  onDelete,
  onRetryMessage,
  onOpenThread,
  highlight,
  onlineUsers,
  emojiPreprocess,
}: {
  preparedById: Map<string, PreparedMessage>;
  minimal: boolean;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onRetryMessage?: (message: ChatMessage) => void;
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
          {minimal && prepared.showTime && (
            <div className="flex justify-center py-3">
              <span className="text-label tabular-nums text-muted-foreground/50">
                {formatTime(prepared.message.timestamp, t)}
              </span>
            </div>
          )}
          <MessageBubble
            message={prepared.message}
            isOwn={prepared.isOwn}
            minimal={minimal}
            hideAvatar={prepared.hideAvatar}
            hideUsername={prepared.hideUsername}
            forceShowTimestamp={prepared.forceShowTimestamp}
            isGrouped={prepared.groupPosition !== "single"}
            groupPosition={prepared.groupPosition}
            isNew={prepared.isNew}
            replyCount={prepared.replyCount}
            onReply={onReply}
            onDelete={onDelete}
            onRetry={onRetryMessage}
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
  canCancel,
}: {
  assistantContext?: AssistantContext | null;
  connected: boolean;
  maxMessageLength: number;
  publicRoom: boolean;
  canCancel: boolean;
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
  const [uploadingImage, setUploadingImage] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);

  const uploadImage = useCallback(async (file?: File) => {
    if (!file) return;
    if (file.size > 5 * 1024 * 1024) {
      toast.error(t("input.imageTooLarge"));
      return;
    }
    setUploadingImage(true);
    const result = await chatAPI.uploadImage(file);
    setUploadingImage(false);
    if (!result.url) {
      toast.error(t("input.imageUploadFailed"));
      return;
    }
    const value = inputRef.current?.value ?? text;
    const separator = value && !value.endsWith(" ") ? " " : "";
    composer.setText(`${value}${separator}![${t("input.image")}](${result.url}) `);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [composer, t, text]);
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
      <BotRunStrip privateChat={!publicRoom} connected={connected} showStop={publicRoom} />
      <div
        data-slot="composer-shell"
        data-visual="composer-body"
        className="relative flex min-h-[56px] w-full cursor-text items-center gap-1 rounded-[32px] border border-foreground/10 bg-card pl-3 pr-4 transition-[border-color,box-shadow] focus-within:border-foreground/25 focus-within:shadow-[0_2px_8px_rgba(17,17,18,0.06)] shadow-[0_2px_8px_rgba(17,17,18,0.05),0_2px_16px_rgba(17,17,18,0.03)]"
      >
        <MentionPopover
          candidates={mentionCandidates}
          activeIndex={mentionIndex}
          onSelect={insertMention}
        />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              disabled={uploadingImage}
              aria-label={t("input.attach")}
              data-visual="composer-attach"
              className="size-11 shrink-0 rounded-full text-muted-foreground sm:size-8"
            >
              {uploadingImage ? <Spinner className="size-4" /> : <Plus className="size-[18px]" />}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="start" sideOffset={8} className="w-40 rounded-lg p-1">
            <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => setEmojiOpen(true)}>
              <Smile className="size-4" />
              {t("emoji.openPicker")}
            </DropdownMenuItem>
            <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => imageInputRef.current?.click()}>
              <ImageIcon className="size-4" />
              {t("input.image")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <input
          ref={imageInputRef}
          type="file"
          accept="image/png,image/jpeg"
          className="hidden"
          data-visual="composer-image-input"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            void uploadImage(file);
          }}
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
          className="max-h-48 min-h-10 min-w-0 flex-1 resize-none bg-transparent px-1 py-2 text-[15px] leading-6 outline-none placeholder:text-muted-foreground/60"
        />

        {assistantContext && (
          <div
            data-visual="composer-ai-context"
            className="flex min-w-0 shrink-0 items-center gap-1.5 px-1 text-label text-muted-foreground"
          >
            <AssistantIcon assistant={assistantContext.assistant} size="sm" />
            <span className="hidden max-w-32 truncate font-medium text-foreground sm:inline">
              {assistantContext.assistant.name}
            </span>
          </div>
        )}

        {text.length > maxMessageLength * 0.8 && (
          <span
            className={cn(
              "shrink-0 text-[11px] tabular-nums",
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

        {canCancel ? (
          <ComposerPrimitive.Cancel asChild>
            <Button
              type="button"
              size="icon"
              disabled={!connected}
              aria-label={t("botRun.stop")}
              data-visual="composer-stop"
              className="size-11 shrink-0 rounded-full sm:size-8"
            >
              <Square className="size-3.5" />
            </Button>
          </ComposerPrimitive.Cancel>
        ) : (
          <ComposerPrimitive.Send asChild>
            <Button
              type="submit"
              size="icon"
              disabled={!connected || !canSend}
              aria-label={t("input.send")}
              data-visual="composer-send"
              className="size-11 shrink-0 rounded-full sm:size-8"
            >
              <ArrowUp className="size-4" />
            </Button>
          </ComposerPrimitive.Send>
        )}
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
    <div className="flex animate-in flex-wrap items-center justify-center gap-2 duration-200 fade-in slide-in-from-bottom-2 motion-reduce:animate-none">
      <ThreadPrimitive.Suggestions>
        {() => (
          <SuggestionPrimitive.Trigger send asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-11 rounded-full bg-background px-3.5 text-xs font-normal shadow-none sm:h-8 sm:px-3"
            >
              <SuggestionPrimitive.Title />
            </Button>
          </SuggestionPrimitive.Trigger>
        )}
      </ThreadPrimitive.Suggestions>
    </div>
  );
}

function ThreadSuggestionsSlot() {
  // Official guidance: welcome suggestions belong to a new chat with an empty
  // composer. Subscribe here so typing does not re-render the transcript.
  const composerEmpty = useAuiState((state) => state.composer.isEmpty);
  return composerEmpty ? <ThreadSuggestions /> : null;
}

function AssistantTypingBubble() {
  const { t } = useTranslation();
  return (
    <div className="flex justify-start py-1.5" role="status" aria-label={t("message.generating")}>
      <div className="flex min-h-[44px] animate-in items-center gap-1 rounded-[22px] bg-[var(--chat-agent-bubble)] px-[15px] py-[11px] fade-in duration-150 motion-reduce:animate-none">
        {[0, 160, 320].map((delay) => (
          <span
            key={delay}
            aria-hidden="true"
            className="size-1.5 animate-pulse rounded-full bg-muted-foreground/60 motion-reduce:animate-none"
            style={{ animationDelay: `${delay}ms` }}
          />
        ))}
      </div>
    </div>
  );
}

function TypingIndicator() {
  const { t } = useTranslation();
  const typingUsers = useChatStore((state) => state.typingUsers);
  if (typingUsers.length === 0) return null;
  return (
    <div className="py-3 text-xs text-muted-foreground">
      <span className="animate-pulse">
        {t("transcript.typing", {
          names: typingUsers.slice(0, 3).join("、"),
        })}
      </span>
    </div>
  );
}

function ThreadSurface({
  messages,
  publicRoom,
  assistantContext,
  connected,
  maxMessageLength = 2000,
  isRunning = false,
  onCancelRun,
  onScrolledChange,
  onReply,
  onDelete,
  onRetryMessage,
  onOpenThread,
  highlight,
}: Pick<
  ChatThreadProps,
  | "messages"
  | "assistantContext"
  | "connected"
  | "maxMessageLength"
  | "isRunning"
  | "onCancelRun"
  | "onScrolledChange"
  | "onReply"
  | "onDelete"
  | "onRetryMessage"
  | "onOpenThread"
  | "highlight"
> & { publicRoom: boolean }) {
  const { t } = useTranslation();
  // Granular subscriptions: presence, typing and profile churn must not
  // re-render the transcript while a reply is streaming.
  const username = useChatStore((state) => state.username);
  const customEmojis = useChatStore((state) => state.customEmojis);
  const onlineUsers = useChatStore((state) => state.onlineUsers);
  const prepared = useMemo(
    () => prepareMessages((messages ?? []).slice(-MAX_RENDERED_MESSAGES), username, !publicRoom),
    [messages, username, publicRoom],
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
        "relative flex flex-col gap-4 overflow-visible bg-card pb-4 pb-safe dark:bg-background",
        publicRoom
          ? "mx-auto w-full max-w-3xl shrink-0 px-4 pt-2 sm:px-6"
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
        canCancel={isRunning && Boolean(onCancelRun)}
      />
      {!publicRoom && isEmpty && <ThreadSuggestionsSlot />}
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
        className="relative flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto scrollbar-thin"
      >
        <div
          className={cn(
            "mx-auto flex w-full max-w-3xl flex-1 flex-col px-4 pt-4 sm:px-6",
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
                  minimal={!publicRoom}
                  onReply={onReply}
                  onDelete={onDelete}
                  onRetryMessage={onRetryMessage}
                  onOpenThread={onOpenThread}
                  highlight={highlight}
                  onlineUsers={onlineUsers}
                  emojiPreprocess={emojiPreprocess}
                />
              )}
            </ThreadPrimitive.Messages>
          </div>

          {!publicRoom && isRunning && !prepared.some((item) => isStreamingMessage(item.message)) && (
            <AssistantTypingBubble />
          )}

          {publicRoom && <TypingIndicator />}

          {!publicRoom && composerFooter}
        </div>
      </ThreadPrimitive.Viewport>
      {publicRoom && composerFooter}
    </ThreadPrimitive.Root>
  );
}

export function ChatThread({
  messages,
  usePrivateMessages = false,
  conversationKey,
  assistantContext,
  connected,
  maxMessageLength = 2000,
  onSend,
  isRunning = false,
  onCancelRun,
  onScrolledChange,
  onReply,
  onDelete,
  onRetryMessage,
  onOpenThread,
  highlight,
}: ChatThreadProps) {
  const { t } = useTranslation();
  const publicRoom = !assistantContext;
  const username = useChatStore((state) => state.username);
  // Subscribe to one transcript only: a private stream must not re-render the
  // public room (or the reverse).
  const storeMessages = useChatStore((state) =>
    usePrivateMessages ? state.privateBotMessages : state.messages,
  );
  const sourceMessages = messages ?? storeMessages;
  // Keep row identity stable across stream deltas. Sanitising parts here used
  // to clone every assistant message on every token, defeating MessageBubble's
  // memo and re-parsing the whole transcript for each delta.
  const visibleMessages = useMemo(
    () => sourceMessages.slice(-MAX_RENDERED_MESSAGES),
    [sourceMessages],
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
    isRunning,
    isDisabled: !connected,
    isSendDisabled: !connected,
    ...(onCancelRun ? { onCancel: async () => onCancelRun() } : {}),
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
        isRunning={isRunning}
        onCancelRun={onCancelRun}
        onScrolledChange={onScrolledChange}
        onReply={onReply}
        onDelete={onDelete}
        onRetryMessage={onRetryMessage}
        onOpenThread={onOpenThread}
        highlight={highlight}
      />
    </AssistantRuntimeProvider>
  );
}
