import { useState } from "react";
import {
  Bell,
  BellOff,
  BellRing,
  Clock,
  Eye,
  EyeOff,
  VolumeX,
  X,
} from "lucide-react";
import { useChatStore } from "@/stores/chatStore";
import { useTranslation } from "@/i18n/context";
import { chatAPI } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";

interface SettingsPanelProps {
  onClose: () => void;
}

function formatMuteExpiry(mutedUntil: number): string {
  if (mutedUntil <= 0) return "";
  const now = Date.now();
  if (mutedUntil <= now) return "";
  const remaining = mutedUntil - now;
  const hours = Math.ceil(remaining / (1000 * 60 * 60));
  if (hours >= 8760) return "";
  if (hours >= 24) return `${Math.floor(hours / 24)}d`;
  if (hours >= 1) return `${hours}h`;
  return "< 1h";
}

export function SettingsPanel({ onClose }: SettingsPanelProps) {
  const { t } = useTranslation();
  const { notificationPrefs, mutedConversations } = useChatStore();

  // Desktop notification permission is only ever requested from this explicit
  // user action (a real gesture), satisfying browser autoplay/permission rules.
  const [notifPermission, setNotifPermission] = useState<string>(
    typeof Notification !== "undefined" ? Notification.permission : "unsupported",
  );
  const requestNotifications = () => {
    if (typeof Notification === "undefined") return;
    Notification.requestPermission().then((p) => setNotifPermission(p));
  };

  const entries = ["public"];

  const isMuted = (key: string): boolean => {
    if (mutedConversations.includes(key)) return true;
    const pref = notificationPrefs[key];
    return !!(pref && pref.mutedUntil > Date.now());
  };

  const getMutedUntil = (key: string): number => notificationPrefs[key]?.mutedUntil ?? 0;
  const getShowPreview = (key: string): boolean => notificationPrefs[key]?.showPreview ?? true;

  const handleUnmute = (key: string) => {
    chatAPI.sendSetNotificationPrefs(key, 0, getShowPreview(key));
  };

  const handleTogglePreview = (key: string) => {
    chatAPI.sendSetNotificationPrefs(key, getMutedUntil(key), !getShowPreview(key));
  };

  const mutedEntries = entries.filter((k) => isMuted(k));
  const previewEntries = entries.filter((k) => !isMuted(k) && notificationPrefs[k]);

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent
        side="right"
        showCloseButton={false}
        data-visual="settings-modal"
        className="w-[min(384px,calc(100vw-0.75rem))]! max-w-none gap-0 bg-card p-0 text-card-foreground"
      >
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div className="flex items-center gap-3">
            <Bell className="size-4 text-muted-foreground" />
            <div>
              <SheetTitle className="text-sm font-semibold">
                {t("settings.notificationPrefs")}
              </SheetTitle>
              <p className="text-label text-muted-foreground">TokenDanceChat</p>
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            className="size-11"
            aria-label={t("a11y.close")}
          >
            <X className="size-4" />
          </Button>
        </div>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-4 py-4" data-visual="settings-content">
            {notifPermission !== "unsupported" && (
              <div className="flex items-center gap-3 rounded-lg border px-3 py-3">
                <BellRing className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="text-xs font-medium">{t("settings.desktopNotifications")}</p>
                  {notifPermission === "denied" && (
                    <p className="mt-0.5 text-micro text-muted-foreground">
                      {t("settings.notificationsDenied")}
                    </p>
                  )}
                </div>
                {notifPermission === "granted" ? (
                  <span className="text-label text-success">
                    {t("settings.notificationsGranted")}
                  </span>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={requestNotifications}
                    className="h-11 rounded-md px-3"
                  >
                    {t("settings.notificationsEnable")}
                  </Button>
                )}
              </div>
            )}

            <section>
              <h3 className="mb-2 px-1 text-xs font-medium text-muted-foreground">
                {t("settings.mutedConversations")}
              </h3>

              {mutedEntries.length === 0 ? (
                <div className="px-4 py-6 text-center">
                  <BellOff className="mx-auto mb-2 size-4 text-muted-foreground/40" />
                  <p className="text-xs text-muted-foreground/70">
                    {t("settings.noMutedConversations")}
                  </p>
                </div>
              ) : (
                <div className="overflow-hidden rounded-lg border bg-card">
                  {mutedEntries.map((key, index) => {
                    const expiry = formatMuteExpiry(getMutedUntil(key));
                    const showPreview = getShowPreview(key);
                    return (
                      <div key={key}>
                        {index > 0 && <Separator />}
                        <div className="flex items-center gap-3 px-3 py-2.5">
                          <BellOff className="size-3.5 shrink-0 text-muted-foreground/60" />
                          <div className="min-w-0 flex-1">
                            <span className="block truncate text-xs text-foreground">
                              {t("chat.publicChat")}
                            </span>
                            {expiry && (
                              <span className="flex items-center gap-1 text-micro text-muted-foreground/60">
                                <Clock className="size-3" />
                                {expiry}
                              </span>
                            )}
                          </div>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="h-9 w-9 rounded-full text-muted-foreground"
                            onClick={() => handleTogglePreview(key)}
                            title={showPreview ? t("settings.previewOn") : t("settings.previewOff")}
                          >
                            {showPreview ? <Eye className="size-3.5" /> : <EyeOff className="size-3.5" />}
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="h-9 w-9 rounded-full text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            onClick={() => handleUnmute(key)}
                            title={t("settings.unmute")}
                          >
                            <VolumeX className="size-3.5" />
                          </Button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </section>

            {previewEntries.length > 0 && (
              <section>
                <h3 className="mb-2 px-1 text-xs font-medium text-muted-foreground">
                  {t("settings.showPreview")}
                </h3>
                <div className="overflow-hidden rounded-lg border bg-card">
                  {previewEntries.slice(0, 20).map((key, index) => {
                    const showPreview = getShowPreview(key);
                    return (
                      <div key={key}>
                        {index > 0 && <Separator />}
                        <div className="flex items-center gap-3 px-3 py-2.5">
                          <Bell className="size-3.5 shrink-0 text-muted-foreground/60" />
                          <span className="min-w-0 flex-1 truncate text-xs">{t("chat.publicChat")}</span>
                          <Switch
                            checked={showPreview}
                            aria-label={showPreview ? t("settings.previewOn") : t("settings.previewOff")}
                            onCheckedChange={() => handleTogglePreview(key)}
                          />
                        </div>
                      </div>
                    );
                  })}
                </div>
              </section>
            )}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}
