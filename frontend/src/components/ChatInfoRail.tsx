import { useMemo, useState, type KeyboardEvent } from "react";
import {
  Activity,
  Bot,
  Brain,
  Check,
  Clock,
  Globe,
  Hash,
  MessageSquareText,
  Sparkles,
  Users,
  X,
  XCircle,
} from "lucide-react";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import { assistants, type AssistantDefinition } from "@/lib/assistantRegistry";
import { CHAT_EVENTS } from "@/lib/events";
import { messageSources } from "@/lib/messageSources";
import { isActiveRun } from "@/stores/botRunState";
import {useChatStore, privateLaneOf } from "@/stores/chatStore";
import { cn } from "@/lib/utils";

type RailTab = "activity" | "members" | "assistant" | "sources" | "thread";

/**
 * Wide-screen companion panel. Like Muse's assistant panel it keeps identity
 * on top and moves activity, roster and sources behind a compact tab row.
 */
export function ChatInfoRail({
  privateChat,
  assistant,
  modelLabel,
  onClose,
  embedded = false,
}: {
  privateChat: boolean;
  assistant: AssistantDefinition;
  modelLabel: string;
  onClose: () => void;
  embedded?: boolean;
}) {
  const { t, lang } = useTranslation();
  const config = useServerConfig();
  const username = useChatStore((state) => state.username);
  const onlineUsers = useChatStore((state) => state.onlineUsers);
  const userProfiles = useChatStore((state) => state.userProfiles);
  const userStatusList = useChatStore((state) => state.userStatusList);
  const botRuns = useChatStore((state) => state.botRuns);
  const activePrivateThread = useChatStore((state) => state.activePrivateThread);
  const rawMessages = useChatStore((state) =>
    privateChat ? state.privateBotMessages : state.messages,
  );
  // The rail describes the conversation in view, not every lane at once.
  const messages = useMemo(() => {
    if (!privateChat) return rawMessages;
    const byId = new Map(rawMessages.map((message) => [message.id, message]));
    return rawMessages.filter((message) => privateLaneOf(message, byId) === activePrivateThread);
  }, [activePrivateThread, privateChat, rawMessages]);
  const [tab, setTab] = useState<RailTab>(privateChat ? "activity" : "members");

  const inScope = (run: (typeof botRuns)[string]) =>
    run.private === privateChat &&
    (!privateChat || (run.requested_by === username && (run.thread_id ?? "") === activePrivateThread));
  const activeRuns = Object.values(botRuns).filter((run) => isActiveRun(run.status) && inScope(run));

  const recentRuns = Object.values(botRuns)
    .filter((run) => !isActiveRun(run.status) && inScope(run))
    .sort((a, b) => (b.timestamp ?? 0) - (a.timestamp ?? 0))
    .slice(0, 8);

  const assistantNames = new Set(assistants.map((item) => item.name));
  const members = onlineUsers
    .filter((user) => !assistantNames.has(user))
    .sort((a, b) => (a === username ? -1 : b === username ? 1 : a.localeCompare(b)));

  const latestSources = [...messages]
    .reverse()
    .filter((message) => message.username === assistant.name)
    .map((message) => messageSources(message.parts ?? []))
    .find((sources) => sources.length > 0) ?? [];

  // Muse groups its activity rail by day with 14px rows; keep the same
  // reading rhythm instead of a flat list of timestamps.
  const runGroups: Array<{ key: string; label: string; runs: typeof recentRuns }> = [];
  for (const run of recentRuns) {
    const timestamp = run.timestamp ?? 0;
    const key = dayKey(timestamp);
    let group = runGroups.find((item) => item.key === key);
    if (!group) {
      group = { key, label: dayLabel(timestamp, lang, t), runs: [] };
      runGroups.push(group);
    }
    group.runs.push(run);
  }

  const capabilities = [
    config?.capabilities?.web_search ? { icon: Globe, label: t("info.webSearch") } : null,
    config?.capabilities?.ambient ? { icon: Sparkles, label: t("info.ambient") } : null,
    { icon: Clock, label: t("info.timeTools") },
    { icon: Brain, label: t("info.memory") },
  ].filter((item) => item !== null);

  const identity = privateChat ? (
    <section className="flex flex-col items-center gap-2 pb-1 text-center">
      <ParticipantAvatar username={assistant.name} assistant={assistant} size="lg" className="size-16 rounded-[20px]" />
      <div className="min-w-0">
        <p className="truncate text-[17px] font-semibold leading-6">{assistant.name}</p>
        <p className="mt-0.5 flex items-center justify-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
          {t("sidebar.online")}
          <span className="text-muted-foreground/50">·</span>
          <span className="truncate">{modelLabel}</span>
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-x-3 gap-y-1 text-label text-muted-foreground">
        {capabilities.map(({ icon: Icon, label }) => (
          <span key={label} className="inline-flex items-center gap-1">
            <Icon className="size-3" aria-hidden="true" />
            {label}
          </span>
        ))}
      </div>
    </section>
  ) : (
    <section className="flex items-center gap-3">
      <span className="flex size-14 shrink-0 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
        <Hash className="size-5" aria-hidden="true" />
      </span>
      <div className="min-w-0">
        <p className="truncate text-base font-semibold">{t("sidebar.publicChat")}</p>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">
          {t("sidebar.publicChatSub")} · {t("chat.onlineCount", { count: members.length })}
        </p>
      </div>
    </section>
  );

  const tabs: Array<{ key: RailTab; label: string; icon: typeof Activity }> = privateChat
    ? [
        { key: "activity", label: t("info.activity"), icon: Activity },
        { key: "sources", label: t("info.recentSources"), icon: Globe },
        { key: "thread", label: t("info.thread"), icon: MessageSquareText },
      ]
    : [
        { key: "members", label: t("info.members"), icon: Users },
        { key: "activity", label: t("info.activity"), icon: Activity },
        { key: "assistant", label: t("info.agent"), icon: Bot },
      ];

  // Mode can change while the rail stays mounted; fall back to the first tab
  // instead of rendering a tab that only exists in the other mode.
  const activeTab = tabs.some((item) => item.key === tab) ? tab : tabs[0].key;
  const activeTabIndex = tabs.findIndex((item) => item.key === activeTab);

  const empty =
    (activeTab === "activity" && activeRuns.length === 0 && recentRuns.length === 0) ||
    (activeTab === "sources" && latestSources.length === 0);

  const handleTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let nextIndex: number | null = null;
    if (event.key === "ArrowRight" || event.key === "ArrowDown") {
      nextIndex = (index + 1) % tabs.length;
    } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
      nextIndex = (index - 1 + tabs.length) % tabs.length;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = tabs.length - 1;
    }
    if (nextIndex === null) return;

    event.preventDefault();
    const nextTab = tabs[nextIndex];
    const target = event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>(
      '[role="tab"]',
    )[nextIndex];
    target?.focus();
    setTab(nextTab.key);
  };

  return (
    <aside
      data-visual="chat-info-rail"
      data-mode={privateChat ? "private" : "public"}
      aria-label={privateChat ? t("info.title") : t("info.publicTitle")}
      className={cn(
        "h-full shrink-0 flex-col border-border/70 bg-sidebar/40",
        embedded
          ? "flex w-full border-l-0 bg-sidebar"
          : "hidden w-[360px] border-l min-[1368px]:flex",
      )}
    >
      <div className="flex h-14 shrink-0 items-center justify-between pl-4 pr-2">
        <h2 className="truncate text-sm font-semibold tracking-[-0.01em]">
          {privateChat ? t("info.title") : t("info.publicTitle")}
        </h2>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onClose}
          aria-label={t("a11y.closeInfo")}
          className={cn("text-muted-foreground", embedded ? "size-11" : "size-8")}
        >
          <X className="size-4" />
        </Button>
      </div>

      <div className="shrink-0 space-y-4 px-4 pb-4">
        {identity}
        <div
          role="tablist"
          aria-label={privateChat ? t("info.title") : t("info.publicTitle")}
          className="relative grid grid-cols-3 gap-1 rounded-lg bg-muted/60 p-1"
        >
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-y-1 left-1 w-[calc((100%-1rem)/3)] rounded-md bg-background shadow-sm transition-transform duration-200 ease-out motion-reduce:transition-none"
            style={{
              transform: `translateX(calc(${Math.max(0, activeTabIndex)} * (100% + 0.25rem)))`,
            }}
          />
          {tabs.map((item, index) => (
            <button
              key={item.key}
              id={`info-tab-${item.key}`}
              type="button"
              role="tab"
              aria-selected={activeTab === item.key}
              aria-controls={`info-panel-${item.key}`}
              tabIndex={activeTab === item.key ? 0 : -1}
              title={item.label}
              onClick={() => setTab(item.key)}
              onKeyDown={(event) => handleTabKeyDown(event, index)}
              className={cn(
                "relative z-10 flex items-center justify-center gap-1 truncate rounded-md text-xs transition-colors focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none",
                embedded ? "h-11 px-1.5" : "h-8 px-2",
                activeTab === item.key
                  ? "font-medium text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <item.icon className="size-3.5 shrink-0" aria-hidden="true" />
              {item.label}
            </button>
          ))}
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div
          key={activeTab}
          id={`info-panel-${activeTab}`}
          role="tabpanel"
          aria-labelledby={`info-tab-${activeTab}`}
          tabIndex={0}
          className="animate-in px-4 pb-8 duration-150 fade-in motion-reduce:animate-none"
        >
          {empty && (
            <p className="px-1.5 py-3 text-xs leading-5 text-muted-foreground">
              {activeTab === "sources" ? t("info.noSources") : t("info.noActivity")}
            </p>
          )}

          {activeTab === "members" && !privateChat && (
            <ul className="m-0 grid list-none gap-0.5 p-0">
              <li className="flex h-10 min-w-0 items-center gap-2.5 px-2">
                <ParticipantAvatar username={assistant.name} assistant={assistant} size="sm" />
                <span className="min-w-0 flex-1 truncate text-[13px] leading-5 text-foreground/85">
                  {assistant.name}
                  <span className="ml-1 text-label text-muted-foreground">AI</span>
                </span>
                <span className="max-w-24 truncate text-xs text-muted-foreground">
                  {t("sidebar.online")}
                </span>
              </li>
              {members.map((member) => {
                const profile = userProfiles[member];
                const status = userStatusList.find((item) => item.username === member)?.status;
                return (
                  <li key={member} className="flex h-10 min-w-0 items-center gap-2.5 px-2">
                    <ParticipantAvatar username={member} size="sm" />
                    <span className="min-w-0 flex-1 truncate text-[13px] leading-5 text-foreground/85">
                      {profile?.display_name || member}
                      {member === username && (
                        <span className="ml-1 text-muted-foreground">({t("sidebar.you")})</span>
                      )}
                    </span>
                    <span className="max-w-24 truncate text-xs text-muted-foreground">
                      {status || t("sidebar.online")}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}

          {activeTab === "activity" && (
            <div className="space-y-3">
              {activeRuns.length > 0 && (
                <section className="flex flex-col gap-2">
                  <h3 className="px-2 pt-2 text-[16px] font-medium leading-[22px]">
                    {t("info.running")}
                  </h3>
                  {activeRuns.map((run) => {
                    const trigger = messages.find(
                      (message) => message.id === run.trigger_message_id,
                    );
                    const preview = trigger?.content || run.trigger_preview;
                    return (
                      <div key={run.run_id} className="flex items-start gap-2 px-2 py-2">
                        <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] bg-muted text-muted-foreground">
                          <Spinner className="size-5" />
                        </span>
                        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                          <p className="break-words text-[14px] font-medium leading-5 text-foreground">
                            {run.requested_by === username
                              ? t("info.replyingYou")
                              : t("info.replyingTo", { name: run.requested_by })}
                          </p>
                          {preview && (
                            <p className="line-clamp-2 break-words text-[13px] leading-[18px] text-muted-foreground">
                              {preview.replace(/\s+/g, " ")}
                            </p>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </section>
              )}

              {runGroups.map((group) => (
                <section key={group.key} className="flex flex-col gap-2">
                  <h3 className="px-2 pt-2 text-[16px] font-medium leading-[22px]">
                    {group.label}
                  </h3>
                  <ul className="m-0 grid list-none gap-0.5 p-0">
                    {group.runs.map((run) => {
                      const trigger = messages.find(
                        (message) => message.id === run.trigger_message_id,
                      );
                      const preview = trigger?.content || run.trigger_preview;
                      const Icon =
                        run.status === "completed" ? Check : run.status === "failed" ? XCircle : X;
                      const label =
                        run.status === "completed"
                          ? t("botRun.completed")
                          : run.status === "failed"
                            ? t("botRun.failed")
                            : t("botRun.cancelled");
                      return (
                        <li key={run.run_id} className="min-w-0">
                          <button
                            type="button"
                            onClick={() => {
                              if (!run.trigger_message_id) return;
                              window.dispatchEvent(
                                new CustomEvent(CHAT_EVENTS.scrollToMessage, {
                                  detail: { id: run.trigger_message_id },
                                }),
                              );
                            }}
                            className="flex w-full min-w-0 items-start gap-2 rounded-[10px] px-2 py-2 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          >
                            <span
                              className={cn(
                                "flex size-10 shrink-0 items-center justify-center rounded-[10px] bg-muted",
                                run.status === "failed"
                                  ? "text-destructive"
                                  : "text-muted-foreground",
                              )}
                            >
                              <Icon className="size-5" aria-hidden="true" />
                            </span>
                            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                              <span className="break-words text-[14px] font-medium leading-5 text-foreground">
                                {label}
                                {!privateChat && ` · ${run.requested_by}`}
                              </span>
                              {preview && (
                                <span className="line-clamp-2 break-words text-[13px] leading-[18px] text-muted-foreground">
                                  {preview.replace(/\s+/g, " ")}
                                </span>
                              )}
                              <span className="text-xs leading-4 tabular-nums text-muted-foreground">
                                {formatClock(run.timestamp ?? 0, lang)}
                              </span>
                            </span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                </section>
              ))}
            </div>
          )}

          {activeTab === "sources" && privateChat && latestSources.length > 0 && (
            <ul className="m-0 grid list-none gap-1 p-0">
              {latestSources.slice(0, 6).map((source) => (
                <li key={source.source_id} className="min-w-0">
                  <a
                    href={source.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex min-w-0 items-start gap-2 rounded-[10px] px-2 py-2 transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] bg-muted text-muted-foreground">
                      <Globe className="size-5" aria-hidden="true" />
                    </span>
                    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="break-words text-[14px] font-medium leading-5 text-foreground">
                        {source.title}
                      </span>
                      <span className="break-words text-[13px] leading-[18px] text-muted-foreground">
                        {sourceHost(source.url)}
                      </span>
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          )}

          {activeTab === "assistant" && !privateChat && (
            <div className="space-y-3">
              <div className="flex items-center gap-3">
                <ParticipantAvatar username={assistant.name} assistant={assistant} size="lg" className="size-12 rounded-2xl" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold">{assistant.name}</p>
                  <p className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
                    {t("sidebar.online")}
                    <span className="text-muted-foreground/50">·</span>
                    <span className="truncate">{modelLabel}</span>
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
                {capabilities.map(({ icon: Icon, label }) => (
                  <span key={label} className="inline-flex items-center gap-1">
                    <Icon className="size-3.5" aria-hidden="true" />
                    {label}
                  </span>
                ))}
              </div>
              {config?.capabilities?.ambient && (
                <p className="rounded-[10px] bg-muted/60 px-3 py-2 text-[13px] leading-[18px] text-muted-foreground">
                  {t("info.ambientHint")}
                </p>
              )}
            </div>
          )}

          {activeTab === "thread" && (
            <div className="space-y-3">
              <div className="grid grid-cols-3 gap-2 px-1">
                <ThreadStat
                  label={t("info.messages")}
                  value={messages.filter((message) => message.username !== "system").length}
                />
                <ThreadStat label={t("info.sources")} value={latestSources.length} />
                <ThreadStat label={t("info.runs")} value={recentRuns.length} />
              </div>
              <p className="px-1.5 text-xs leading-5 text-muted-foreground">
                {t("info.threadHint")}
              </p>
            </div>
          )}
        </div>
      </ScrollArea>
    </aside>
  );
}

function ThreadStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[10px] bg-muted/60 px-3 py-2.5">
      <p className="text-[18px] font-semibold leading-6 tabular-nums text-foreground">{value}</p>
      <p className="mt-0.5 text-label text-muted-foreground">{label}</p>
    </div>
  );
}

function dayKey(timestamp: number): string {
  const date = new Date(timestamp);
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function dayLabel(
  timestamp: number,
  lang: string,
  t: (key: string) => string,
): string {
  const date = new Date(timestamp);
  const today = new Date();
  if (isSameDay(date, today)) return t("info.today");
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (isSameDay(date, yesterday)) return t("info.yesterday");
  return new Intl.DateTimeFormat(lang, { month: "short", day: "numeric" }).format(date);
}

function formatClock(timestamp: number, lang: string): string {
  if (!timestamp) return "";
  return new Intl.DateTimeFormat(lang, { hour: "2-digit", minute: "2-digit" }).format(
    new Date(timestamp),
  );
}

function sourceHost(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}
