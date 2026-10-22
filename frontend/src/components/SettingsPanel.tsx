import { useEffect, useState } from "react";
import {
  Ban,
  Bell,
  BellOff,
  BellRing,
  Clock,
  Code2,
  Eye,
  EyeOff,
  Globe,
  Sparkles,
  Monitor,
  Moon,
  Sun,
  VolumeX,
  X,
} from "lucide-react";
import { useChatStore } from "@/stores/chatStore";
import { useTranslation } from "@/i18n/context";
import type { Language } from "@/i18n/translations";
import { Avatar } from "@/components/Avatar";
import { useServerConfig } from "@/hooks/useServerConfig";
import { assistants, modelDisplayName } from "@/lib/assistantRegistry";
import { chatAPI } from "@/lib/api";
import { sourceUrl } from "@/lib/source";
import { THEME_STORAGE_KEY, type Theme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";

interface SettingsPanelProps {
  onClose: () => void;
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
  lang: Language;
  onLangChange: (lang: Language) => void;
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

const THEME_OPTIONS: Array<{ value: Theme; icon: typeof Sun }> = [
  { value: "light", icon: Sun },
  { value: "dark", icon: Moon },
  { value: "system", icon: Monitor },
];

export function SettingsPanel({ onClose, theme, onThemeChange, lang, onLangChange }: SettingsPanelProps) {
  const { t } = useTranslation();
  const {
    notificationPrefs,
    mutedConversations,
    showReasoning,
    setShowReasoning,
    blockedUsers,
    removeBlockedUser,
    connected,
    username,
    isGuest,
    userProfiles,
  } = useChatStore();
  const config = useServerConfig();

  // Desktop notification permission is only ever requested from this explicit
  // user action (a real gesture), satisfying browser autoplay/permission rules.
  const [notifPermission, setNotifPermission] = useState<string>(
    typeof Notification !== "undefined" ? Notification.permission : "unsupported",
  );
  const requestNotifications = () => {
    if (typeof Notification === "undefined") return;
    Notification.requestPermission().then((p) => setNotifPermission(p));
  };

  // The permission can change in browser UI while the sheet is open.
  useEffect(() => {
    if (typeof Notification === "undefined") return;
    const refresh = () => setNotifPermission(Notification.permission);
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, []);

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
  const themeLabels: Record<Theme, string> = {
    light: t("settings.themeLight"),
    dark: t("settings.themeDark"),
    system: t("settings.themeSystem"),
  };

  const updateTheme = (next: Theme) => {
    localStorage.setItem(THEME_STORAGE_KEY, next);
    onThemeChange(next);
  };

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent
        side="right"
        showCloseButton={false}
        data-visual="settings-modal"
        className="w-[min(440px,calc(100vw-0.75rem))]! max-w-none gap-0 bg-card p-0 text-card-foreground"
      >
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div>
            <SheetTitle className="text-sm font-semibold">{t("settings.title")}</SheetTitle>
            <p className="text-label text-muted-foreground">TokenDanceChat</p>
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
          <div className="space-y-7 px-5 py-5" data-visual="settings-content">
            <section>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                {t("settings.accountSection")}
              </h3>
              <div className="flex items-center gap-3 rounded-lg bg-muted/40 px-3 py-2.5">
                <Avatar
                  src={username ? userProfiles[username]?.avatar_url : undefined}
                  name={username ? userProfiles[username]?.display_name || username : "?"}
                  size="md"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs font-medium text-foreground">
                    {username || t("join.buttonJoin")}
                  </p>
                  <p className="mt-0.5 text-label text-muted-foreground">
                    {isGuest ? t("settings.accountGuest") : t("settings.accountMember")}
                  </p>
                </div>
              </div>
            </section>

            <section>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                {t("settings.appearance")}
              </h3>
              <div className="grid grid-cols-3 gap-1 rounded-lg bg-muted/60 p-1" role="group">
                {THEME_OPTIONS.map(({ value, icon: Icon }) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={theme === value}
                    onClick={() => updateTheme(value)}
                    className={cn(
                      "flex h-11 items-center justify-center gap-1.5 rounded-md text-xs transition-colors sm:h-9 focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none",
                      theme === value
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon className="size-3.5" aria-hidden="true" />
                    {themeLabels[value]}
                  </button>
                ))}
              </div>
              <div className="mt-3 flex items-center justify-between rounded-lg px-1.5 py-1.5">
                <span className="text-xs text-foreground">{t("settings.language")}</span>
                <div className="flex gap-1 rounded-md bg-muted/60 p-0.5" role="group" aria-label={t("settings.language")}>
                  {([["zh-CN", "中文"], ["en-US", "English"]] as const).map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={lang === value}
                      onClick={() => onLangChange(value)}
                      className={cn(
                        "h-11 min-w-11 rounded px-2.5 text-label transition-colors sm:h-7 focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none",
                        lang === value
                          ? "bg-background text-foreground"
                          : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
            </section>

            <section>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                {t("settings.assistantSection")}
              </h3>
              <div className="space-y-1">
                <SettingRow
                  icon={<Eye className="size-4" />}
                  title={t("settings.showReasoning")}
                  description={t("settings.showReasoningHint")}
                  control={
                    <Switch
                      checked={showReasoning}
                      aria-label={t("settings.showReasoning")}
                      onCheckedChange={setShowReasoning}
                    />
                  }
                />
                <SettingRow
                  icon={<Sparkles className="size-4" />}
                  title={t("settings.ambientStatus")}
                  description={t("settings.ambientHint")}
                  control={
                    <span
                      className={cn(
                        "text-label",
                        config?.capabilities?.ambient ? "text-success" : "text-muted-foreground",
                      )}
                    >
                      {config?.capabilities?.ambient
                        ? t("settings.capabilityOn")
                        : t("settings.capabilityOff")}
                    </span>
                  }
                />
                <SettingRow
                  icon={<Globe className="size-4" />}
                  title={t("settings.webSearchStatus")}
                  description={config?.model ? modelDisplayName(config.model) : undefined}
                  control={
                    <span
                      className={cn(
                        "text-label",
                        config?.capabilities?.web_search ? "text-success" : "text-muted-foreground",
                      )}
                    >
                      {config?.capabilities?.web_search
                        ? t("settings.capabilityOn")
                        : t("settings.capabilityOff")}
                    </span>
                  }
                />
              </div>
            </section>

            <section>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                {t("settings.notifications")}
              </h3>
              <div className="space-y-1">
                {notifPermission !== "unsupported" && (
                  <SettingRow
                    icon={<BellRing className="size-4" />}
                    title={t("settings.desktopNotifications")}
                    description={
                      notifPermission === "denied"
                        ? t("settings.notificationsDenied")
                        : undefined
                    }
                    control={
                      notifPermission === "granted" ? (
                        <span className="text-label text-success">
                          {t("settings.notificationsGranted")}
                        </span>
                      ) : notifPermission === "denied" ? (
                        <span className="text-label text-muted-foreground">
                          {t("settings.notificationsBlocked")}
                        </span>
                      ) : (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={requestNotifications}
                          className="h-11 rounded-md px-2.5 text-xs sm:h-8"
                        >
                          {t("settings.notificationsEnable")}
                        </Button>
                      )
                    }
                  />
                )}
                {assistants.map((assistant) => {
                  const key = `dm-${assistant.name}`;
                  return (
                    <SettingRow
                      key={key}
                      icon={<Bell className="size-4" />}
                      title={`${assistant.name} ${t("settings.notifications")}`}
                      description={t("settings.privateNotificationHint")}
                      control={
                        <Switch
                          checked={!isMuted(key)}
                          disabled={!connected}
                          className="after:-inset-y-3.5"
                          aria-label={`${assistant.name} ${t("settings.notifications")}`}
                          onCheckedChange={(enabled) => chatAPI.sendSetNotificationPrefs(
                            key, enabled ? 0 : Number.MAX_SAFE_INTEGER, false,
                          )}
                        />
                      }
                    />
                  );
                })}
                {previewEntries.slice(0, 20).map((key) => {
                  const showPreview = getShowPreview(key);
                  return (
                    <SettingRow
                      key={key}
                      icon={<Bell className="size-4" />}
                      title={t("chat.publicChat")}
                      description={t("settings.showPreview")}
                      control={
                        <Switch
                          checked={showPreview}
                          aria-label={showPreview ? t("settings.previewOn") : t("settings.previewOff")}
                          onCheckedChange={() => handleTogglePreview(key)}
                        />
                      }
                    />
                  );
                })}
                {mutedEntries.map((key) => {
                  const expiry = formatMuteExpiry(getMutedUntil(key));
                  return (
                    <SettingRow
                      key={key}
                      icon={<BellOff className="size-4" />}
                      title={t("chat.publicChat")}
                      description={expiry ? `${t("settings.mutedConversations")} · ${expiry}` : undefined}
                      control={
                        <div className="flex items-center gap-1">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="size-11 rounded-full text-muted-foreground sm:size-8"
                            onClick={() => handleTogglePreview(key)}
                            title={
                              getShowPreview(key) ? t("settings.previewOn") : t("settings.previewOff")
                            }
                            aria-label={
                              getShowPreview(key) ? t("settings.previewOn") : t("settings.previewOff")
                            }
                          >
                            {getShowPreview(key) ? (
                              <Eye className="size-3.5" />
                            ) : (
                              <EyeOff className="size-3.5" />
                            )}
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="size-11 rounded-full text-muted-foreground hover:bg-destructive/10 hover:text-destructive sm:size-8"
                            onClick={() => handleUnmute(key)}
                            title={t("settings.unmute")}
                            aria-label={t("settings.unmute")}
                          >
                            <VolumeX className="size-3.5" />
                          </Button>
                        </div>
                      }
                    />
                  );
                })}
                {mutedEntries.length === 0 && previewEntries.length === 0 &&
                  assistants.every((assistant) => !isMuted(`dm-${assistant.name}`)) && (
                  <p className="flex items-center gap-2 px-1.5 py-2 text-xs text-muted-foreground">
                    <Clock className="size-3.5" aria-hidden="true" />
                    {t("settings.noMutedConversations")}
                  </p>
                )}
              </div>
            </section>

            {blockedUsers.length > 0 && (
              <section>
                <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                  {t("settings.blockedUsers")}
                </h3>
                <div className="space-y-1">
                  {blockedUsers.map((name) => (
                    <SettingRow
                      key={name}
                      icon={<Ban className="size-4" />}
                      title={name}
                      control={
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-11 rounded-md px-2.5 text-xs sm:h-8"
                          onClick={() => {
                            chatAPI.sendUnblock(name);
                            removeBlockedUser(name);
                          }}
                        >
                          {t("settings.unblock")}
                        </Button>
                      }
                    />
                  ))}
                </div>
              </section>
            )}

            <Separator />

            <section>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground">
                {t("settings.about")}
              </h3>
              <a
                href={sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2.5 rounded-lg px-1.5 py-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
              >
                <Code2 className="size-4" aria-hidden="true" />
                {t("sidebar.sourceAndLicense")}
              </a>
            </section>
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function SettingRow({
  icon,
  title,
  description,
  control,
}: {
  icon: React.ReactNode;
  title: string;
  description?: string;
  control: React.ReactNode;
}) {
  return (
    <div className="flex min-h-11 items-center gap-3 rounded-lg px-1.5 py-1.5">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-xs font-medium text-foreground">{title}</p>
        {description && (
          <p className="mt-0.5 line-clamp-2 text-label leading-4 text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      <div className="shrink-0">{control}</div>
    </div>
  );
}
