import { Square } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import { chatAPI } from "@/lib/api";
import { useChatStore } from "@/stores/chatStore";
import { isActiveRun } from "@/stores/botRunState";

/** Run controls are a view of the same store as the assistant-ui transcript. */
export function BotRunStrip({ privateChat, connected, showStop = true }: { privateChat: boolean; connected: boolean; showStop?: boolean }) {
  const { t } = useTranslation();
  const config = useServerConfig();
  const username = useChatStore((state) => state.username);
  const botRuns = useChatStore((state) => state.botRuns);
  const messages = useChatStore((state) => privateChat ? state.privateBotMessages : state.messages);
  const runs = Object.values(botRuns).filter((run) =>
    isActiveRun(run.status) && run.private === privateChat && (!privateChat || run.requested_by === username),
  );
  if (!runs.length) return null;

  return (
    <div aria-live="polite" aria-relevant="additions text" aria-label={t("botRun.label")}
      className="mb-2 max-h-36 overflow-y-auto" data-visual="bot-run-strip">
      {runs.map((run) => {
        const own = run.requested_by === username;
        const trigger = messages.find((message) => message.id === run.trigger_message_id);
        const preview = trigger?.content.replace(/\s+/g, " ").slice(0, 80);
        const requestLabel = own ? t("botRun.yourReply") : t("botRun.replyTo", { name: run.requested_by });
        return (
          <div key={run.run_id} className="flex min-h-11 items-center gap-3 px-1 sm:min-h-8" data-run-id={run.run_id}>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              <p className="truncate"><span className="font-medium text-foreground">{config?.bot_name || "TokenBot"}</span>
                {" · "}{run.cancelPending ? t("botRun.stopping") : t(`botRun.${run.status}`)}{" · "}{requestLabel}</p>
              {preview && !privateChat && <p className="truncate text-label">{preview}</p>}
            </div>
            {own && showStop && config?.capabilities?.run_control === true && (
              <Button type="button" variant="ghost" size="sm"
                className="min-h-11 shrink-0 gap-1.5 px-2 sm:min-h-8"
                disabled={!connected || run.cancelPending}
                aria-label={t("botRun.stopLabel", { message: preview || requestLabel })}
                onClick={() => {
                  if (chatAPI.cancelBotRun(run.run_id)) useChatStore.getState().setRunCancelPending(run.run_id, true);
                  else toast.error(t("system.disconnected"));
                }}>
                <Square className="size-3" aria-hidden="true" />{t("botRun.stop")}
              </Button>
            )}
          </div>
        );
      })}
    </div>
  );
}
