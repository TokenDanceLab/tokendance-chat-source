import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import {
  AssistantRuntimeProvider,
  AttachmentPrimitive,
  ComposerPrimitive,
  SuggestionPrimitive,
  ThreadPrimitive,
  useAui,
  useAuiState,
  useExternalStoreRuntime,
  type Attachment,
  type AttachmentAdapter,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { ArrowDown, ArrowUp, Image as ImageIcon, Mic, Plus, Reply, Smile, Square, X } from "lucide-react";
import { toast } from "sonner";
import { BotRunTimeline, hasVisibleAssistantText } from "@/components/BotRunTimeline";
import { useSpeechInput } from "@/hooks/useSpeechInput";
import { isStreamingMessage } from "@/stores/botRunState";
import { MessageBubble } from "@/components/MessageBubble";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
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
import { CHAT_EVENTS } from "@/lib/events";
import { systemMessageText, withVisibleReplyPreview } from "@/lib/messagePresentation";
import { cn, formatDate, formatTime, isImeComposing, plainTextPreview } from "@/lib/utils";
import { useChatStore, privateLaneOf } from "@/stores/chatStore";

const EmojiPicker = lazy(() =>
  import("@/components/EmojiPicker").then((m) => ({ default: m.EmojiPicker })),
);

const GROUP_WINDOW_MS = 2 * 60 * 1000;
/** Muse-style time markers break a quiet 1:1 thread without a stamp per row. */
const TIME_MARKER_GAP_MS = 5 * 60 * 1000;
const MAX_RENDERED_MESSAGES = 200;
const CHAT_THREAD_MODULE_LOADED_AT = Date.now();

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
  onSend: (content: string, threadId?: string) => void;
  /** True while this conversation's assistant run is active. */
  isRunning?: boolean;
  /** Cancels the current run; enables the composer's cancel primitive. */
  onCancelRun?: () => void;
  cancelPending?: boolean;
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
      // A refresh must render its recent history in place. Only a message born
      // after this page mounted (or an optimistic row) earns an entrance.
      isNew: message.id.startsWith("optimistic_") ||
        message.timestamp >= CHAT_THREAD_MODULE_LOADED_AT,
      showDate: !previous || newDay,
      showTime: !newDay && (!previous || message.timestamp - previous.timestamp > TIME_MARKER_GAP_MS),
      replyCount: replyCounts.get(message.id) ?? 0,
    };
  });
}

/**
 * assistant-ui treats a changed runtime message id as a different row and
 * remounts it. Optimistic messages keep client_message_id when the server echo
 * replaces them, so use that stable identity while MessageBubble continues to
 * address the persisted server id through metadata.
 */
export function runtimeMessageId(message: ChatMessage): string {
  return message.client_message_id
    ? `client:${message.client_message_id}`
    : `message:${message.id}`;
}

function messageText(message: ThreadMessageLike): string {
  if (typeof message.content === "string") return message.content;
  return message.content
    .filter((part) => part.type === "text")
    .map((part) => ("text" in part ? part.text : ""))
    .join("");
}

function SystemNotice({
  message,
  onRetry,
}: {
  message: ChatMessage;
  onRetry?: (content: string, threadId?: string) => void;
}) {
  const { t } = useTranslation();
  let retryContent: string | undefined;
  if (message.id.startsWith("run-failed-") || message.id.startsWith("run-stopped-")) {
    try {
      const parsed = JSON.parse(message.content) as { params?: { content?: unknown } };
      if (typeof parsed.params?.content === "string" && parsed.params.content.trim()) {
        retryContent = parsed.params.content;
      }
    } catch { /* plain-text system notice */ }
  }
  return (
    <div className="flex justify-center py-2">
      <span className="flex flex-wrap items-center justify-center gap-1 text-xs text-muted-foreground">
        {systemMessageText(message.content, t)}
        {retryContent && onRetry && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-visual="run-retry"
            className="h-6 px-1.5 text-label"
            onClick={() => onRetry(retryContent!, message.thread_id)}
          >
            {t("message.retry")}
          </Button>
        )}
      </span>
    </div>
  );
}

function ChatMessageRow({
  prepared,
  minimal,
  onReply,
  onDelete,
  onRetryMessage,
  onSend,
  onOpenThread,
  highlight,
  onlineUsers,
  emojiPreprocess,
}: {
  prepared: PreparedMessage;
  minimal: boolean;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onRetryMessage?: (message: ChatMessage) => void;
  onSend?: (content: string, threadId?: string) => void;
  onOpenThread?: (message: ChatMessage) => void;
  highlight?: string;
  onlineUsers: string[];
  emojiPreprocess: { emojiMap: Map<string, string>; pattern: RegExp } | null;
}) {
  const { t } = useTranslation();
  // The waiting bubble owns this stage. A source/reasoning-only draft must
  // not create an empty second bubble before the first visible answer text.
  if (isStreamingMessage(prepared.message) && !hasVisibleAssistantText(prepared.message)) return null;

  return (
    <div
      data-message-id={prepared.message.id}
      data-role={prepared.message.username === "system" ? "system" : prepared.isOwn ? "user" : "assistant"}
      className="min-w-0"
    >
      {prepared.message.username === "system" ? (
        <SystemNotice message={prepared.message} onRetry={onSend} />
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
            onRetryStopped={onSend}
            onOpenThread={onOpenThread}
            highlight={highlight}
            onlineUsers={onlineUsers}
            emojiPreprocess={emojiPreprocess}
          />
        </>
      )}
    </div>
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
  const userProfiles = useChatStore((state) => state.userProfiles);
  if (candidates.length === 0) return null;
  return (
    <div
      role="listbox"
      aria-label={t("input.mentionSuggestions")}
      className="absolute bottom-full left-2 right-2 z-30 mb-2 max-h-56 overflow-y-auto rounded-xl border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {candidates.map((candidate, index) => {
        const profile = userProfiles[candidate];
        return (
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
            <span aria-hidden="true">
              <ParticipantAvatar username={candidate} size="sm" className="size-6 [&_[data-slot=avatar]]:size-6" />
            </span>
            <span className="truncate">{profile?.display_name || candidate}</span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * Muse-style attachment previews: rounded thumbnails with a remove affordance
 * inside the composer. The image itself travels as an assistant-ui attachment
 * (uploaded by the runtime adapter) and only turns into Markdown at send time.
 */
function ComposerAttachmentStrip() {
  const count = useAuiState((state) => state.composer.attachments.length);
  if (count === 0) return null;
  return (
    <div data-visual="composer-attachments" className="mb-2 flex flex-wrap gap-2">
      <ComposerPrimitive.Attachments>
        {({ attachment }) => <ComposerAttachmentCard attachment={attachment} />}
      </ComposerPrimitive.Attachments>
    </div>
  );
}

function ComposerAttachmentCard({ attachment }: { attachment: Attachment }) {
  const { t } = useTranslation();
  const imagePart = attachment.content?.find((part) => part.type === "image");
  const src = imagePart && "image" in imagePart ? imagePart.image : undefined;
  const uploading = attachment.status.type === "running";
  return (
    <div
      data-visual="composer-attachment"
      className="relative size-14 overflow-hidden rounded-xl border border-foreground/10 bg-muted"
    >
      {src ? (
        <img src={src} alt={attachment.name} className="size-full object-cover" />
      ) : (
        <span className="flex size-full items-center justify-center text-muted-foreground">
          <ImageIcon className="size-4" aria-hidden="true" />
        </span>
      )}
      {uploading && (
        <span className="absolute inset-0 grid place-items-center bg-background/50">
          <Spinner className="size-4" />
        </span>
      )}
      <AttachmentPrimitive.Remove
        aria-label={t("input.removeImage")}
        title={t("input.removeImage")}
        className="absolute right-1 top-1 flex size-5 items-center justify-center rounded-full bg-black/60 text-white transition-colors before:absolute before:-inset-2 before:content-[''] hover:bg-black/80 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-white/80"
      >
        <X className="size-3" aria-hidden="true" />
      </AttachmentPrimitive.Remove>
    </div>
  );
}

function ChatComposer({
  assistantContext,
  connected,
  maxMessageLength,
  publicRoom,
  conversationKey,
  canCancel,
  cancelPending,
  onCancelRun,
}: {
  assistantContext?: AssistantContext | null;
  connected: boolean;
  maxMessageLength: number;
  publicRoom: boolean;
  conversationKey: string;
  canCancel: boolean;
  cancelPending: boolean;
  onCancelRun?: () => void;
}) {
  const { t, lang } = useTranslation();
  const composer = useAui().composer;
  const text = useAuiState((state) => state.composer.text);
  const draftKey = `tdchat-draft:${conversationKey}`;
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
  const inputRevision = useRef(0);
  const imageInputRef = useRef<HTMLInputElement>(null);

  // Drafts survive lane switches and reloads, but stay scoped to one
  // conversation key so they can never leak into another lane.
  useEffect(() => {
    const saved = localStorage.getItem(draftKey);
    if (saved) composer.setText(saved);
  }, [composer, draftKey]);
  useEffect(() => {
    const timer = setTimeout(() => {
      if (text) localStorage.setItem(draftKey, text);
      else localStorage.removeItem(draftKey);
    }, 250);
    return () => clearTimeout(timer);
  }, [draftKey, text]);

  const appendTranscript = useCallback(
    (transcript: string) => {
      const value = inputRef.current?.value ?? text;
      const separator = value && !value.endsWith(" ") ? " " : "";
      composer.setText(`${value}${separator}${transcript}`);
      requestAnimationFrame(() => inputRef.current?.focus());
    },
    [composer, text],
  );
  const speech = useSpeechInput(appendTranscript, lang, () =>
    toast.error(t("input.voiceFailed")),
  );

  const uploadImage = useCallback(async (file?: File) => {
    if (!file) return;
    setUploadingImage(true);
    // The runtime's attachment adapter validates, uploads and renders the
    // thumbnail; the composer text never carries raw Markdown for images.
    try {
      await composer.addAttachment(file);
    } catch {
      // The adapter already surfaced the failure as a toast.
    } finally {
      setUploadingImage(false);
    }
  }, [composer]);

  const onlineUsers = useChatStore((state) => state.onlineUsers);
  const username = useChatStore((state) => state.username);
  const replyTo = useChatStore((state) => state.replyTo);
  const setReplyTo = useChatStore((state) => state.setReplyTo);

  const mentionCandidates = useMemo(() => {
    if (!publicRoom || !mentionQuery) return [];
    const query = mentionQuery.query.normalize("NFKC").toLowerCase();
    const self = username.normalize("NFKC").toLowerCase();
    const users = [...onlineUsers, ...assistants.map((assistant) => assistant.name)];
    const seen = new Set<string>();
    return users.filter((user) => {
      const canonical = user.normalize("NFKC").toLowerCase();
      if (canonical === self || seen.has(canonical) || !canonical.startsWith(query)) {
        return false;
      }
      seen.add(canonical);
      return true;
    }).slice(0, 6);
  }, [mentionQuery, onlineUsers, publicRoom, username]);

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
      const input = inputRef.current;
      if (!input) return;
      // A menu click may arrive before React commits the most recent query.
      // Resolve its replacement range from the live textarea, not the render
      // that created the option (especially after rapid keyboard selection).
      const value = input.value;
      const end = input.selectionStart;
      const match = value.slice(0, end).match(/(?:^|\s)@([\p{L}\p{N}_]*)$/u);
      if (!match) return;
      const start = end - match[1].length - 1;
      const next = `${value.slice(0, start)}@${username} ${value.slice(end)}`;
      const revision = ++inputRevision.current;
      composer.setText(next);
      setMentionQuery(null);
      setMentionIndex(0);
      requestAnimationFrame(() => {
        const current = inputRef.current;
        // A newer edit/selection supersedes this cursor restoration.
        if (!current || inputRevision.current !== revision || current.value !== next) return;
        current.focus();
        const cursor = start + username.length + 2;
        current.setSelectionRange(cursor, cursor);
      });
    },
    [composer],
  );

  const handleInputChange = useCallback(
    (event: React.ChangeEvent<HTMLTextAreaElement>) => {
      inputRevision.current++;
      updateMention(event.currentTarget.value, event.currentTarget.selectionStart);
    },
    [updateMention],
  );

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTextAreaElement>) => {
      // Chinese/Japanese/Korean IME uses Enter to confirm a candidate. Never
      // turn that keystroke into a mention selection.
      if (isImeComposing(event)) return;
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
      <ComposerAttachmentStrip />
      {replyTo && (
        <div
          data-visual="composer-reply"
          className="mb-2 flex min-h-11 items-center gap-2 rounded-xl bg-muted/60 px-3 py-1.5 sm:min-h-9"
        >
          <Reply className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <p className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{replyTo.username}</span>
            <span className="mx-1">·</span>
            {plainTextPreview(replyTo.content, 80)}
          </p>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={t("input.cancel")}
            className="size-11 shrink-0 rounded-full text-muted-foreground sm:size-7"
            onClick={() => setReplyTo(null)}
          >
            <X className="size-3.5" />
          </Button>
        </div>
      )}
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
          onSelect={() => { inputRevision.current++; }}
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
            <ParticipantAvatar username={assistantContext.assistant.name} assistant={assistantContext.assistant} size="sm" />
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

        {canCancel && publicRoom ? (
          <Button type="button" size="icon" disabled={!connected || cancelPending}
            aria-label={t(cancelPending ? "botRun.stopping" : "botRun.stop")}
            data-visual="composer-stop" className="size-11 shrink-0 rounded-full bg-[var(--chat-own)] text-[var(--chat-own-foreground)] hover:bg-[var(--chat-own)] sm:size-8"
            onClick={() => { if (connected && !cancelPending) onCancelRun?.(); }}>
            <Square className="size-3.5 fill-current" aria-hidden="true" />
          </Button>
        ) : canCancel ? (
          <ComposerPrimitive.Cancel asChild>
            <Button
              type="button"
              size="icon"
              disabled={!connected || cancelPending}
              aria-label={t(cancelPending ? "botRun.stopping" : "botRun.stop")}
              data-visual="composer-stop"
              className="size-11 shrink-0 rounded-full bg-[var(--chat-own)] text-[var(--chat-own-foreground)] hover:bg-[var(--chat-own)] sm:size-8"
            >
              <Square className="size-3.5 fill-current" aria-hidden="true" />
            </Button>
          </ComposerPrimitive.Cancel>
        ) : canSend ? (
          <ComposerPrimitive.Send asChild>
            <Button
              type="submit"
              size="icon"
              disabled={!connected || !canSend}
              aria-label={t("input.send")}
              data-visual="composer-send"
              className="size-11 shrink-0 rounded-full sm:size-8"
              onClick={() => speech.stop()}
            >
              <ArrowUp className="size-4" />
            </Button>
          </ComposerPrimitive.Send>
        ) : speech.supported ? (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={speech.listening ? t("input.voiceStop") : t("input.voiceStart")}
            aria-pressed={speech.listening}
            data-visual="composer-voice"
            onClick={speech.listening ? speech.stop : speech.start}
            className={cn(
              "size-11 shrink-0 rounded-full text-muted-foreground sm:size-8",
              speech.listening && "bg-primary text-primary-foreground hover:bg-primary/90",
            )}
          >
            {speech.listening ? <Square className="size-3.5" /> : <Mic className="size-4" />}
          </Button>
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

function TypingIndicator() {
  const { t } = useTranslation();
  const allTypingUsers = useChatStore((state) => state.typingUsers);
  const typingUsers = allTypingUsers.filter((username) => !assistants.some((assistant) => assistant.name === username));
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
  conversationKey,
  assistantContext,
  connected,
  maxMessageLength = 2000,
  isRunning = false,
  onCancelRun,
  cancelPending = false,
  onScrolledChange,
  onReply,
  onDelete,
  onRetryMessage,
  onSend,
  onOpenThread,
  highlight,
}: Pick<
  ChatThreadProps,
  | "messages"
  | "conversationKey"
  | "assistantContext"
  | "connected"
  | "maxMessageLength"
  | "isRunning"
  | "onCancelRun"
  | "cancelPending"
  | "onScrolledChange"
  | "onReply"
  | "onDelete"
  | "onRetryMessage"
  | "onSend"
  | "onOpenThread"
  | "highlight"
> & { publicRoom: boolean }) {
  const transcriptMessages = messages ?? [];
  // --- Older pages and jump targets -------------------------------------
  // The assistant-ui rewrite kept the event producers but dropped these two
  // consumers, which made long rooms a dead end and search hits inert.
  const viewportRef = useRef<HTMLDivElement>(null);
  const loadingOlderRef = useRef(false);
  const noMoreHistoryRef = useRef(false);
  const pendingPrependRef = useRef(0);
  // A search jump may need several history pages before the target exists.
  const pendingJumpRef = useRef<string | null>(null);
  const jumpingRef = useRef(false);

  useEffect(() => {
    const stop = () => {
      noMoreHistoryRef.current = true;
      loadingOlderRef.current = false;
    };
    window.addEventListener(CHAT_EVENTS.noMoreHistory, stop);
    return () => window.removeEventListener(CHAT_EVENTS.noMoreHistory, stop);
  }, []);

  const scrollToTarget = useCallback((id: string) => {
    const viewport = viewportRef.current;
    const target = document.getElementById(`msg-${id}`) ??
      document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`);
    if (!viewport || !target) return false;
    const vr = viewport.getBoundingClientRect();
    const tr = target.getBoundingClientRect();
    const top = viewport.scrollTop + (tr.top - vr.top) - (viewport.clientHeight - tr.height) / 2;
    jumpingRef.current = true;
    viewport.scrollTo({ top: Math.max(0, top), behavior: "smooth" });
    window.setTimeout(() => { jumpingRef.current = false; }, 1200);
    target.classList.add("bg-primary/[0.06]");
    window.setTimeout(() => target.classList.remove("bg-primary/[0.06]"), 2400);
    return true;
  }, []);

  // Search can hit a message that is not loaded yet. Keep paging history until
  // the target exists, then scroll it into view instead of silently doing
  // nothing. While jumping, the scroll handler must not treat the smooth
  // animation as "user reached the top" and prepend another page.
  useEffect(() => {
    const tryPending = () => {
      const id = pendingJumpRef.current;
      if (!id) return;
      if (scrollToTarget(id)) {
        pendingJumpRef.current = null;
        return;
      }
      const viewport = viewportRef.current;
      const oldest = transcriptMessages[0];
      if (!publicRoom || !viewport || !oldest || loadingOlderRef.current || noMoreHistoryRef.current) {
        pendingJumpRef.current = null;
        return;
      }
      loadingOlderRef.current = true;
      pendingPrependRef.current = viewport.scrollHeight;
      chatAPI.sendLoadHistory(oldest.timestamp);
    };
    tryPending();
    const handler = (event: Event) => {
      const id = (event as CustomEvent<{ id?: string }>).detail?.id;
      if (!id) return;
      pendingJumpRef.current = id;
      tryPending();
    };
    window.addEventListener(CHAT_EVENTS.scrollToMessage, handler);
    return () => window.removeEventListener(CHAT_EVENTS.scrollToMessage, handler);
  }, [publicRoom, scrollToTarget, transcriptMessages]);

  // Keep the reader's position when an older page is prepended above them.
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport || pendingPrependRef.current === 0) return;
    const delta = viewport.scrollHeight - pendingPrependRef.current;
    pendingPrependRef.current = 0;
    loadingOlderRef.current = false;
    if (delta > 0) viewport.scrollTop += delta;
  }, [transcriptMessages.length]);

  const handleViewportScroll = useCallback((event: React.UIEvent<HTMLElement>) => {
    const viewport = event.currentTarget;
    onScrolledChange?.(viewport.scrollTop > 4);
    if (jumpingRef.current) return;
    // Public rooms page through history over the WebSocket; private lanes load
    // their snapshot in one go and have no older-page protocol yet.
    if (!publicRoom || loadingOlderRef.current || noMoreHistoryRef.current) return;
    if (viewport.scrollTop > 80 || transcriptMessages.length === 0) return;
    const oldest = transcriptMessages[0];
    if (!oldest) return;
    loadingOlderRef.current = true;
    pendingPrependRef.current = viewport.scrollHeight;
    chatAPI.sendLoadHistory(oldest.timestamp);
    window.setTimeout(() => { loadingOlderRef.current = false; }, 5000);
  }, [onScrolledChange, publicRoom, transcriptMessages]);
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

  // The composer dock is a sibling of the scrolling viewport in both modes.
  // Anchoring it to the shell (instead of centring it with the empty-state
  // hero) keeps its position fixed while typing, when the suggestion chips
  // disappear, and when the mobile keyboard resizes the visual viewport.
  const composerFooter = (
    <div
      data-visual={publicRoom ? "room-composer-dock" : "assistant-composer-footer"}
      className="relative mx-auto flex w-full max-w-3xl shrink-0 flex-col gap-3 overflow-visible bg-card px-4 pt-2 pb-4 pb-safe sm:px-6 dark:bg-background"
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
      {/* Suggestions sit above the composer so hiding them (first keystroke)
          shrinks the dock upward and never moves the input. */}
      {!publicRoom && isEmpty && <ThreadSuggestionsSlot />}
      <ChatComposer
        assistantContext={assistantContext}
        connected={connected}
        maxMessageLength={maxMessageLength}
        publicRoom={publicRoom}
        conversationKey={conversationKey}
        canCancel={isRunning && Boolean(onCancelRun)}
        cancelPending={cancelPending}
        onCancelRun={onCancelRun}
      />
    </div>
  );

  return (
    <ThreadPrimitive.Root
      className="flex h-full min-h-0 flex-col"
      data-visual={publicRoom ? "room-timeline" : "assistant-thread"}
    >
      <ThreadPrimitive.Viewport
        ref={viewportRef}
        role="log"
        aria-label={publicRoom ? t("chat.roomName") : (assistantContext?.assistant.name ?? t("chat.roomName"))}
        autoScroll
        turnAnchor={publicRoom ? "bottom" : "top"}
        onScroll={handleViewportScroll}
        className="relative flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto scrollbar-thin"
      >
        <div
          className={cn(
            "mx-auto flex w-full max-w-3xl flex-1 flex-col px-4 pt-4 sm:px-6",
            // The empty hero centres in the space above the fixed composer
            // dock; the transcript grows bottom-up once messages exist.
            publicRoom ? "justify-start" : isEmpty ? "justify-center" : "justify-end",
          )}
        >
          {isEmpty && (publicRoom ? (
            <p className="py-6 text-center text-xs text-muted-foreground" role="status">
              {historyLoaded ? t("transcript.emptyTitle") : t("transcript.loading")}
            </p>
          ) : <ThreadWelcome />)}

          <div className={cn("flex flex-col gap-0", !isEmpty && "mb-6")}>
            {prepared.map((item) => (
              <ChatMessageRow
                key={runtimeMessageId(item.message)}
                prepared={item}
                minimal={!publicRoom}
                onReply={onReply}
                onDelete={onDelete}
                onRetryMessage={onRetryMessage}
                onSend={onSend}
                onOpenThread={onOpenThread}
                highlight={highlight}
                onlineUsers={onlineUsers}
                emojiPreprocess={emojiPreprocess}
              />
            ))}
          </div>

          <BotRunTimeline privateChat={!publicRoom} messages={messages ?? []} />

          {publicRoom && <TypingIndicator />}
        </div>
      </ThreadPrimitive.Viewport>
      {composerFooter}
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
  cancelPending = false,
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
  const isGuest = useChatStore((state) => state.isGuest);
  // Images are uploaded through the same guarded endpoint as before, but as
  // runtime attachments: the composer shows thumbnails, and onNew decides how
  // the persisted message carries them (Markdown image refs).
  const attachmentAdapter = useMemo<AttachmentAdapter>(() => ({
    accept: "image/png,image/jpeg",
    async add({ file }) {
      const imageLimitMb = isGuest ? 2 : 4;
      if (file.size > imageLimitMb * 1024 * 1024) {
        toast.error(t("input.imageTooLarge", { limit: imageLimitMb }));
        throw new Error("image too large");
      }
      const result = await chatAPI.uploadImage(file);
      if (!result.url) {
        toast.error(t("input.imageUploadFailed"));
        throw new Error("image upload failed");
      }
      return {
        id: crypto.randomUUID(),
        type: "image",
        name: file.name,
        contentType: file.type,
        file,
        status: { type: "requires-action", reason: "composer-send" },
        content: [{ type: "image", image: result.url }],
      };
    },
    async remove() {
      // Uploaded images stay as message assets; nothing to delete remotely.
    },
    async send(attachment) {
      return { ...attachment, status: { type: "complete" }, content: attachment.content ?? [] };
    },
  }), [isGuest, t]);
  // Subscribe to one transcript only: a private stream must not re-render the
  // public room (or the reverse). Lane filtering follows the shared lane
  // convention: "" is the main chat, "lane-*" a side thread, and a thread
  // reply inherits its parent message's lane.
  const publicMessages = useChatStore((state) => state.messages);
  const privateAllLanes = useChatStore((state) => state.privateBotMessages);
  const activeLane = useChatStore((state) => state.activePrivateThread);
  const storeMessages = useMemo(
    () => {
      if (!usePrivateMessages) return publicMessages;
      const byId = new Map(privateAllLanes.map((m) => [m.id, m]));
      return privateAllLanes.filter((m) => privateLaneOf(m, byId) === activeLane);
    },
    [usePrivateMessages, privateAllLanes, publicMessages, activeLane],
  );
  const sourceMessages = messages ?? storeMessages;

  // Keep row identity stable across stream deltas. Sanitising parts here used
  // to clone every assistant message on every token, defeating MessageBubble's
  // memo and re-parsing the whole transcript for each delta.
  const visibleMessages = useMemo(
    () => sourceMessages.slice(-MAX_RENDERED_MESSAGES),
    [sourceMessages],
  );
  // assistant-ui owns the composer/runtime and viewport semantics, while the
  // transcript itself is rendered directly from Zustand below. Keep its thread
  // snapshot structural: changing streamed text must not rebuild the message
  // repository or replace a rendered row.
  const runtimeIdentityKey = JSON.stringify(visibleMessages.map((message) => [
    runtimeMessageId(message),
    message.username === username ? "user" : "assistant",
    message.timestamp,
  ]));
  const runtimeMessages = useMemo<ThreadMessageLike[]>(
    () => {
      const structure = JSON.parse(runtimeIdentityKey) as Array<
        [id: string, role: "user" | "assistant", timestamp: number]
      >;
      return structure.map(([id, role, timestamp]) => ({
        id,
        role,
        content: "",
        createdAt: new Date(timestamp),
        ...(role === "user" ? {} : { status: { type: "complete", reason: "stop" } as const }),
      }));
    },
    [runtimeIdentityKey],
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
    // Public IM stays sendable while the assistant runs; its stop button
    // invokes the same owner-scoped handler without the private runtime gate.
    isRunning: !publicRoom && isRunning,
    isDisabled: !connected,
    isSendDisabled: !connected,
    ...(onCancelRun ? { onCancel: async () => onCancelRun() } : {}),
    onNew: async (message) => {
      const text = messageText(message).trim();
      const images = (message.attachments ?? [])
        .flatMap((attachment) => attachment.content ?? [])
        .flatMap((part) => (part.type === "image" ? [part.image] : []));
      const markdown = images
        .map((url) => `![${t("input.image")}](${url})`)
        .join(" ");
      const content = [text, markdown].filter(Boolean).join(" ");
      if (content) {
        localStorage.removeItem(`tdchat-draft:${conversationKey}`);
        onSend(content);
      }
    },
    adapters: { attachments: attachmentAdapter },
    suggestions,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime} key={conversationKey}>
      <ThreadSurface
        messages={visibleMessages}
        publicRoom={publicRoom}
        conversationKey={conversationKey}
        assistantContext={assistantContext}
        connected={connected}
        maxMessageLength={maxMessageLength}
        isRunning={isRunning}
        onCancelRun={onCancelRun}
        cancelPending={cancelPending}
        onScrolledChange={onScrolledChange}
        onReply={onReply}
        onDelete={onDelete}
        onRetryMessage={onRetryMessage}
        onSend={onSend}
        onOpenThread={onOpenThread}
        highlight={highlight}
      />
    </AssistantRuntimeProvider>
  );
}
