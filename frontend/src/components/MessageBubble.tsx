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
import { cn, formatTime, formatFullTime } from "@/lib/utils";
import { CHAT_EVENTS } from "@/lib/events";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI } from "@/lib/api";
import { MessageContextMenu } from "@/components/MessageContextMenu";
import { useSwipeableMessage } from "@/hooks/useTouchGestures";
import { Avatar } from "@/components/Avatar";
import { AssistantIcon } from "@/components/AssistantIcon";
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
  replyCount?: number;
  onOpenThread?: (message: ChatMessage) => void;
  selectMode?: boolean;
  isSelected?: boolean;
  onToggleSelect?: (id: string) => void;
  onLongPress?: (id: string) => void;
  highlight?: string;
  isNew?: boolean;
  onlineUsers?: string[];
  emojiPreprocess?: { emojiMap: Map<string, string>; pattern: RegExp } | null;
}

type MessageGroupPosition = "single" | "first" | "middle" | "last";

const GROUP_RADIUS: Record<MessageGroupPosition, string> = {
  single: "rounded-2xl",
  first: "rounded-2xl rounded-b-lg",
  middle: "rounded-lg",
  last: "rounded-lg rounded-b-2xl",
};

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
  replyCount = 0,
  onOpenThread,
  selectMode = false,
  isSelected = false,
  onToggleSelect,
  onLongPress,
  highlight,
  isNew = false,
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

  const [showEmojiPicker, setShowEmojiPicker] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editContent, setEditContent] = useState("");
  const [contextMenu, setContextMenu] = useState({ visible: false, x: 0, y: 0 });

  const isDeleted = message.deleted === true;
  const isStreaming = message.id.startsWith("stream-");
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
  const messageAvatarUrl = userProfile?.avatar_url || null;
  const hasUrls = useMemo(() => {
    if (isDeleted) return false;
    return extractURLs(message.content).length > 0;
  }, [message.content, isDeleted]);

  const handleAddReaction = useCallback(
    (emoji: string) => {
      import("@/lib/sound").then((m) => m.playReactionSound());
      chatAPI.sendReaction(message.id, emoji);
    },
    [message.id],
  );

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
        onLongPress?.(message.id);
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
  const renderRichContent = useCallback((content: string) => {
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
      const mentionRegex = /@([\p{L}\p{N}_]+)/gu;
      const segParts: Array<
        { type: "text"; value: string } | { type: "mention"; username: string }
      > = [];
      let lastIdx = 0;
      let m: RegExpExecArray | null;
      while ((m = mentionRegex.exec(text)) !== null) {
        if (m.index > lastIdx) {
          segParts.push({ type: "text", value: text.slice(lastIdx, m.index) });
        }
        segParts.push({ type: "mention", username: m[1] });
        lastIdx = m.index + m[0].length;
      }
      if (lastIdx < text.length) {
        segParts.push({ type: "text", value: text.slice(lastIdx) });
      }

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
              remarkPlugins={[remarkGfm]}
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
                  remarkPlugins={[remarkGfm]}
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
            return renderText(part.value, j, hasMention);
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
      {onReply && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => onReply(message)}
          aria-label={t("input.replyTo")}
          title={t("input.replyTo")}
          className="size-5"
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
        className="size-5"
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
        className="size-5"
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
        className="size-5"
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
              groupPosition === "single" ? "py-1.5" : "py-0.5",
              isNew && "animate-in fade-in slide-in-from-bottom-1 duration-150",
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
                {!hideAvatar ? (
                  <MessageAvatar className="size-7 shrink-0 translate-y-0! self-start rounded-full bg-transparent text-label">
                    {isAssistant ? (
                      <AssistantIcon assistant={assistant} size="sm" />
                    ) : (
                      <Avatar
                        src={messageAvatarUrl}
                        name={messageDisplayName}
                        size="sm"
                        onClick={handleAvatarClick}
                      />
                    )}
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

                  {(message.reply_to_id || message.reply_to_content) && !selectMode && (
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
                        {(message.reply_to_content || "").slice(0, 80)}
                        {(message.reply_to_content || "").length > 80 ? "..." : ""}
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
                      variant={isAssistant ? "ghost" : "secondary"}
                      className={cn(
                        isAssistant ? "max-w-full" : "max-w-[min(80%,36rem)]",
                        isDeleted && "opacity-60",
                      )}
                    >
                      <BubbleContent
                        data-visual="message-bubble-surface"
                        className={cn(
                          "text-[15px] leading-6",
                          isAssistant
                            ? "px-0.5 py-0"
                            : cn("border-0 px-4 py-2", GROUP_RADIUS[groupPosition]),
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
                            streaming={isStreaming}
                            renderText={renderRichContent}
                          />
                        ) : (
                          <div className="markdown-body select-text">{mentionContent}</div>
                        )}

                        {!isEditing && hasUrls && (
                          <MessageLinkPreviews
                            content={message.content}
                            messageTimestamp={message.timestamp}
                          />
                        )}
                      </BubbleContent>
                      {hideUsername && !selectMode && !isDeleted && (
                        <div
                          className={cn(
                            "absolute -top-3 z-10 opacity-0 transition-opacity group-hover/message-row:opacity-100 focus-within:opacity-100",
                            isOwn ? "right-full mr-1" : "right-0",
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

                  {translatedText && (
                    <div className="mt-1 max-w-full border-l-2 border-border pl-2.5">
                      <p className="mb-0.5 text-label text-muted-foreground">
                        {t("message.translated")}
                      </p>
                      <p className="text-sm text-foreground/80">{translatedText}</p>
                    </div>
                  )}

                  {!isDeleted && (reactions || replyCount > 0) && (
                    <div
                      className={cn(
                        "mt-0.5 flex flex-wrap items-center gap-1",
                        isOwn && "self-end",
                      )}
                    >
                      {replyCount > 0 && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          data-visual="message-bubble-thread"
                          onClick={
                            selectMode
                              ? undefined
                              : () => {
                                  if (onOpenThread) onOpenThread(message);
                                  else
                                    window.dispatchEvent(
                                      new CustomEvent(CHAT_EVENTS.viewThread, {
                                        detail: { messageId: message.id },
                                      }),
                                    );
                                }
                          }
                          className="h-6 gap-1 px-1.5 text-label text-muted-foreground"
                          aria-label={`${replyCount} replies`}
                        >
                          <Reply className="size-3" />
                          {replyCount}
                        </Button>
                      )}
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
                        ) : (
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
          onDelete={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            onDelete?.(message.id);
          }}
          onSelect={() => {
            setContextMenu({ visible: false, x: 0, y: 0 });
            onLongPress?.(message.id);
          }}
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
  const [showTooltip, setShowTooltip] = useState(false);

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setShowTooltip((v) => !v);
        }}
        className="inline-flex cursor-pointer hover:text-foreground"
        aria-label={readLabel}
      >
        <Check className="size-3" />
        <Check className="-ml-2 size-3" />
      </button>
      {showTooltip && (
        <div className="absolute bottom-full right-0 z-50 mb-1.5 min-w-36 rounded-lg border bg-popover p-2 text-popover-foreground shadow-md">
          <div className="mb-1 text-label text-muted-foreground">{readByLabel}</div>
          {readers.map((r) => {
            const profile = userProfiles[r];
            const isOnline = onlineUsers.includes(r);
            return (
              <div key={r} className="flex items-center gap-2 py-0.5 text-xs">
                <span className="flex size-4 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted">
                  {profile?.avatar_url ? (
                    <img src={profile.avatar_url} alt="" className="size-full object-cover" />
                  ) : (
                    <span className="text-[8px]">
                      {(profile?.display_name || r)[0].toUpperCase()}
                    </span>
                  )}
                </span>
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
