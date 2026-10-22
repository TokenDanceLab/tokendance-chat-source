import { isStreamingMessage } from "@/stores/botRunState";
import { toast } from "sonner";
import {
  memo,
  useMemo,
  useCallback,
  useState,
  useRef,
  useEffect,
  lazy,
  Suspense,
} from "react";
import type { ComponentPropsWithoutRef } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  Check,
  Copy,
  MoreHorizontal,
  Reply,
  Smile,
  Trash2,
} from "lucide-react";
import { cn, formatTime, formatFullTime, plainTextPreview } from "@/lib/utils";
import { CHAT_EVENTS } from "@/lib/events";
import { messageSources } from "@/lib/messageSources";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI } from "@/lib/api";
import { MessageContextMenu } from "@/components/MessageContextMenu";
import { useSwipeableMessage } from "@/hooks/useTouchGestures";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
import type { ChatMessage } from "@/lib/api";
import { MessageLinkPreviews, extractURLs } from "@/components/LinkPreview";
import { PollMessage } from "@/components/PollMessage";
import { AssistantParts } from "@/components/assistant-ui/message-parts";
import { assistants } from "@/lib/assistantRegistry";
import { Button } from "@/components/ui/button";
import { Bubble, BubbleContent } from "@/components/ui/bubble";
import {
  Message,
  MessageAvatar,
  MessageContent,
  MessageFooter,
  MessageHeader,
} from "@/components/ui/message";
import { Spinner } from "@/components/ui/spinner";

const GFM_REMARK_PLUGINS = [remarkGfm];
const STREAMING_REMARK_PLUGINS: never[] = [];

const EmojiPicker = lazy(() =>
  import("@/components/EmojiPicker").then((m) => ({ default: m.EmojiPicker })),
);

interface MessageBubbleProps {
  message: ChatMessage;
  isOwn: boolean;
  currentUsername?: string;
  hideAvatar?: boolean;
  hideUsername?: boolean;
  forceShowTimestamp?: boolean;
  isGrouped?: boolean;
  groupPosition?: MessageGroupPosition;
  onReply?: (message: ChatMessage) => void;
  onDelete?: (messageId: string) => void;
  onRetry?: (message: ChatMessage) => void;
  /** Retry the prompt that produced a stopped partial assistant reply. */
  onRetryStopped?: (content: string) => void;
  replyCount?: number;
  onOpenThread?: (message: ChatMessage) => void;
  selectMode?: boolean;
  isSelected?: boolean;
  onToggleSelect?: (id: string) => void;
  onLongPress?: (id: string) => void;
  highlight?: string;
  isNew?: boolean;
  /** Muse-style 1:1 transcript: no identity chrome, no per-row timestamp. */
  minimal?: boolean;
  onlineUsers?: string[];
  emojiPreprocess?: { emojiMap: Map<string, string>; pattern: RegExp } | null;
}

type MessageGroupPosition = "single" | "first" | "middle" | "last";

// Muse keeps the outer capsule at 22px and only softens the touching edge
// of a grouped message to 6px.
function groupRadius(position: MessageGroupPosition, isOwn: boolean): string {
  if (position === "single") return "rounded-[22px]";
  if (position === "middle") return "rounded-[22px]";
  if (position === "first") return isOwn ? "rounded-[22px] rounded-br-md" : "rounded-[22px] rounded-bl-md";
  return isOwn ? "rounded-[22px] rounded-tr-md" : "rounded-[22px] rounded-tl-md";
}

function normalizeMentionDisplay(username: string): string {
  const lower = username.toLowerCase();
  if (["tokenbot", "webuichat", "webuibot", "webui"].includes(lower)) {
    return "TokenBot";
  }
  return username;
}

const safeMarkdownComponents = {
  a: ({ href, children, ...props }: ComponentPropsWithoutRef<"a">) => {
    if (href && /^(javascript|data|vbscript):/i.test(href)) {
      return <span>{children}</span>;
    }
    const msgMatch = href?.match(/#msg-(.+)$/);
    if (msgMatch) {
      return (
        <a
          href={href}
          onClick={(e) => {
            e.preventDefault();
            window.dispatchEvent(
              new CustomEvent(CHAT_EVENTS.scrollToMessage, { detail: { id: msgMatch[1] } }),
            );
          }}
          {...props}
        >
          {children}
        </a>
      );
    }
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
        {children}
      </a>
    );
  },
  img: ({ src, alt, ...props }: ComponentPropsWithoutRef<"img">) => (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      className="max-w-full cursor-zoom-in"
      onClick={(e) => {
        e.stopPropagation();
        if (src) useChatStore.getState().setLightboxImage(src);
      }}
      {...props}
    />
  ),
};

const CodeBlock = memo(function CodeBlock({
  language,
  code,
}: {
  language: string;
  code: string;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable */
    }
  }, [code]);

  return (
    <div className="td-code-block my-2 overflow-hidden rounded-lg border bg-muted/40">
      <div className="flex items-center justify-between border-b px-3 py-1.5">
        <span className="font-mono text-label text-muted-foreground">
          {language || "code"}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          onClick={handleCopy}
          aria-label={t("a11y.copyCode")}
          className="h-6 gap-1 text-label text-muted-foreground"
        >
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
          {copied ? t("message.copied") : t("message.copy")}
        </Button>
      </div>
      <pre className="overflow-x-auto p-3 text-[13px] leading-5">
        <code className={`language-${language || ""}`}>{code}</code>
      </pre>
    </div>
  );
});

function parseContentForCodeBlocks(
  content: string,
): Array<{ type: "text"; value: string } | { type: "code"; language: string; code: string }> {
  const parts: Array<
    { type: "text"; value: string } | { type: "code"; language: string; code: string }
  > = [];
  const codeBlockRegex = /```(\w*)\n([\s\S]*?)```/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = codeBlockRegex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ type: "text", value: content.slice(lastIndex, match.index) });
    }
    parts.push({ type: "code", language: match[1] || "", code: match[2].trimEnd() });
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < content.length) {
    parts.push({ type: "text", value: content.slice(lastIndex) });
  }
  return parts;
}

export const MessageBubble = memo(function MessageBubble({
  message,
  isOwn,
  hideAvatar = false,
  hideUsername = false,
  forceShowTimestamp = false,
  isGrouped = false,
  groupPosition = "single",
  onReply,
  onDelete,
  onRetry,
  onRetryStopped,
  replyCount = 0,
  onOpenThread,
  selectMode = false,
  isSelected = false,
  onToggleSelect,
  onLongPress,
  highlight,
  isNew = false,
  minimal = false,
  onlineUsers = [],
  emojiPreprocess,
}: MessageBubbleProps) {
  const { t } = useTranslation();
  const setSelectedProfileUser = useChatStore((s) => s.setSelectedProfileUser);
  const translations = useChatStore((s) => s.translations);
  const polls = useChatStore((s) => s.polls);
  const translatedText = translations[message.id];
  const reactionsByMessageId = useChatStore((s) => s.reactionsByMessageId);
  const readByMessageId = useChatStore((s) => s.readByMessageId);
  const reactions = reactionsByMessageId[message.id] || message.reactions;
  const readBy = readByMessageId[message.id] || message.read_by;
  const userProfiles = useChatStore((s) => s.userProfiles);
  const addBlockedUser = useChatStore((s) => s.addBlockedUser);

  const [showEmojiPicker, setShowEmojiPicker] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editContent, setEditContent] = useState("");
  const [contextMenu, setContextMenu] = useState({ visible: false, x: 0, y: 0 });
  const [isPendingDimmed, setIsPendingDimmed] = useState(false);

  const isDeleted = message.deleted === true;
  const isStreaming = isStreamingMessage(message);
  // Until the server echo replaces the optimistic row, show it as pending.
  const isPending = !message.failed && message.id.startsWith("optimistic_");
  const assistant = assistants.find(
    (a) => a.name.toLowerCase() === message.username.toLowerCase(),
  );
  const isAssistant =
    !!assistant ||
    ["tokenbot", "webuichat", "webuibot", "webui"].includes(message.username.toLowerCase());
  const userProfile = userProfiles[message.username];
  const messageDisplayName = isAssistant
    ? assistant?.name || "TokenBot"
    : userProfile?.display_name || message.username;
  const hasUrls = useMemo(() => {
    if (isDeleted) return false;
    return extractURLs(message.content).length > 0;
  }, [message.content, isDeleted]);

  // Muse keeps an optimistic message at full contrast until an acknowledgement
  // is genuinely delayed. Dimming immediately and undimming on a fast echo
  // reads as two unrelated flashes around the same send.
  useEffect(() => {
    if (!isPending) {
      setIsPendingDimmed(false);
      return;
    }
    const timeout = window.setTimeout(() => setIsPendingDimmed(true), 250);
    return () => window.clearTimeout(timeout);
  }, [isPending, message.id]);

  const handleAddReaction = useCallback(
    (emoji: string) => {
      import("@/lib/sound").then((m) => m.playReactionSound());
      chatAPI.sendReaction(message.id, emoji);
    },
    [message.id],
  );

  const handleBlock = useCallback(() => {
    chatAPI.sendBlock(message.username);
    addBlockedUser(message.username);
    setContextMenu({ visible: false, x: 0, y: 0 });
    toast.success(t("message.blocked", { name: message.username }));
  }, [addBlockedUser, message.username, t]);

  const swipe = useSwipeableMessage({
    onReply: () => onReply?.(message),
    onCopy: async () => {
      await navigator.clipboard.writeText(message.content);
    },
    onDelete: () => onDelete?.(message.id),
    isOwn,
    disabled: selectMode || isDeleted,
  });

  const contextMenuPosRef = useRef({ x: 0, y: 0 });
  const longPressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handlePointerMove = useCallback((e: React.PointerEvent) => {
    contextMenuPosRef.current = { x: e.clientX, y: e.clientY };
  }, []);

  const handlePointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (selectMode || isDeleted) return;
      contextMenuPosRef.current = { x: e.clientX, y: e.clientY };
      longPressTimerRef.current = setTimeout(() => {
        if (onLongPress) {
          onLongPress(message.id);
        } else {
          // No select mode is wired today; long-press is the mobile entry to
          // the full context menu instead of a dead gesture.
          setContextMenu({
            visible: true,
            x: contextMenuPosRef.current.x,
            y: contextMenuPosRef.current.y,
          });
        }
      }, 500);
    },
    [selectMode, isDeleted, onLongPress, message.id],
  );

  const clearLongPress = useCallback(() => {
    if (longPressTimerRef.current) {
      clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
  }, []);

  const handleContextMenu = useCallback(
    (e: React.MouseEvent) => {
      if (selectMode || isDeleted) return;
      e.preventDefault();
      setContextMenu({ visible: true, x: e.clientX, y: e.clientY });
    },
    [selectMode, isDeleted],
  );

  const openContextMenuFromButton = useCallback(
    (e: React.MouseEvent<HTMLButtonElement>) => {
      if (selectMode || isDeleted) return;
      e.preventDefault();
      e.stopPropagation();
      const rect = e.currentTarget.getBoundingClientRect();
      setContextMenu({ visible: true, x: rect.right - 228, y: rect.bottom + 6 });
    },
    [selectMode, isDeleted],
  );

  const handleBubbleClick = useCallback(
    (e: React.MouseEvent) => {
      if (swipe.showActions) {
        swipe.closeActions();
        return;
      }
      if (selectMode && onToggleSelect) {
        e.preventDefault();
        e.stopPropagation();
        onToggleSelect(message.id);
      }
    },
    [message.id, selectMode, onToggleSelect, swipe],
  );

  useEffect(() => () => clearLongPress(), [clearLongPress]);

  useEffect(() => {
    const handler = () => setShowEmojiPicker(false);
    window.addEventListener(CHAT_EVENTS.closeEmojiPicker, handler);
    return () => window.removeEventListener(CHAT_EVENTS.closeEmojiPicker, handler);
  }, []);

  // Renders Markdown with custom emoji, @mentions, and search highlighting.
  // Assistant replies call it once per text part.
  const renderRichContent = useCallback((content: string, streaming = false) => {
    let processedContent = content;
    if (emojiPreprocess) {
      processedContent = processedContent.replace(
        emojiPreprocess.pattern,
        (_match: string, name: string) => {
          const url = emojiPreprocess.emojiMap.get(name);
          if (!url) return _match;
          return `<img src="${url}" alt=":${name}:" class="inline-custom-emoji" />`;
        },
      );
    }

    const renderSegment = (text: string, key: number): React.ReactNode => {
      type Segment =
        | { type: "text"; value: string }
        | { type: "mention"; username: string }
        | { type: "code"; value: string };
      const segParts: Segment[] = [];

      // Code spans and fences are opaque: a backticked `@name` must render as
      // code, not as a mention button with unbalanced backticks around it.
      const pushText = (value: string) => {
        const mentionRegex = /@([\p{L}\p{N}_]+)/gu;
        let lastIdx = 0;
        let m: RegExpExecArray | null;
        while ((m = mentionRegex.exec(value)) !== null) {
          if (m.index > lastIdx) {
            segParts.push({ type: "text", value: value.slice(lastIdx, m.index) });
          }
          segParts.push({ type: "mention", username: m[1] });
          lastIdx = m.index + m[0].length;
        }
        if (lastIdx < value.length) {
          segParts.push({ type: "text", value: value.slice(lastIdx) });
        }
      };

      let textStart = 0;
      for (let i = 0; i < text.length; ) {
        if (text.startsWith("```", i)) {
          // Fenced blocks stay in the text stream: parseContentForCodeBlocks
          // turns them into CodeBlock and they must keep block layout.
          const close = text.indexOf("```", i + 3);
          i = close < 0 ? text.length : close + 3;
          continue;
        }
        if (text[i] === "`") {
          let run = 1;
          while (text[i + run] === "`") run++;
          const close = text.indexOf("`".repeat(run), i + run);
          if (close >= 0) {
            pushText(text.slice(textStart, i));
            segParts.push({ type: "code", value: text.slice(i, close + run) });
            i = close + run;
            textStart = i;
            continue;
          }
          i += run;
          continue;
        }
        i++;
      }
      pushText(text.slice(textStart));

      const renderText = (value: string, subKey: number, inline = false) => {
        // Markdown trims leading whitespace at a paragraph boundary. Mentions
        // are rendered inline, so keep the separating space after @name.
        const renderedValue =
          inline && value.startsWith(" ") ? `\u00a0${value.slice(1)}` : value;
        const codeParts = parseContentForCodeBlocks(value);
        const components = inline
          ? {
              ...safeMarkdownComponents,
              p: ({ children }: ComponentPropsWithoutRef<"p">) => <>{children}</>,
            }
          : safeMarkdownComponents;
        if (codeParts.length === 1 && codeParts[0].type === "text") {
          return (
            <ReactMarkdown
              key={subKey}
              remarkPlugins={streaming ? STREAMING_REMARK_PLUGINS : GFM_REMARK_PLUGINS}
              components={components}
            >
              {renderedValue}
            </ReactMarkdown>
          );
        }
        return (
          <span key={subKey}>
            {codeParts.map((part, j) =>
              part.type === "code" ? (
                <CodeBlock key={j} language={part.language} code={part.code} />
              ) : (
                <ReactMarkdown
                  key={j}
                  remarkPlugins={streaming ? STREAMING_REMARK_PLUGINS : GFM_REMARK_PLUGINS}
                  components={components}
                >
                  {inline && part.value.startsWith(" ")
                    ? `\u00a0${part.value.slice(1)}`
                    : part.value}
                </ReactMarkdown>
              ),
            )}
          </span>
        );
      };

      if (segParts.length === 0) return renderText(text, key);

      const hasMention = segParts.some((part) => part.type === "mention");
      // Any split means the original text was cut mid-paragraph. Render each
      // fragment inline so code spans do not turn into stacked <p> blocks.
      const inlineFragments = hasMention || segParts.length > 1;
      return (
        <span key={key}>
          {segParts.map((part, j) => {
            if (part.type === "mention") {
              const mentionDisplay = normalizeMentionDisplay(part.username);
              return (
                <button
                  key={j}
                  type="button"
                  onClick={() => setSelectedProfileUser(mentionDisplay)}
                  className="font-medium underline decoration-border underline-offset-2 hover:decoration-foreground"
                >
                  @{mentionDisplay}
                </button>
              );
            }
            if (part.type === "code") return renderText(part.value, j, true);
            return renderText(part.value, j, inlineFragments);
          })}
        </span>
      );
    };

    if (highlight) {
      const escaped = highlight.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      const hlRegex = new RegExp(`(${escaped})`, "gi");
      const segments = processedContent.split(hlRegex);
      const hlLower = highlight.toLowerCase();
      return (
        <>
          {segments.map((seg, i) =>
            seg.toLowerCase() === hlLower && seg.length > 0 ? (
              <mark key={i} className="rounded-sm bg-warning/35 px-0.5 text-foreground">
                {seg}
              </mark>
            ) : (
              renderSegment(seg, i)
            ),
          )}
        </>
      );
    }

    return renderSegment(processedContent, 0);
  }, [emojiPreprocess, highlight, setSelectedProfileUser]);

  const mentionContent = useMemo(
    () =>
      isDeleted ? (
        <span className="text-muted-foreground italic line-through">
          {t("chat.deletedMessage")}
        </span>
      ) : (
        renderRichContent(message.content)
      ),
    [isDeleted, message.content, renderRichContent, t],
  );

  const handleAvatarClick = useCallback(() => {
    if (selectMode) return;
    setSelectedProfileUser(message.username);
  }, [message.username, setSelectedProfileUser, selectMode]);

  const copyMessage = useCallback(async () => {
    await navigator.clipboard.writeText(message.content);
  }, [message.content]);

  const renderActions = (className?: string) => (
    <div className={cn("hidden shrink-0 items-center sm:flex", className)}>
      {replyCount > 0 && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          data-visual="message-bubble-thread"
          onClick={() => {
            if (onOpenThread) onOpenThread(message);
            else
              window.dispatchEvent(
                new CustomEvent(CHAT_EVENTS.viewThread, { detail: { messageId: message.id } }),
              );
          }}
          aria-label={`${replyCount} replies`}
          title={`${replyCount} replies`}
          className="h-5 min-w-5 gap-0.5 px-1"
        >
          <Reply className="size-3.5" />
          <span className="text-label">{replyCount}</span>
        </Button>
      )}
      {onReply && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => onReply(message)}
          aria-label={t("input.replyTo")}
          title={t("input.replyTo")}
          className="size-6"
        >
          <Reply className="size-3.5" />
        </Button>
      )}
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={() => setShowEmojiPicker(true)}
        aria-label={t("message.addReaction")}
        title={t("message.addReaction")}
        className="size-6"
      >
        <Smile className="size-3.5" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={copyMessage}
        aria-label={t("message.copy")}
        title={t("message.copy")}
        className="size-6"
      >
        <Copy className="size-3.5" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={openContextMenuFromButton}
        data-visual="message-bubble-menu"
        aria-label={t("message.contextMenu")}
        title={t("message.contextMenu")}
        className="size-6"
      >
        <MoreHorizontal className="size-3.5" />
      </Button>
    </div>
  );

  return (
    <>
      <div className="relative overflow-hidden">
        <div
          className={cn(
            "absolute inset-y-0 right-0 flex items-center gap-1 px-3 transition-opacity",
            swipe.showActions ? "opacity-100" : "pointer-events-none opacity-0",
          )}
          aria-hidden={!swipe.showActions}
        >
          {onReply && (
            <Button
              type="button"
              variant="secondary"
              size="icon-sm"
              className="size-11 sm:size-9"
              onClick={() => {
                onReply(message);
                swipe.closeActions();
              }}
              aria-label={t("input.replyTo")}
            >
              <Reply className="size-4" />
            </Button>
          )}
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            className="size-11 sm:size-9"
            onClick={async () => {
              await copyMessage();
              swipe.closeActions();
            }}
            aria-label={t("message.copy")}
          >
            <Copy className="size-4" />
          </Button>
          {isOwn && onDelete && (
            <Button
              type="button"
              variant="secondary"
              size="icon-sm"
              className="size-11 sm:size-9"
              onClick={() => {
                onDelete(message.id);
                swipe.closeActions();
              }}
              aria-label={t("message.delete")}
            >
              <Trash2 className="size-4" />
            </Button>
          )}
        </div>

        <div
          className="touch-pan-y transition-transform duration-200 ease-out"
          style={{ transform: `translateX(${swipe.translateX}px)` }}
          onTouchStart={swipe.onTouchStart}
          onTouchMove={swipe.onTouchMove}
          onTouchEnd={swipe.onTouchEnd}
        >
          <div
            id={`msg-${message.id}`}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={clearLongPress}
            onPointerCancel={clearLongPress}
            onClick={handleBubbleClick}
            onContextMenu={handleContextMenu}
            onDoubleClick={() => {
              if (!selectMode && !isDeleted && onReply) onReply(message);
            }}
            data-visual="message-bubble"
            data-message-own={isOwn ? "true" : "false"}
            className={cn(
              "group/message-row relative scroll-mt-24",
              minimal
                ? (groupPosition === "single" || groupPosition === "last"
                    ? "py-3"
                    : isAssistant ? "py-3" : "py-0.5")
                : (groupPosition === "single"
                    ? "py-1.5"
                    // Distinct assistant turns need visible air even when the
                    // header is shared; rapid human continuations stay tight.
                    : isAssistant ? "py-2.5" : "py-0.5"),
              isNew && !isStreaming &&
                "animate-in fade-in slide-in-from-bottom-1 duration-[180ms] ease-[cubic-bezier(0,0,0.58,1)] motion-reduce:animate-none",
              selectMode && "cursor-pointer",
              isSelected && "bg-accent",
            )}
          >
            {selectMode && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleSelect?.(message.id);
                }}
                className="absolute left-0 top-1/2 z-10 flex size-9 -translate-y-1/2 items-center justify-center"
                aria-label={isSelected ? "Deselect message" : "Select message"}
              >
                <span
                  className={cn(
                    "flex size-4 items-center justify-center rounded-[4px] border",
                    isSelected
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-muted-foreground/50",
                  )}
                >
                  {isSelected && <Check className="size-3" />}
                </span>
              </button>
            )}

            <div className="flex w-full min-w-0">
              <Message
                align={isOwn ? "end" : "start"}
                className={cn("gap-2.5", selectMode && !isOwn && "pl-5")}
              >
                {minimal ? null : !hideAvatar ? (
                  <MessageAvatar className="size-7 shrink-0 translate-y-0! self-start rounded-full bg-transparent text-label">
                    <ParticipantAvatar username={message.username} name={messageDisplayName} assistant={assistant}
                      size="sm" onClick={isAssistant ? undefined : handleAvatarClick} />
                  </MessageAvatar>
                ) : (
                  <div className="w-8 shrink-0" aria-hidden="true" />
                )}

                <MessageContent className="min-w-0 items-start gap-1">
                  {!isOwn && !hideUsername && (
                    <MessageHeader
                      className="w-full gap-2 px-0.5 text-label font-normal"
                    >
                      <button
                        type="button"
                        className="min-w-0 cursor-pointer truncate text-xs font-medium text-foreground hover:underline"
                        onClick={handleAvatarClick}
                      >
                        {messageDisplayName}
                      </button>
                      {isAssistant && (
                        <span className="text-label text-muted-foreground">AI</span>
                      )}
                      {(!isGrouped || forceShowTimestamp) && (
                        <span
                          data-visual="message-bubble-meta"
                          className="text-label text-muted-foreground/70"
                          title={formatFullTime(message.timestamp)}
                        >
                          {formatTime(message.timestamp, t)}
                          {(message as ChatMessage & { mention_all?: boolean }).mention_all && (
                            <span className="ml-1">@all</span>
                          )}
                        </span>
                      )}
                      {!selectMode && !isDeleted && (
                        <div className="ml-1 flex shrink-0 items-center">
                          {renderActions(
                            "opacity-0 transition-opacity group-hover/message-row:opacity-100 focus-within:opacity-100",
                          )}
                        </div>
                      )}
                    </MessageHeader>
                  )}

                  {(message.reply_to_id || message.reply_to_content) && !selectMode && !minimal && (
                    <button
                      type="button"
                      data-visual="message-bubble-reply-preview"
                      className={cn(
                        "mb-0.5 max-w-full cursor-pointer border-l-2 border-border pl-2.5 text-left text-xs text-muted-foreground hover:text-foreground",
                        isOwn && "self-end border-l-0 border-r-2 pr-2.5",
                      )}
                      onClick={() => {
                        if (!message.reply_to_id) return;
                        window.dispatchEvent(
                          new CustomEvent(CHAT_EVENTS.scrollToMessage, {
                            detail: { id: message.reply_to_id },
                          }),
                        );
                      }}
                    >
                      <span className="block font-medium">
                        {message.reply_to_user || "..."}
                      </span>
                      <span className="block max-w-[260px] truncate">
                        {plainTextPreview(message.reply_to_content || "", 80)}
                      </span>
                    </button>
                  )}

                  {isAssistant && isStreaming && !message.parts?.length && (
                    <div className="mb-1 flex items-center gap-1.5 px-0.5 text-xs text-muted-foreground">
                      <Spinner className="size-3" />
                      {t("message.generating")}
                    </div>
                  )}

                  {isEditing ? (
                    <div
                      className={cn(
                        "flex w-full max-w-[36rem] min-w-[260px] flex-col gap-2",
                        isOwn && "self-end",
                      )}
                    >
                      <textarea
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" && !e.shiftKey) {
                            e.preventDefault();
                            if (editContent.trim()) {
                              chatAPI.sendMessageEdit(message.id, editContent);
                              setIsEditing(false);
                            }
                          } else if (e.key === "Escape") {
                            setIsEditing(false);
                          }
                        }}
                        className="min-h-16 w-full resize-none rounded-lg border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                        autoFocus
                      />
                      <div className="flex items-center justify-end gap-1.5">
                        <span className="flex-1 text-label text-muted-foreground">
                          {t("input.escapeToCancel")}
                        </span>
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          onClick={() => setIsEditing(false)}
                        >
                          {t("input.cancel")}
                        </Button>
                        <Button
                          type="button"
                          size="xs"
                          onClick={() => {
                            if (editContent.trim()) {
                              chatAPI.sendMessageEdit(message.id, editContent);
                              setIsEditing(false);
                            }
                          }}
                        >
                          {t("input.save")}
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <Bubble
                      align={isOwn ? "end" : "start"}
                      variant={isAssistant ? "agent" : isOwn ? "brand" : "secondary"}
                      className={cn(
                        isAssistant
                          ? "max-w-[85%] sm:max-w-[min(85%,calc(100%-148px))]"
                          : "max-w-[min(80%,32.5rem)]",
                        isDeleted && "opacity-60",
                        isPendingDimmed &&
                          "opacity-70 transition-opacity duration-200 ease-[cubic-bezier(0,0,0.58,1)]",
                      )}
                      aria-busy={isPending || undefined}
                    >
                      <BubbleContent
                        data-visual="message-bubble-surface"
                        className={cn(
                          isAssistant
                            ? "min-h-[44px] px-[15px] py-[11px] text-[16px] leading-[24px]"
                            : "min-h-[44px] px-[15px] py-[11px] text-[16px] leading-[22px]",
                          groupRadius(groupPosition, isOwn),
                        )}
                      >
                        {polls[message.id] || message.poll ? (
                          <PollMessage
                            poll={(polls[message.id] || message.poll)!}
                            messageId={message.id}
                          />
                        ) : isAssistant && message.parts?.length ? (
                          <AssistantParts
                            parts={message.parts}
                            publicOnly={!message.private}
                            streaming={isStreaming}
                            renderText={renderRichContent}
                          />
                        ) : (
                          <div className="markdown-body select-text">{mentionContent}</div>
                        )}

                        {!isEditing && hasUrls && !(isAssistant && messageSources(message.parts ?? []).length > 0) && (
                          <MessageLinkPreviews
                            content={message.content}
                            messageTimestamp={message.timestamp}
                          />
                        )}
                      </BubbleContent>
                      {minimal && !isDeleted && !selectMode && !isStreaming && (
                        // Muse reveals a compact action row at the bubble's
                        // bottom edge; absolute placement keeps bubble height.
                        <div
                          className={cn(
                            "pointer-events-none absolute top-1/2 hidden -translate-y-1/2 scale-95 items-center gap-0.5 rounded-full text-muted-foreground opacity-0 transition-[opacity,transform] duration-150 group-hover/message-row:pointer-events-auto group-hover/message-row:scale-100 group-hover/message-row:opacity-100 focus-within:pointer-events-auto focus-within:scale-100 focus-within:opacity-100 sm:flex",
                            isOwn ? "right-full mr-2 origin-right" : "left-full ml-2 origin-left",
                          )}
                        >
                          {replyCount > 0 && onOpenThread && (
                            <button
                              type="button"
                              data-visual="message-bubble-thread"
                              aria-label={`${replyCount} replies`}
                              title={`${replyCount} replies`}
                              onClick={() => onOpenThread(message)}
                              className="flex h-[26px] min-w-[26px] items-center justify-center gap-0.5 rounded-full px-1 transition-colors hover:bg-muted hover:text-foreground"
                            >
                              <Reply className="size-3.5" />
                              <span className="text-label">{replyCount}</span>
                            </button>
                          )}
                          {onReply && (
                            <button
                              type="button"
                              aria-label={t("input.replyTo")}
                              title={t("input.replyTo")}
                              onClick={() => onReply(message)}
                              className="flex size-[26px] items-center justify-center rounded-full transition-colors hover:bg-muted hover:text-foreground"
                            >
                              <Reply className="size-3.5" />
                            </button>
                          )}
                          <button
                            type="button"
                            aria-label={t("message.addReaction")}
                            title={t("message.addReaction")}
                            onClick={() => setShowEmojiPicker(true)}
                            className="flex size-[26px] items-center justify-center rounded-full transition-colors hover:bg-muted hover:text-foreground"
                          >
                            <Smile className="size-3.5" />
                          </button>
                          <button
                            type="button"
                            aria-label={t("message.copy")}
                            title={t("message.copy")}
                            data-visual="message-copy"
                            onClick={copyMessage}
                            className="flex size-[26px] items-center justify-center rounded-full transition-colors hover:bg-muted hover:text-foreground"
                          >
                            <Copy className="size-3.5" />
                          </button>
                          <button
                            type="button"
                            aria-label={t("message.contextMenu")}
                            title={t("message.contextMenu")}
                            data-visual="message-bubble-menu"
                            onClick={openContextMenuFromButton}
                            className="flex size-[26px] items-center justify-center rounded-full transition-colors hover:bg-muted hover:text-foreground"
                          >
                            <MoreHorizontal className="size-3.5" />
                          </button>
                        </div>
                      )}
                      {hideUsername && !minimal && !selectMode && !isDeleted && (
                        <div
                          className={cn(
                            "absolute top-1/2 z-10 -translate-y-1/2 opacity-0 transition-opacity group-hover/message-row:opacity-100 focus-within:opacity-100",
                            isOwn ? "right-full mr-1" : "left-full ml-1",
                          )}
                        >
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-xs"
                            onClick={openContextMenuFromButton}
                            data-visual="message-bubble-menu"
                            aria-label={t("message.contextMenu")}
                            title={t("message.contextMenu")}
                            className="size-6 rounded-md bg-card"
                          >
                            <MoreHorizontal className="size-3.5" />
                          </Button>
                        </div>
                      )}
                    </Bubble>
                  )}

                  {message.failed && !isDeleted && (
                    <div
                      className={cn(
                        "mt-1 flex flex-wrap items-center gap-2 text-label text-destructive/80",
                        isOwn && "self-end justify-end",
                      )}
                      role="status"
                    >
                      <span>{message.failureReason || t("message.sendFailed")}</span>
                      {isOwn && onRetry && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          data-visual="message-retry"
                          className="h-6 px-1.5 text-label"
                          onClick={() => onRetry(message)}
                        >
                          {t("message.retry")}
                        </Button>
                      )}
                    </div>
                  )}

                  {message.stopped && !isDeleted && (
                    <div
                      className="mt-1 flex flex-wrap items-center gap-2 self-start text-label text-muted-foreground"
                      role="status"
                      data-visual="message-stopped"
                    >
                      <span>{t("message.stopped")}</span>
                      {message.content && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          className="h-6 px-1.5 text-label"
                          onClick={copyMessage}
                        >
                          {t("message.copy")}
                        </Button>
                      )}
                      {onRetryStopped && message.reply_to_content && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          data-visual="message-stopped-retry"
                          className="h-6 px-1.5 text-label"
                          onClick={() => onRetryStopped(message.reply_to_content!)}
                        >
                          {t("message.retry")}
                        </Button>
                      )}
                    </div>
                  )}

                  {translatedText && (
                    <div className="mt-1 max-w-full border-l-2 border-border pl-2.5">
                      <p className="mb-0.5 text-label text-muted-foreground">
                        {t("message.translated")}
                      </p>
                      <p className="text-sm text-foreground/80">{translatedText}</p>
                    </div>
                  )}

                  {!isDeleted && reactions && (
                    <div
                      className={cn(
                        "mt-0.5 flex flex-wrap items-center gap-1",
                        isOwn && "self-end",
                      )}
                    >
                      {reactions &&
                        Object.entries(reactions).map(
                          ([emoji, users]) =>
                            users.length > 0 && (
                              <Button
                                key={emoji}
                                type="button"
                                variant="ghost"
                                size="xs"
                                data-visual="message-bubble-reaction"
                                onClick={selectMode ? undefined : () => handleAddReaction(emoji)}
                                className={cn(
                                  "h-6 gap-1 border border-border px-1.5 text-label",
                                  currentUsernameIn(users, useChatStore.getState().username) &&
                                    "bg-accent",
                                )}
                                aria-label={`${emoji} ${users.length} reactions`}
                                title={users.join(", ")}
                              >
                                <span className="text-name leading-none">{emoji}</span>
                                <span className="tabular-nums text-muted-foreground">
                                  {users.length}
                                </span>
                              </Button>
                            ),
                        )}
                    </div>
                  )}

                  {isOwn &&
                    !minimal &&
                    (groupPosition === "single" ||
                      groupPosition === "last" ||
                      message.edited) && (
                    <MessageFooter
                      data-visual="message-bubble-meta"
                      className="mt-1 justify-end gap-2 px-1 text-label text-muted-foreground/70"
                    >
                      {!selectMode && !isDeleted && (
                        <div className="opacity-0 transition-opacity group-hover/message-row:opacity-100 focus-within:opacity-100">
                          {renderActions()}
                        </div>
                      )}
                      <span
                        className="inline-flex items-center gap-1"
                        title={formatFullTime(message.timestamp)}
                      >
                        {readBy && readBy.length > 0 ? (
                          <ReadReceipt
                            readers={readBy}
                            readByLabel={t("message.readBy")}
                            readLabel={t("message.read")}
                            onlineLabel={t("a11y.online")}
                            userProfiles={userProfiles}
                            onlineUsers={onlineUsers}
                          />
                        ) : message.failed ? null : (
                          <span aria-label={t("message.sent")}>
                            <Check className="size-3" />
                          </span>
                        )}
                        {formatTime(message.timestamp, t)}
                        {message.edited && (
                          <span className="text-muted-foreground/60">
                            {t("message.edited")}
                          </span>
                        )}
                      </span>
                    </MessageFooter>
                  )}
                </MessageContent>
              </Message>
            </div>
          </div>
        </div>
      </div>

      {showEmojiPicker && (
        <Suspense fallback={null}>
          <EmojiPicker
            onSelect={(emoji: string) => handleAddReaction(emoji)}
            onClose={() => setShowEmojiPicker(false)}
          />
        </Suspense>
      )}

      {contextMenu.visible && (
        <MessageContextMenu
          message={message}
          isOwn={isOwn}
          position={{ x: contextMenu.x, y: contextMenu.y }}
          onClose={() => setContextMenu({ visible: false, x: 0, y: 0 })}
          onReply={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            onReply?.(message);
          }}
          onCopy={async () => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            await copyMessage();
          }}
          onBlock={
            !isOwn && !isAssistant && message.username !== "system" ? handleBlock : undefined
          }
          onDelete={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            onDelete?.(message.id);
          }}
          onSelect={
            onLongPress
              ? () => {
                  setContextMenu({ visible: false, x: 0, y: 0 });
                  onLongPress(message.id);
                }
              : undefined
          }
          onEdit={
            isOwn
              ? () => {
                  setContextMenu({ visible: false, x: 0, y: 0 });
                  setIsEditing(true);
                  setEditContent(message.content);
                }
              : undefined
          }
          onPin={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            chatAPI.sendPinMessage(message.id);
          }}
          onReact={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            setShowEmojiPicker(true);
          }}
          onTranslate={
            !isOwn && message.content && message.content.length < 500
              ? () => {
                  setContextMenu({ visible: false, x: 0, y: 0 });
                  chatAPI.sendTranslateMessage(message.id, message.content, "");
                }
              : undefined
          }
        />
      )}
    </>
  );
});

function currentUsernameIn(users: string[], username: string): boolean {
  return !!username && users.includes(username);
}

function ReadReceipt({
  readers,
  readByLabel,
  readLabel,
  onlineLabel,
  userProfiles,
  onlineUsers,
}: {
  readers: string[];
  readByLabel: string;
  readLabel: string;
  onlineLabel: string;
  userProfiles: Record<string, { display_name?: string; avatar_url?: string }>;
  onlineUsers: string[];
}) {
  const [tooltip, setTooltip] = useState<{ left: number; top: number; up: boolean } | null>(
    null,
  );

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          if (tooltip) {
            setTooltip(null);
            return;
          }
          // Fixed positioning escapes the message row's overflow clip and the
          // scroll viewport, so the reader list is never cut in half.
          const rect = e.currentTarget.getBoundingClientRect();
          const up = rect.top > 180;
          setTooltip({
            left: Math.max(8, Math.min(rect.right - 208, window.innerWidth - 216)),
            top: up ? rect.top - 6 : rect.bottom + 6,
            up,
          });
        }}
        className="inline-flex cursor-pointer p-2 -m-2 hover:text-foreground"
        aria-label={readLabel}
      >
        <Check className="size-3" />
        <Check className="-ml-2 size-3" />
      </button>
      {tooltip && (
        <div
          className="fixed z-50 w-52 rounded-lg border bg-popover p-2 text-popover-foreground shadow-md"
          style={{
            left: tooltip.left,
            top: tooltip.top,
            transform: tooltip.up ? "translateY(-100%)" : undefined,
          }}
        >
          <div className="mb-1 text-label text-muted-foreground">{readByLabel}</div>
          {readers.map((r) => {
            const profile = userProfiles[r];
            const isOnline = onlineUsers.includes(r);
            return (
              <div key={r} className="flex items-center gap-2 py-0.5 text-xs">
                <ParticipantAvatar username={r} size="sm" className="[&_[data-slot=avatar]]:size-4 [&_[data-slot=avatar-fallback]]:text-[8px]" />
                <span className="truncate">{profile?.display_name || r}</span>
                {isOnline && (
                  <span
                    className="ml-auto size-1.5 shrink-0 rounded-full bg-success"
                    title={onlineLabel}
                  />
                )}
              </div>
            );
          })}
        </div>
      )}
    </span>
  );
}
