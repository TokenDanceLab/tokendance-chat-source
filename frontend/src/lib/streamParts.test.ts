import { describe, expect, it } from "vitest";
import { applyStreamEvent, partsToText } from "@/lib/streamParts";
import type { ChatPart, WSStreamEvent } from "@/lib/api";

function event(patch: Partial<WSStreamEvent>): WSStreamEvent {
  return { type: "stream", username: "TokenBot", ...patch };
}

describe("applyStreamEvent", () => {
  it("accumulates consecutive text deltas into one part", () => {
    let parts: ChatPart[] = [];
    parts = applyStreamEvent(parts, event({ kind: "text", content: "Hello" }));
    parts = applyStreamEvent(parts, event({ kind: "text", content: " world" }));

    expect(parts).toEqual([{ type: "text", text: "Hello world" }]);
    expect(partsToText(parts)).toBe("Hello world");
  });

  it("keeps reasoning out of the message text", () => {
    let parts: ChatPart[] = [];
    parts = applyStreamEvent(parts, event({ kind: "reasoning", content: "think" }));
    parts = applyStreamEvent(parts, event({ kind: "text", content: "answer" }));

    expect(parts.map((part) => part.type)).toEqual(["reasoning", "text"]);
    expect(partsToText(parts)).toBe("answer");
  });

  it("upserts a tool card by call id", () => {
    let parts: ChatPart[] = [];
    parts = applyStreamEvent(
      parts,
      event({
        kind: "tool_call",
        tool_call: { id: "call-1", name: "get_current_time", arguments: "{}", status: "running" },
      }),
    );
    parts = applyStreamEvent(
      parts,
      event({
        kind: "tool_result",
        tool_call: { id: "call-1", name: "get_current_time", arguments: "{}", result: "{}", status: "complete" },
      }),
    );

    expect(parts).toHaveLength(1);
    expect(parts[0]).toMatchObject({ type: "tool-call", status: "complete", result: "{}" });
  });

  it("uses the authoritative parts on done", () => {
    const final: ChatPart[] = [{ type: "text", text: "final" }];
    const parts = applyStreamEvent([{ type: "text", text: "draft" }], event({ kind: "done", parts: final }));
    expect(parts).toEqual(final);
  });

  it("replaces the draft with the error text", () => {
    const parts = applyStreamEvent(
      [{ type: "text", text: "draft" }],
      event({ kind: "error", content: "failed" }),
    );
    expect(parts).toEqual([{ type: "text", text: "failed" }]);
  });
});
