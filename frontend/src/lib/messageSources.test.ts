import { describe, expect, it } from "vitest";
import { messageSources, safeSourceURL, sanitizeMessageParts } from "@/lib/messageSources";
import type { ChatPart } from "@/lib/api";

const source: ChatPart = { type: "source", source_id: "1", title: "Release notes", url: "https://example.com/release", status: "fetched", cited: true };

describe("server-registered message sources", () => {
  it.each(["javascript:alert(1)", "data:text/html,hi", "file:///etc/passwd", "//example.com", "/relative", "https://user:pass@example.com", "https://example.com\\@other.test", "https://exam\nple.com", "https://example.com/with space", "https://"])("rejects unsafe or ambiguous URL %s", (url) => { // # leak-guard-allow:LG-F4C6A155 synthetic credentials rejected by this test
    expect(safeSourceURL(url)).toBeNull();
    expect(messageSources([{ ...source, url }])).toEqual([]);
  });

  it("projects only safe DTO fields and never promotes a discovery to a citation", () => {
    const result = messageSources([{ ...source, status: "discovered", query: "private query", arguments: "raw arguments", result: "full page", text: "injected content" }]);
    expect(result).toEqual([{ ...source, status: "discovered", cited: false }]);
    expect(messageSources([{ ...source, status: "failed" }])[0].cited).toBe(false);
    expect(messageSources([null, {}, { ...source, status: "verified" }, { ...source, source_id: "" }])).toEqual([]);
  });

  it("deduplicates source IDs and canonical URLs while retaining read status", () => {
    expect(messageSources([
      { ...source, status: "discovered", cited: false }, source, { ...source, source_id: "2" },
      { ...source, url: "https://spoofed.test", title: "Conflicting ID" },
    ])).toEqual([source]);
  });

  it("excludes raw web calls in both conversations and all other tool parts in public", () => {
    const parts: ChatPart[] = [
      { type: "text", text: "Answer", result: "must not leak" },
      { type: "reasoning", text: "Private thought" },
      { type: "tool-call", tool_name: "web_search", arguments: "secret query" },
      { type: "tool-call", tool_name: "web_fetch", result: "full page" },
      { type: "tool-call", tool_name: "get_current_time" }, { type: "tool-call", tool_name: "send_message", arguments: "working on it" }, source,
    ];
    expect(sanitizeMessageParts(parts, true)).toEqual([{ type: "text", text: "Answer" }, source]);
    expect(sanitizeMessageParts(parts).map((part) => part.type)).toEqual(["text", "reasoning", "tool-call", "source"]);
    expect(
      sanitizeMessageParts(parts).some(
        (part) => part.type === "tool-call" && part.tool_name === "send_message",
      ),
    ).toBe(false);
    expect(messageSources([{ type: "text", text: "[Link](https://example.com)" }])).toEqual([]);
  });

  it("merges reasoning across tool rounds into one block", () => {
    const parts: ChatPart[] = [
      { type: "reasoning", text: "first pass" },
      { type: "tool-call", tool_name: "get_current_time" },
      { type: "reasoning", text: "second pass" },
      { type: "text", text: "Answer" },
    ];
    expect(sanitizeMessageParts(parts)).toEqual([
      { type: "reasoning", text: "first pass\n\nsecond pass" },
      { type: "tool-call", tool_name: "get_current_time" },
      { type: "text", text: "Answer" },
    ]);
  });
});
