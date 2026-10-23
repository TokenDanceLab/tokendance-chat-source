import { describe, expect, it } from "vitest";
import { prepareMessages, runtimeMessageId } from "@/components/assistant-ui/chat-thread";
import type { ChatMessage } from "@/lib/api";

function message(id: string, timestamp: number, overrides: Partial<ChatMessage> = {}): ChatMessage {
  return { id, username: "alice", content: id, timestamp, ...overrides };
}

describe("prepareMessages", () => {
  it("groups consecutive messages from the same speaker inside two minutes", () => {
    const prepared = prepareMessages(
      [
        message("a", 1_000),
        message("b", 1_000 + 30_000),
        message("c", 1_000 + 30_000 + 3 * 60_000),
      ],
      "alice",
    );
    expect(prepared.map((item) => item.groupPosition)).toEqual(["first", "last", "single"]);
    expect(prepared[1].hideAvatar).toBe(true);
    expect(prepared[1].hideUsername).toBe(true);
    // Only the last row of a group carries the timestamp in the public thread.
    expect(prepared[0].forceShowTimestamp).toBe(false);
    expect(prepared[1].forceShowTimestamp).toBe(true);
  });

  it("removes identity chrome and per-row timestamps in the minimal thread", () => {
    const prepared = prepareMessages(
      [message("a", 1_000), message("b", 3_000, { username: "TokenBot" })],
      "alice",
      true,
    );
    expect(prepared.every((item) => item.hideAvatar && item.hideUsername)).toBe(true);
    expect(prepared.every((item) => item.forceShowTimestamp === false)).toBe(true);
  });

  it("adds a time marker after a quiet gap but not between adjacent rows", () => {
    const prepared = prepareMessages(
      [
        message("a", 1_000),
        message("b", 2_000),
        message("c", 2_000 + 6 * 60_000),
      ],
      "alice",
      true,
    );
    expect(prepared.map((item) => item.showTime)).toEqual([false, false, true]);
  });

  it("shows a date marker instead of a time marker on a new day", () => {
    const dayOne = new Date(2026, 9, 20, 23, 55).getTime();
    const dayTwo = new Date(2026, 9, 21, 0, 5).getTime();
    const prepared = prepareMessages(
      [message("a", dayOne), message("b", dayTwo)],
      "alice",
      true,
    );
    expect(prepared[0].showDate).toBe(true);
    expect(prepared[1].showDate).toBe(true);
    expect(prepared[1].showTime).toBe(false);
  });

  it("does not replay entrance motion for messages restored from history", () => {
    const prepared = prepareMessages(
      [
        message("history", Date.now() - 1_000),
        message("live", Date.now() + 1_000),
      ],
      "alice",
    );
    expect(prepared.map((item) => item.isNew)).toEqual([false, true]);
  });
});

describe("runtimeMessageId", () => {
  it("keeps an optimistic row mounted when its server echo replaces the id", () => {
    const optimistic = message("optimistic_1", 1_000, {
      client_message_id: "optimistic_1",
    });
    const echo = message("server-1", 1_003, {
      client_message_id: "optimistic_1",
    });
    expect(runtimeMessageId(optimistic)).toBe("client:optimistic_1");
    expect(runtimeMessageId(echo)).toBe("client:optimistic_1");
  });
});
