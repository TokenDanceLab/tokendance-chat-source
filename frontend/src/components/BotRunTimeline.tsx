import { useEffect, useState } from "react";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
import { useTranslation } from "@/i18n/context";
import type { ChatMessage } from "@/lib/api";
import { tokenBot } from "@/lib/assistantRegistry";
import { cn } from "@/lib/utils";
import { isActiveRun, type BotRun } from "@/stores/botRunState";
import { useChatStore } from "@/stores/chatStore";

/** Sources and internal activity are not the first visible answer token. */
export function hasVisibleAssistantText(message: ChatMessage): boolean {
  return message.parts?.length
    ? message.parts.some((part) => part.type === "text" && Boolean(part.text?.trim()))
    : Boolean(message.content.trim());
}

function hasAnswerText(run: BotRun, messages: ChatMessage[]): boolean {
  return messages.some((message) =>
    (message.run_id === run.run_id || message.stream_id === run.stream_id ||
      message.id === run.message_id || message.message_id === run.message_id ||
      message.id === `stream-${run.run_id}`) && hasVisibleAssistantText(message),
  );
}

/** Muse's three-dot chat waiting state, using the existing motion primitives. */
export function AssistantTypingBubble() {
  const { t } = useTranslation();
  const [reduceMotion, setReduceMotion] = useState(
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  const [activeDot, setActiveDot] = useState(0);
  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduceMotion(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (reduceMotion) return;
    const interval = window.setInterval(() => setActiveDot((index) => (index + 1) % 3), 400);
    return () => window.clearInterval(interval);
  }, [reduceMotion]);
  return (
    <div role="status" aria-label={t("message.generating")} data-visual="assistant-typing-bubble"
      className={cn(
        "flex h-[44px] w-fit items-center gap-1 rounded-[22px] bg-[var(--chat-agent-bubble)] px-[15px]",
        !reduceMotion && "animate-in fade-in slide-in-from-bottom-2 duration-300 motion-reduce:animate-none",
      )}>
      {[0, 1, 2].map((index) => (
        <span key={index} aria-hidden="true" data-active={reduceMotion || activeDot === index}
          className={cn(
            "size-2 rounded-full bg-[var(--chat-typing-dot)]",
            !reduceMotion && "animate-bounce transition-opacity duration-200 motion-reduce:animate-none",
            !reduceMotion && activeDot !== index && "opacity-25",
          )}
          style={{ animationDelay: `${index * 150}ms` }} />
      ))}
    </div>
  );
}

/** A single timeline-only view of visible running and queued requests. */
export function BotRunTimeline({ privateChat, messages }: { privateChat: boolean; messages: ChatMessage[] }) {
  const { t } = useTranslation();
  const username = useChatStore((state) => state.username);
  const botRuns = useChatStore((state) => state.botRuns);
  const runs = Object.values(botRuns).filter((run) =>
    isActiveRun(run.status) && run.private === privateChat &&
    (!privateChat || run.requested_by === username) &&
    (run.status === "queued" || !hasAnswerText(run, messages)),
  );
  if (!runs.length) return null;
  return (
    <div aria-live="polite" aria-relevant="additions text" aria-label={t("botRun.label")}
      data-visual="bot-run-timeline" className="flex flex-col gap-2 py-1.5">
      {runs.map((run) => {
        const own = run.requested_by === username;
        const requestLabel = own ? t("botRun.yourReply") : t("botRun.replyTo", { name: run.requested_by });
        return (
          <div key={run.run_id} data-run-id={run.run_id} data-run-status={run.status}
            className={cn("flex items-start gap-2.5", !privateChat && run.status === "queued" && "pl-[37px]")}>
            {run.status === "running" ? (
              <>
                {!privateChat && <div className="flex size-7 shrink-0 items-start justify-center"><ParticipantAvatar username={tokenBot.name} assistant={tokenBot} size="sm" /></div>}
                <AssistantTypingBubble />
              </>
            ) : (
              <p className="py-1 text-xs text-muted-foreground" role="status">
                {t(run.cancelPending ? "botRun.stopping" : "botRun.queued")}
                {!privateChat && <> · {requestLabel}</>}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}
