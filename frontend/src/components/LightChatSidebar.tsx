import {
  ChevronsUpDown,
  Code,
  Hash,
  LogIn,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Search,
  Settings,
  X,
} from "lucide-react";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
import { useConnectionSettled } from "@/hooks/useConnectionSettled";
import { useTranslation } from "@/i18n/context";
import { assistants } from "@/lib/assistantRegistry";
import { CHAT_EVENTS } from "@/lib/events";
import { cn } from "@/lib/utils";
import { sourceUrl } from "@/lib/source";
import { useChatStore } from "@/stores/chatStore";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export type ChatSpace = "public" | string;

interface LightChatSidebarProps {
  activeSpace: ChatSpace;
  activeAssistantId: string;
  onSelectSpace: (space: ChatSpace) => void;
  onClose?: () => void;
  onOpenSettings: () => void;
  onLeave: () => void;
  onAccountLogout?: () => void;
  /** Desktop icon-rail mode; mobile sheets always render expanded. */
  collapsed?: boolean;
  onToggleCollapse?: () => void;
}

export function LightChatSidebar({
  activeSpace,
  activeAssistantId,
  onSelectSpace,
  onClose,
  onOpenSettings,
  onLeave,
  onAccountLogout,
  collapsed = false,
  onToggleCollapse,
}: LightChatSidebarProps) {
  const { t } = useTranslation();
  const {
    connected,
    username,
    isGuest,
    setShowAuthModal,
    userProfiles,
    unreadByConversation,
    activePrivateThread,
    privateThreads,
    setActivePrivateThread,
  } = useChatStore();
  const settledConnected = useConnectionSettled(connected);
  const currentProfile = username ? userProfiles[username] : undefined;
  const displayName = currentProfile?.display_name || username || t("join.buttonJoin");
  const statusText = settledConnected
    ? isGuest
      ? t("sidebar.guestMode")
      : t("sidebar.online")
    : t("sidebar.connecting");
  const publicUnread = unreadByConversation.public || 0;
  const isAssistantActive = (assistantId: string) =>
    activeSpace === assistantId ||
    (activeSpace !== "public" && activeAssistantId === assistantId);
  const activeAssistantName = assistants.find((a) => a.id === activeAssistantId)?.name ?? "TokenBot";
  // Side lanes are the assistant's private side threads; "" is the main chat.
  const lanes = privateThreads.filter((thread) => thread.thread_id !== "");
  const createSideChat = () => setActivePrivateThread(`lane-${crypto.randomUUID()}`);

  const accountMenu = (
    <DropdownMenuContent
      side={collapsed ? "right" : "top"}
      align="start"
      sideOffset={6}
      className="w-56 rounded-lg p-1"
    >
      {isGuest && (
        <DropdownMenuItem className="min-h-9 rounded-md" onSelect={() => setShowAuthModal(true)}>
          <LogIn className="size-4" />
          {t("auth.loginOrRegister")}
        </DropdownMenuItem>
      )}
      <DropdownMenuItem className="min-h-9 rounded-md" onSelect={onOpenSettings}>
        <Settings className="size-4" />
        {t("settings.openSettings")}
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem asChild className="min-h-9 rounded-md">
        <a href={sourceUrl} target="_blank" rel="noopener noreferrer">
          <Code className="size-4" />
          {t("sidebar.sourceAndLicense")}
        </a>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      {onAccountLogout && (
        <DropdownMenuItem
          variant="destructive"
          className="min-h-9 rounded-md"
          onSelect={() => {
            onClose?.();
            onAccountLogout();
          }}
        >
          <LogOut className="size-4" />
          {t("auth.accountLogout")}
        </DropdownMenuItem>
      )}
      <DropdownMenuItem
        className="min-h-9 rounded-md"
        onSelect={() => {
          onClose?.();
          onLeave();
        }}
      >
        <LogOut className="size-4" />
        {t("chat.leave")}
      </DropdownMenuItem>
    </DropdownMenuContent>
  );

  if (collapsed) {
    return (
      <aside
        className="flex h-full w-[72px] flex-col items-center bg-sidebar text-sidebar-foreground"
        aria-label={t("chat.publicChat")}
        data-visual="light-chat-sidebar"
        data-collapsed="true"
      >
        <div className="flex h-14 shrink-0 items-center justify-center">
          <img
            src="/tokendance-mark-transparent.svg"
            alt="TokenDance"
            className="size-7 shrink-0"
            draggable={false}
          />
        </div>

        <div className="flex w-full flex-col items-center gap-1 px-2 pb-2">
          <RailButton
            label={t("search.placeholder")}
            onClick={() => window.dispatchEvent(new CustomEvent(CHAT_EVENTS.openSearch))}
          >
            <Search className="size-5" />
          </RailButton>
          <RailButton
            label={t("sidebar.publicChat")}
            active={activeSpace === "public"}
            badge={publicUnread > 0}
            onClick={() => onSelectSpace("public")}
          >
            <Hash className="size-5" />
          </RailButton>
          {assistants.map((assistant) => (
            <RailButton
              key={assistant.id}
              label={assistant.name}
              active={isAssistantActive(assistant.id)}
              badge={(unreadByConversation[`dm-${assistant.name}`] || 0) > 0}
              onClick={() => onSelectSpace(assistant.id)}
            >
              <ParticipantAvatar username={assistant.name} assistant={assistant} size="sm" />
            </RailButton>
          ))}
        </div>

        <div className="mt-auto flex flex-col items-center gap-1 pb-2">
          {onToggleCollapse && (
            <RailButton label={t("a11y.expandSidebar")} onClick={onToggleCollapse}>
              <PanelLeftOpen className="size-4" />
            </RailButton>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label={t("sidebar.accountMenu")}
                className="flex size-11 items-center justify-center rounded-lg transition-colors hover:bg-sidebar-accent/60 focus-visible:ring-1 focus-visible:ring-ring/40 focus-visible:outline-none data-[state=open]:bg-sidebar-accent/60"
              >
                <ParticipantAvatar username={username || ""} name={displayName} size="sm" />
              </button>
            </DropdownMenuTrigger>
            {accountMenu}
          </DropdownMenu>
        </div>
      </aside>
    );
  }

  return (
    <aside
      className="flex h-full w-[240px] flex-col bg-sidebar text-sidebar-foreground"
      aria-label={t("chat.publicChat")}
      data-visual="light-chat-sidebar"
    >
      <div className="flex h-14 shrink-0 items-center gap-2.5 px-4">
        <img
          src="/tokendance-mark-transparent.svg"
          alt="TokenDance"
          className="size-7 shrink-0"
          draggable={false}
        />
        <p className="min-w-0 flex-1 truncate text-sm font-semibold tracking-[-0.01em]">
          TokenDance
        </p>
        {onToggleCollapse && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onToggleCollapse}
            aria-label={t("a11y.collapseSidebar")}
            className="hidden size-8 text-muted-foreground lg:inline-flex"
          >
            <PanelLeftClose className="size-4" />
          </Button>
        )}
        {onClose && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            aria-label={t("a11y.closeSidebar")}
            className="size-11 text-muted-foreground lg:hidden"
          >
            <X className="size-4" />
          </Button>
        )}
      </div>

      <div className="px-4 pb-2">
        <Button
          type="button"
          variant="ghost"
          onClick={() => window.dispatchEvent(new CustomEvent(CHAT_EVENTS.openSearch))}
          className="h-11 w-full justify-start gap-2 rounded-lg bg-background px-2.5 text-name font-normal text-muted-foreground hover:bg-background/80 lg:h-8"
          aria-label={t("search.placeholder")}
        >
          <Search className="size-4" />
          <span className="flex-1 text-left">{t("search.placeholder")}</span>
          <Kbd className="h-4 bg-muted px-1 text-micro text-muted-foreground">⌘K</Kbd>
        </Button>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <nav className="space-y-4 px-2 pb-4 pt-1" aria-label={t("chat.publicChat")}>
          <section>
            <SectionLabel>{t("sidebar.conversations")}</SectionLabel>
            <SpaceButton
              active={activeSpace === "public"}
              icon={<Hash className="size-5" />}
              title={t("sidebar.publicChat")}
              onClick={() => onSelectSpace("public")}
              trailing={
                publicUnread > 0 ? (
                  <UnreadBadge count={publicUnread} label={t("sidebar.unread")} />
                ) : null
              }
            />
          </section>

          <section>
            <SectionLabel>{t("sidebar.aiAssistants")}</SectionLabel>
            <div className="space-y-0.5">
              {assistants.map((assistant) => (
                <SpaceButton
                  key={assistant.id}
                  active={isAssistantActive(assistant.id) && activePrivateThread === ""}
                  icon={<ParticipantAvatar username={assistant.name} assistant={assistant} size="sm" />}
                  title={assistant.name}
                  onClick={() => {
                    setActivePrivateThread("");
                    onSelectSpace(assistant.id);
                  }}
                  trailing={
                    (unreadByConversation[`dm-${assistant.name}`] || 0) > 0 ? (
                      <UnreadBadge
                        count={unreadByConversation[`dm-${assistant.name}`]}
                        label={t("sidebar.unread")}
                      />
                    ) : null
                  }
                />
              ))}
            </div>
            {activeSpace !== "public" && (
              <div className="mt-1 space-y-0.5">
                {lanes.length > 0 && (
                  <div className="flex h-11 items-center justify-between pl-2 pr-1 lg:h-7">
                    <span className="text-label font-medium text-muted-foreground/80">
                      {t("sidebar.sideChats")}
                    </span>
                    <button
                      type="button"
                      aria-label={t("sidebar.newSideChat")}
                      title={t("sidebar.newSideChat")}
                      onClick={createSideChat}
                      className="flex size-11 items-center justify-center rounded-lg text-muted-foreground/70 transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring/40 lg:size-6 lg:rounded-md"
                    >
                      <Plus className="size-3.5" />
                    </button>
                  </div>
                )}
                {lanes.length === 0 ? (
                  // A lone "旁聊" header reads as an empty section; offer the
                  // first side chat as an action row instead.
                  <button
                    type="button"
                    aria-label={t("sidebar.newSideChat")}
                    onClick={createSideChat}
                    className="flex h-11 w-full items-center gap-1.5 rounded-lg pl-2 pr-2 text-left text-muted-foreground/80 transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-foreground focus-visible:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring/40 lg:h-7"
                  >
                    <Plus className="size-3 shrink-0" aria-hidden="true" />
                    <span className="min-w-0 flex-1 truncate text-label">
                      {t("sidebar.newSideChat")}
                    </span>
                  </button>
                ) : (
                  lanes.map((thread) => {
                    const laneKey = `dm-${activeAssistantName}:${thread.thread_id}`;
                    const laneUnread = unreadByConversation[laneKey] || 0;
                    return (
                      <button
                        key={thread.thread_id}
                        type="button"
                        aria-pressed={activePrivateThread === thread.thread_id}
                        title={thread.preview || t("sidebar.newChatPlaceholder")}
                        onClick={() => setActivePrivateThread(thread.thread_id)}
                        className={cn(
                          "flex h-11 w-full items-center rounded-lg pl-2 pr-2 text-left transition-colors lg:h-8",
                          activePrivateThread === thread.thread_id
                            ? "bg-sidebar-accent text-sidebar-accent-foreground"
                            : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
                        )}
                      >
                        <span className="min-w-0 flex-1 truncate text-name">
                          {thread.preview || t("sidebar.newChatPlaceholder")}
                        </span>
                        {laneUnread > 0 && (
                          <UnreadBadge count={laneUnread} label={t("sidebar.unread")} />
                        )}
                      </button>
                    );
                  })
                )}
              </div>
            )}
          </section>
        </nav>
      </ScrollArea>

      <div className="p-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={t("sidebar.accountMenu")}
              className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-sidebar-accent/60 focus-visible:bg-sidebar-accent/60 focus-visible:ring-1 focus-visible:ring-ring/40 focus-visible:outline-none data-[state=open]:bg-sidebar-accent/60"
            >
              <ParticipantAvatar username={username || ""} name={displayName} size="sm" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-name font-medium leading-5">
                  {displayName}
                </span>
                <span className="block truncate text-xs leading-4 text-muted-foreground">
                  {statusText}
                </span>
              </span>
              <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground/70" />
            </button>
          </DropdownMenuTrigger>
          {accountMenu}
        </DropdownMenu>
      </div>
    </aside>
  );
}

function RailButton({
  label,
  active = false,
  badge = false,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  badge?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      aria-label={label}
      title={label}
      className={cn(
        "relative flex size-11 items-center justify-center rounded-xl transition-colors",
        active
          ? "bg-sidebar-accent text-sidebar-accent-foreground"
          : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
      )}
    >
      {children}
      {badge && (
        <span
          aria-hidden="true"
          className="absolute right-2.5 top-2.5 size-1.5 rounded-full bg-foreground"
        />
      )}
    </button>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-1 flex items-center px-2 text-label font-medium text-muted-foreground">
      {children}
    </div>
  );
}

function UnreadBadge({ count, label }: { count: number; label: string }) {
  return (
    <span
      data-visual="sidebar-unread"
      role="status"
      aria-label={`${count} ${label}`}
      className="inline-flex h-5 min-w-5 animate-in shrink-0 items-center justify-center rounded-full bg-foreground px-1.5 text-micro font-medium tabular-nums text-background fade-in-0 zoom-in-95 duration-150"
    >
      {count > 99 ? "99+" : count}
    </span>
  );
}

interface SpaceButtonProps {
  active: boolean;
  icon: React.ReactNode;
  title: string;
  onClick: () => void;
  trailing?: React.ReactNode;
}

function SpaceButton({ active, icon, title, onClick, trailing }: SpaceButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "flex h-11 w-full items-center gap-2.5 rounded-xl px-2 text-left transition-colors lg:h-9",
        active
          ? "bg-sidebar-accent text-sidebar-accent-foreground"
          : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
      )}
    >
      <span className="flex size-6 shrink-0 items-center justify-center">{icon}</span>
      <span className="min-w-0 flex-1 truncate text-name font-medium">{title}</span>
      {trailing}
    </button>
  );
}
