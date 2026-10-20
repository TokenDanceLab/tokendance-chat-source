import { ChevronsUpDown, Code, Hash, LogIn, LogOut, Search, Settings, X } from "lucide-react";
import { AssistantIcon } from "@/components/AssistantIcon";
import { Avatar } from "@/components/Avatar";
import { useServerConfig } from "@/hooks/useServerConfig";
import { useTranslation } from "@/i18n/context";
import { assistants, modelDisplayName, tokenBot } from "@/lib/assistantRegistry";
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
}

export function LightChatSidebar({
  activeSpace,
  activeAssistantId,
  onSelectSpace,
  onClose,
  onOpenSettings,
  onLeave,
  onAccountLogout,
}: LightChatSidebarProps) {
  const { t } = useTranslation();
  const {
    connected,
    username,
    isGuest,
    setShowAuthModal,
    onlineUsers,
    userProfiles,
    userStatusList,
    unreadByConversation,
  } = useChatStore();
  const serverConfig = useServerConfig();
  const modelLabel = modelDisplayName(serverConfig?.model || tokenBot.model.id);
  const currentProfile = username ? userProfiles[username] : undefined;
  const displayName = currentProfile?.display_name || username || t("join.buttonJoin");
  const statusText = connected
    ? isGuest
      ? t("sidebar.guestMode")
      : t("sidebar.online")
    : t("sidebar.connecting");
  const assistantNames = new Set(assistants.map((a) => a.name));
  const people = onlineUsers.filter((u) => !assistantNames.has(u) && u !== username).slice(0, 8);
  const publicUnread = unreadByConversation.public || 0;

  return (
    <aside
      className="flex h-full w-[272px] flex-col bg-sidebar text-sidebar-foreground"
      aria-label={t("chat.publicChat")}
      data-visual="light-chat-sidebar"
    >
      <div className="flex h-14 shrink-0 items-center gap-2.5 px-4">
        <img
          src="/tokendance-mark-transparent.svg"
          alt="TokenDance"
          className="size-7 shrink-0 dark:invert"
          draggable={false}
        />
        <p className="min-w-0 flex-1 truncate text-sm font-semibold tracking-[-0.01em]">
          TokenDance
        </p>
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
        <nav className="space-y-5 px-2 pb-4 pt-2" aria-label={t("chat.publicChat")}>
          <section>
            <SectionLabel>{t("sidebar.conversations")}</SectionLabel>
            <SpaceButton
              active={activeSpace === "public"}
              icon={<Hash className="size-5" />}
              title={t("sidebar.publicChat")}
              subtitle={t("sidebar.publicChatSub")}
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
                  active={
                    activeSpace === assistant.id ||
                    (activeSpace !== "public" && activeAssistantId === assistant.id)
                  }
                  icon={<AssistantIcon assistant={assistant} size="sm" />}
                  title={assistant.name}
                  subtitle={modelLabel}
                  onClick={() => onSelectSpace(assistant.id)}
                  trailing={<span className="text-label text-muted-foreground">AI</span>}
                />
              ))}
            </div>
          </section>

          {people.length > 0 && (
            <section>
              <SectionLabel>
                {t("sidebar.online")}
                <span className="ml-auto tabular-nums">{onlineUsers.length}</span>
              </SectionLabel>
              <div>
                {people.map((user) => {
                  const profile = userProfiles[user];
                  const status = userStatusList.find((u) => u.username === user);
                  return (
                    <div
                      key={user}
                      className="flex h-8 items-center gap-2.5 px-2 text-xs text-foreground/80"
                    >
                      <Avatar
                        src={profile?.avatar_url}
                        name={profile?.display_name || user}
                        size="sm"
                      />
                      <span className="min-w-0 flex-1 truncate">
                        {profile?.display_name || user}
                      </span>
                      {status?.status && (
                        <span className="max-w-24 truncate text-label text-muted-foreground">
                          {status.status}
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            </section>
          )}
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
              <Avatar src={currentProfile?.avatar_url} name={displayName} size="sm" />
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
          <DropdownMenuContent side="top" align="start" sideOffset={6} className="w-56 rounded-lg p-1">
            {isGuest && (
              <DropdownMenuItem
                className="min-h-9 rounded-md"
                onSelect={() => setShowAuthModal(true)}
              >
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
        </DropdownMenu>
      </div>
    </aside>
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
  subtitle: string;
  onClick: () => void;
  trailing?: React.ReactNode;
}

function SpaceButton({ active, icon, title, subtitle, onClick, trailing }: SpaceButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "flex h-11 w-full items-center gap-2.5 rounded-lg px-2 text-left transition-colors",
        active
          ? "bg-sidebar-accent text-sidebar-accent-foreground"
          : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
      )}
    >
      <span className="flex size-7 shrink-0 items-center justify-center">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-name font-medium">{title}</span>
        <span className="block truncate text-xs font-normal text-muted-foreground">
          {subtitle}
        </span>
      </span>
      {trailing}
    </button>
  );
}
