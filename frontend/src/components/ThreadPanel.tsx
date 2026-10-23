import { useRef, useEffect, useState, useCallback, type KeyboardEvent } from "react";
import { ArrowUp, MessagesSquare, X } from "lucide-react";
import { MessageBubble } from "./MessageBubble";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import type { ChatMessage } from "@/lib/api";
import { scrollBehavior } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from "@/components/ui/input-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";

interface ThreadPanelProps {
  parentMessage: ChatMessage | null;
  threadMessages: ChatMessage[];
  onClose: () => void;
  onSendReply: (content: string) => void;
}

export function ThreadPanel({
  parentMessage,
  threadMessages,
  onClose,
  onSendReply,
}: ThreadPanelProps) {
  const { t } = useTranslation();
  const { username } = useChatStore();
  const [replyContent, setReplyContent] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: scrollBehavior() });
  }, [threadMessages]);

  const handleSend = useCallback(() => {
    const trimmed = replyContent.trim();
    if (trimmed) {
      onSendReply(trimmed);
      setReplyContent("");
    }
  }, [replyContent, onSendReply]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  if (!parentMessage) return null;

  const replyCount = threadMessages.length;
  const countText = t("thread.replyCount", { count: replyCount.toString() });

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent
        side="right"
        showCloseButton={false}
        data-visual="thread-panel"
        className="w-full max-w-md gap-0 bg-card p-0 text-card-foreground"
      >
        <div className="flex items-center justify-between border-b px-4 py-3.5">
          <div className="flex min-w-0 items-center gap-3">
            <MessagesSquare className="size-4 text-muted-foreground" />
            <div className="min-w-0">
              <SheetTitle className="truncate text-sm font-semibold">
                {t("thread.replies")}
              </SheetTitle>
              <p className="truncate text-label text-muted-foreground">{countText}</p>
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            aria-label={t("thread.close")}
            className="ml-2 size-10 text-muted-foreground"
          >
            <X className="size-4" />
          </Button>
        </div>

        <div className="border-b bg-muted/25 px-3 py-2">
          <div>
            <MessageBubble
              message={parentMessage}
              isOwn={parentMessage.username === username}
              isGrouped={false}
            />
          </div>
        </div>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-1 px-3 py-3">
            {threadMessages.length === 0 ? (
              <div className="flex flex-col items-center justify-center gap-2 py-12 text-center">
                <MessagesSquare className="size-5 text-muted-foreground/50" />
                <p className="text-xs text-muted-foreground">{t("thread.replyPlaceholder")}</p>
              </div>
            ) : (
              threadMessages.map((msg) => (
                <MessageBubble
                  key={msg.id}
                  message={msg}
                  isOwn={msg.username === username}
                  isGrouped={false}
                />
              ))
            )}
            <div ref={messagesEndRef} />
          </div>
        </ScrollArea>

        <div className="border-t bg-card p-3 pb-safe">
          <InputGroup className="rounded-lg border bg-background">
            <InputGroupTextarea
              value={replyContent}
              onChange={(e) => setReplyContent(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t("thread.replyPlaceholder")}
              aria-label={t("thread.replyPlaceholder")}
              rows={2}
              className="max-h-32 min-h-11 px-3 py-2 text-sm"
            />
            <InputGroupAddon align="block-end" className="pb-1.5">
              <span className="px-1 text-micro text-muted-foreground/50">
                Shift + Enter
              </span>
              <InputGroupButton
                type="button"
                size="icon-sm"
                variant="default"
                aria-label={t("thread.replyPlaceholder")}
                disabled={!replyContent.trim()}
                onClick={handleSend}
                className="ml-auto size-8 rounded-full"
              >
                <ArrowUp className="size-4" />
              </InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </div>
      </SheetContent>
    </Sheet>
  );
}
