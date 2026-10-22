import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@/lib/api";
import { systemMessageText, withVisibleReplyPreview } from "@/lib/messagePresentation";

describe("system notification presentation", () => {
  it("resolves the translation envelope and parameters using the current locale", () => {
    const content = JSON.stringify({ key: "system.userJoined", params: { username: "alice" } });
    expect(systemMessageText(content, (_key, params) => `${params?.username} joined the room`)).toBe("alice joined the room");
    expect(systemMessageText(content, (_key, params) => `${params?.username} 加入了聊天室`)).toBe("alice 加入了聊天室");
    expect(systemMessageText('{"key":"system.reconnected"}', () => "Reconnected")).toBe("Reconnected");
  });

  it("preserves legacy prose and unrelated or malformed JSON", () => {
    for (const content of ["Server restarted", "{broken", "null", '{"content":"hello"}', '{"key":"other.value"}', '{"key":"system.unknown"}', '{"key":"system.userJoined","params":{"username":{}}}']) {
      expect(systemMessageText(content, (key) => key)).toBe(content);
    }
  });
});

describe("visible conversation reply previews", () => {
  const parent: ChatMessage = { id: "parent", username: "alice", content: "Private request", timestamp: 1 };
  const reply: ChatMessage = { id: "reply", username: "TokenBot", content: "Reply", reply_to_id: "parent", timestamp: 2 };

  it("fills missing historical preview fields from the visible parent", () => {
    expect(withVisibleReplyPreview(reply, new Map([[parent.id, parent]]))).toMatchObject({ reply_to_user: "alice", reply_to_content: "Private request" });
    expect(reply.reply_to_content).toBeUndefined();
  });

  it("never looks beyond the supplied conversation or reveals deleted parents", () => {
    expect(withVisibleReplyPreview(reply, new Map())).toBe(reply);
    expect(withVisibleReplyPreview(reply, new Map([[parent.id, { ...parent, deleted: true }]]))).toBe(reply);
  });

  it("preserves a complete server preview and avoids self references", () => {
    const complete = { ...reply, reply_to_user: "alice", reply_to_content: "Frozen request" };
    expect(withVisibleReplyPreview(complete, new Map([[parent.id, parent]]))).toBe(complete);
    const self = { ...reply, reply_to_id: reply.id };
    expect(withVisibleReplyPreview(self, new Map([[reply.id, self]]))).toBe(self);
  });
});
