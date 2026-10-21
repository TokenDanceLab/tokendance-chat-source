import type { ChatMessage } from "@/lib/api";

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** Resolve local system notification envelopes while keeping legacy prose. */
export function systemMessageText(content: string, t: Translate): string {
  try {
    const value: unknown = JSON.parse(content);
    if (!value || typeof value !== "object" || !("key" in value) ||
      typeof value.key !== "string" || !value.key.startsWith("system.")) return content;
    const params = "params" in value ? value.params : undefined;
    if (params !== undefined && (!params || typeof params !== "object" || Array.isArray(params) ||
      Object.values(params).some((param) => typeof param !== "string" && typeof param !== "number"))) return content;
    const translated = t(value.key, params as Record<string, string | number> | undefined);
    return translated === value.key ? content : translated;
  } catch {
    return content;
  }
}

/** The caller supplies only the visible conversation, never a global lookup. */
export function withVisibleReplyPreview(message: ChatMessage, visible: ReadonlyMap<string, ChatMessage>): ChatMessage {
  if (!message.reply_to_id || message.reply_to_id === message.id ||
    (message.reply_to_user && message.reply_to_content)) return message;
  const parent = visible.get(message.reply_to_id);
  if (!parent || parent.deleted) return message;
  return {
    ...message,
    reply_to_user: message.reply_to_user || parent.username,
    reply_to_content: message.reply_to_content || parent.content,
  };
}
