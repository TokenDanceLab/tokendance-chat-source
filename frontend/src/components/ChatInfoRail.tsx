import { useState } from "react";
import { Brain, Check, Clock, Globe, Hash, MessageSquareText, Sparkles, X, XCircle } from "lucide-react";
import { Avatar } from "@/components/Avatar";
import { AssistantIcon } from "@/components/AssistantIcon";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import { assistants, type AssistantDefinition } from "@/lib/assistantRegistry";
import { CHAT_EVENTS } from "@/lib/events";
import { messageSources } from "@/lib/messageSources";
import { isActiveRun } from "@/stores/botRunState";
import { useChatStore } from "@/stores/chatStore";
import { cn, formatTime } from "@/lib/utils";

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
}: {
  privateChat: boolean;
  assistant: AssistantDefinition;
  modelLabel: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const config = useServerConfig();
  const username = useChatStore((state) => state.username);
  const onlineUsers = useChatStore((state) => state.onlineUsers);
  const userProfiles = useChatStore((state) => state.userProfiles);
  const userStatusList = useChatStore((state) => state.userStatusList);
  const botRuns = useChatStore((state) => state.botRuns);
  const messages = useChatStore((state) =>
    privateChat ? state.privateBotMessages : state.messages,
  );
  const [tab, setTab] = useState<RailTab>(privateChat ? "activity" : "members");

  const activeRuns = Object.values(botRuns).filter((run) =>
    isActiveRun(run.status) &&
    run.private === privateChat &&
    (!privateChat || run.requested_by === username),
  );

  const recentRuns = Object.values(botRuns)
    .filter((run) =>
      run.private === privateChat &&
      !isActiveRun(run.status) &&
      (!privateChat || run.requested_by === username),
    )
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

  const capabilities = [
    config?.capabilities?.web_search ? { icon: Globe, label: t("info.webSearch") } : null,
    config?.capabilities?.ambient ? { icon: Sparkles, label: t("info.ambient") } : null,
    { icon: Clock, label: t("info.timeTools") },
    { icon: Brain, label: t("info.memory") },
  ].filter((item) => item !== null);

  const identity = privateChat ? (
    <section className="flex items-center gap-3">
      <AssistantIcon assistant={assistant} size="lg" className="size-14 rounded-2xl" />
      <div className="min-w-0">
        <p className="truncate text-base font-semibold">{assistant.name}</p>
        <p className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
          {t("sidebar.online")}
          <span className="text-muted-foreground/50">·</span>
          <span className="truncate">{modelLabel}</span>
        </p>
        <div className="mt-1.5 flex flex-wrap gap-x-2.5 gap-y-1 text-label text-muted-foreground">
          {capabilities.map(({ icon: Icon, label }) => (
            <span key={label} className="inline-flex items-center gap-1">
              <Icon className="size-3" aria-hidden="true" />
              {label}
            </span>
          ))}
        </div>
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

  const tabs: Array<{ key: RailTab; label: string }> = privateChat
    ? [
        { key: "activity", label: t("info.activity") },
        { key: "sources", label: t("info.recentSources") },
        { key: "thread", label: t("info.thread") },
      ]
    : [
        { key: "members", label: t("info.members") },
        { key: "activity", label: t("info.activity") },
        { key: "assistant", label: t("info.agent") },
      ];

  // Mode can change while the rail stays mounted; fall back to the first tab
  // instead of rendering a tab that only exists in the other mode.
  const activeTab = tabs.some((item) => item.key === tab) ? tab : tabs[0].key;

  const empty =
    (activeTab === "members" && members.length === 0) ||
    (activeTab === "activity" && activeRuns.length === 0 && recentRuns.length === 0) ||
    (activeTab === "sources" && latestSources.length === 0);

  return (
    <aside
      data-visual="chat-info-rail"
      data-mode={privateChat ? "private" : "public"}
      aria-label={privateChat ? t("info.title") : t("info.publicTitle")}
      className="hidden h-full w-[360px] shrink-0 flex-col border-l border-border/70 bg-sidebar/40 xl:flex"
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
          className="size-8 text-muted-foreground"
        >
          <X className="size-4" />
        </Button>
      </div>

      <div className="shrink-0 space-y-4 px-4 pb-4">
        {identity}
        <div
          role="tablist"
          aria-label={privateChat ? t("info.title") : t("info.publicTitle")}
          className="grid grid-cols-3 gap-1 rounded-lg bg-muted/60 p-1"
        >
          {tabs.map((item) => (
            <button
              key={item.key}
              type="button"
              role="tab"
              aria-selected={activeTab ===item.key}
              onClick={() => setTab(item.key)}
              className={cn(
                "h-8 truncate rounded-md px-2 text-label transition-colors focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none",
                activeTab ===item.key
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="px-4 pb-8">
          {empty && (
            <p className="px-1.5 py-3 text-xs leading-5 text-muted-foreground">
              {activeTab ==="members"
                ? t("info.noMembers")
                : activeTab ==="sources"
                  ? t("info.noSources")
                  : t("info.noActivity")}
            </p>
          )}

          {activeTab ==="members" && !privateChat && members.length > 0 && (
            <ul className="m-0 grid list-none gap-0.5 p-0">
              {members.map((member) => {
                const profile = userProfiles[member];
                const status = userStatusList.find((item) => item.username === member)?.status;
                return (
                  <li key={member} className="flex h-9 min-w-0 items-center gap-2.5 px-1.5">
                    <Avatar
                      src={profile?.avatar_url}
                      name={profile?.display_name || member}
                      size="sm"
                    />
                    <span className="min-w-0 flex-1 truncate text-xs text-foreground/85">
                      {profile?.display_name || member}
                      {member === username && (
                        <span className="ml-1 text-muted-foreground">({t("sidebar.you")})</span>
                      )}
                    </span>
                    <span className="max-w-24 truncate text-label text-muted-foreground">
                      {status || t("sidebar.online")}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}

          {activeTab ==="activity" && (
            <div className="space-y-5">
              {activeRuns.length > 0 && (
                <section>
                  <SectionLabel>{t("info.running")}</SectionLabel>
                  <div className="space-y-1.5">
                    {activeRuns.map((run) => (
                      <div
                        key={run.run_id}
                        className="flex items-center gap-2 rounded-lg bg-muted/60 px-2.5 py-2 text-xs text-muted-foreground"
                      >
                        <Spinner className="size-3" />
                        <span className="min-w-0 truncate">
                          {run.requested_by === username
                            ? t("info.replyingYou")
                            : t("info.replyingTo", { name: run.requested_by })}
                        </span>
                      </div>
                    ))}
                  </div>
                </section>
              )}

              {recentRuns.length > 0 && (
                <ul className="m-0 grid list-none gap-1 p-0">
                  {recentRuns.map((run) => {
                    const trigger = messages.find((message) => message.id === run.trigger_message_id);
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
                          className="flex w-full min-w-0 items-start gap-2.5 rounded-lg px-1.5 py-1.5 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <span
                            className={cn(
                              "mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full bg-muted",
                              run.status === "failed" ? "text-destructive" : "text-muted-foreground",
                            )}
                          >
                            <Icon className="size-3.5" aria-hidden="true" />
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-xs font-medium text-foreground">
                              {label}
                              {!privateChat && ` · ${run.requested_by}`}
                            </span>
                            {trigger?.content && (
                              <span className="mt-0.5 block truncate text-label text-muted-foreground">
                                {trigger.content.replace(/\s+/g, " ")}
                              </span>
                            )}
                          </span>
                          <span className="shrink-0 text-label tabular-nums text-muted-foreground/70">
                            {formatTime(run.timestamp ?? 0, t)}
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}

          {activeTab ==="sources" && privateChat && latestSources.length > 0 && (
            <ul className="m-0 grid list-none gap-1 p-0">
              {latestSources.slice(0, 6).map((source) => (
                <li key={source.source_id} className="min-w-0">
                  <a
                    href={source.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex min-w-0 items-start gap-2 rounded-lg px-1.5 py-1.5 transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                      <Globe className="size-3.5" aria-hidden="true" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-xs font-medium text-foreground">
                        {source.title}
                      </span>
                      <span className="mt-0.5 block truncate text-label text-muted-foreground">
                        {sourceHost(source.url)}
                      </span>
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          )}

          {activeTab ==="assistant" && !privateChat && (
            <div className="space-y-3">
              <div className="flex items-center gap-3">
                <AssistantIcon assistant={assistant} size="lg" className="size-12 rounded-2xl" />
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
            </div>
          )}

          {activeTab ==="thread" && (
            <div className="space-y-2">
              <div className="flex items-center gap-2 px-1.5 text-xs text-muted-foreground">
                <MessageSquareText className="size-3.5" aria-hidden="true" />
                {t("info.messages")}
                <span className="ml-auto tabular-nums text-foreground/80">
                  {messages.filter((message) => message.username !== "system").length}
                </span>
              </div>
              <p className="px-1.5 text-label leading-5 text-muted-foreground">
                {t("info.threadHint")}
              </p>
            </div>
          )}
        </div>
      </ScrollArea>
    </aside>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-2 flex items-center px-1.5 text-label font-medium text-muted-foreground">
      {children}
    </div>
  );
}

function sourceHost(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}
