import type { ChatMessage } from "@/lib/api";

export type MergeDirection = "prepend" | "append";

export interface MergeResult {
  messages: ChatMessage[];
  addedCount: number;
  /** Which side messages were dropped from, if the cap was exceeded. */
  droppedSide: "head" | "tail" | null;
  /** Monotonic counter — bumps on every merge that changes the array. */
  revision: number;
}

let revisionCounter = 0;

/**
 * Merges `incoming` messages into `current` in the given `direction`.
 *
 * Invariants (tested):
 *  - `prepend`: older messages at front, truncate from TAIL (keep newest).
 *  - `append`: new message at end, truncate from HEAD (keep newest).
 *  - Stable chronological ordering.
 *  - Dedup by stable message ID.
 *  - Optimistic-echo replacement: a server echo matching a `client_message_id`
 *    replaces the optimistic entry in place, preserving reactions.
 *  - Fallback-echo replacement: if no `client_message_id` match, scan the last 5
 *    messages for an optimistic twin (same content + username within 5 s).
 *  - `revision` bumps only when the array actually changes.
 */
export function mergeMessageWindow(
  current: ChatMessage[],
  incoming: ChatMessage[],
  direction: MergeDirection,
  cap: number,
): MergeResult {
  if (incoming.length === 0) {
    return { messages: current, addedCount: 0, droppedSide: null, revision: revisionCounter };
  }

  let messages = [...current];
  const existingIDs = new Set(messages.map((m) => m.id));
  const newMessages: ChatMessage[] = [];

  for (const msg of incoming) {
    // Optimistic echo: replace by client_message_id.
    if (msg.client_message_id) {
      const idx = messages.findIndex((m) => m.id === msg.client_message_id);
      if (idx >= 0) {
        // Keep the optimistic slot's timestamp so the bubble does not jump
        // when the server echo lands a few milliseconds later. Reactions and
        // the settled delivery state belong to the visible row as well.
        messages[idx] = {
          ...msg,
          timestamp: messages[idx].timestamp,
          reactions: messages[idx].reactions,
          failed: false,
          failureReason: undefined,
        };
        // The real server ID is now in the array; add it to existingIDs so
        // the dedup below doesn't re-add this message.
        existingIDs.add(msg.id);
        continue;
      }
    }

    // Fallback: scan last 5 messages for an optimistic twin.
    if (!msg.id.startsWith("optimistic_")) {
      const start = Math.max(0, messages.length - 5);
      let replaced = false;
      for (let i = messages.length - 1; i >= start; i--) {
        const m = messages[i];
        if (
          m.id.startsWith("optimistic_") &&
          m.username === msg.username &&
          m.content === msg.content &&
          Math.abs(m.timestamp - msg.timestamp) < 5000
        ) {
          messages[i] = {
            ...msg,
            timestamp: m.timestamp,
            reactions: m.reactions,
            failed: false,
            failureReason: undefined,
          };
          existingIDs.add(msg.id);
          replaced = true;
          break;
        }
      }
      if (replaced) continue;
    }

    // A persisted delivery replaces its draft by the same stable message ID.
    // History can arrive before the live final after a reconnect.
    if (existingIDs.has(msg.id)) {
      const index = messages.findIndex((message) => message.id === msg.id && message.streaming === true);
      if (index >= 0 && !msg.streaming) messages[index] = { ...msg, streaming: false };
      continue;
    }

    newMessages.push(msg);
    existingIDs.add(msg.id);
  }

  if (newMessages.length === 0 && messages.length === current.length) {
    return { messages, addedCount: 0, droppedSide: null, revision: revisionCounter };
  }

  // Merge in the requested direction.
  if (direction === "prepend") {
    messages = [...newMessages, ...messages];
  } else {
    messages = [...messages, ...newMessages];
  }

  const addedCount = newMessages.length;
  let droppedSide: "head" | "tail" | null = null;

  // Enforce cap.
  if (messages.length > cap) {
    if (direction === "prepend") {
      // Prepend: keep the head (oldest messages), drop from the tail.
      messages.length = cap;
      droppedSide = "tail";
    } else {
      // Append: keep the tail (newest messages), drop from the head.
      messages = messages.slice(messages.length - cap);
      droppedSide = "head";
    }
  }

  revisionCounter += 1;
  return { messages, addedCount, droppedSide, revision: revisionCounter };
}