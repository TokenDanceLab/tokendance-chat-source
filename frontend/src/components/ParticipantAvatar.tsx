import { AssistantIcon } from "@/components/AssistantIcon";
import { Avatar } from "@/components/Avatar";
import { assistants, type AssistantDefinition } from "@/lib/assistantRegistry";
import { useChatStore } from "@/stores/chatStore";

/** One participant identity resolution for menus, messages, and roster chrome. */
export function ParticipantAvatar({ username, name, assistant, size = "md", className, onClick, online }: {
  username: string;
  name?: string;
  assistant?: AssistantDefinition;
  size?: "sm" | "md" | "lg";
  className?: string;
  onClick?: () => void;
  online?: boolean;
}) {
  const profile = useChatStore((state) => state.userProfiles[username]);
  const knownAssistant = assistant ?? assistants.find((item) => item.name.toLowerCase() === username.toLowerCase());
  if (knownAssistant) {
    return <AssistantIcon assistant={knownAssistant} avatarUrl={profile?.avatar_url} size={size} className={className} />;
  }
  return <Avatar src={profile?.avatar_url} name={name || profile?.display_name || username} identityName={username}
    size={size} className={className} onClick={onClick} online={online} />;
}
