import { beforeEach, describe, expect, it } from "vitest";
import type { ChatMessage, ChatSourcePart, WSBotRunEvent, WSStreamEvent } from "@/lib/api";
import { useChatStore } from "@/stores/chatStore";
import { ownActiveRun } from "@/stores/botRunState";

const store = () => useChatStore.getState();
const source: ChatSourcePart = { type: "source", source_id: "source-1", title: "Release notes", url: "https://example.com/release", status: "fetched", cited: true };
function run(overrides: Partial<WSBotRunEvent> = {}): WSBotRunEvent {
  return { type: "bot_run", run_id: "r1", message_id: "m1", stream_id: "r1", status: "running", requested_by: "alice", trigger_message_id: "trigger", room_id: "", private: false, ...overrides };
}
function chunk(overrides: Partial<WSStreamEvent> = {}): WSStreamEvent {
  return { type: "stream", run_id: "r1", message_id: "m1", stream_id: "r1", username: "TokenBot", content: "Hello", kind: "text", ...overrides };
}
function final(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return { id: "m1", message_id: "m1", run_id: "r1", stream_id: "r1", username: "TokenBot", content: "Complete", timestamp: 123, ...overrides };
}

beforeEach(() => { store().reset(); store().setUsername("alice"); store().setConnected(true); });

describe("Composer run ownership", () => {
  it("selects running before queued and scopes private/public independently", () => {
    for (const event of [
      run({ run_id: "queued", message_id: "q", status: "queued", timestamp: 1 }),
      run({ run_id: "running", message_id: "r", timestamp: 2 }),
      run({ run_id: "peer", message_id: "p", requested_by: "bob", timestamp: 0 }),
      run({ run_id: "private", message_id: "d", private: true, to: "alice" }),
    ]) store().receiveBotRun(event);
    expect(ownActiveRun(store().botRuns, "alice", false)?.run_id).toBe("running");
    expect(ownActiveRun(store().botRuns, "alice", true)?.run_id).toBe("private");
    expect(ownActiveRun(store().botRuns, "charlie", false)).toBeUndefined();
    store().receiveBotRun(run({ run_id: "running", message_id: "r", status: "cancelled" }));
    expect(ownActiveRun(store().botRuns, "alice", false)?.run_id).toBe("queued");
  });
});

describe("Private lane routing", () => {
  it("keeps the lane on streaming drafts and falls back to the run's lane", () => {
    store().receiveBotRun(run({ run_id: "r9", message_id: "m9", stream_id: "r9", private: true, to: "alice", thread_id: "lane-1" }));
    store().receiveBotStream(chunk({ run_id: "r9", message_id: "m9", stream_id: "r9", private: true, to: "alice", thread_id: "lane-1" }));
    expect(store().privateBotMessages.find((message) => message.id === "m9")?.thread_id).toBe("lane-1");

    store().receiveBotRun(run({ run_id: "r10", message_id: "m10", stream_id: "r10", private: true, to: "alice", thread_id: "lane-2" }));
    store().receiveBotStream(chunk({ run_id: "r10", message_id: "m10", stream_id: "r10", private: true, to: "alice" }));
    expect(store().privateBotMessages.find((message) => message.id === "m10")?.thread_id).toBe("lane-2");
  });
});

describe("Bot run delivery fences", () => {
  it.each(["cancelled", "failed"] as const)("%s settles drafts and rejects 50 late chunks, done, final and old snapshots", (status) => {
    store().addMessage({ id: "trigger", username: "alice", content: "explain the release", timestamp: 100 });
    store().receiveBotRun(run());
    store().receiveBotStream(chunk());
    expect(store().messages.find((message) => message.id === "m1")?.streaming).toBe(true);
    store().receiveBotRun(run({ status }));
    for (let i = 0; i < 50; i++) store().receiveBotStream(chunk());
    store().receiveBotStream(chunk({ kind: "done", parts: [source] }));
    store().addMessage(final({ parts: [source] }));
    store().setHistory([final({ parts: [source] })]);
    store().receiveBotRun(run({ status: "queued" }));
    store().receiveBotRun(run());
    if (status === "failed") {
      // A failed run leaves one calm inline notice, never a resurrected draft.
      expect(store().messages.map((message) => message.id)).toEqual(["trigger", "run-failed-r1"]);
      expect(store().messages[1]).toMatchObject({ username: "system" });
      expect(JSON.parse(store().messages[1].content).params.content).toBe("explain the release");
    } else {
      // A user-requested stop keeps the partial answer as a terminal reply.
      expect(store().messages.map((message) => message.id)).toEqual(["trigger", "m1"]);
      expect(store().messages[1]).toMatchObject({ streaming: false, stopped: true, content: "Hello" });
      expect(store().messages[1].parts).toEqual([{ type: "text", text: "Hello" }]);
    }
    expect(store().botRuns.r1.status).toBe(status);
  });

  it("does not replay a failure notice when a snapshot first reports the run as failed", () => {
    store().receiveBotRun(run({ status: "failed" }));
    expect(store().messages.filter((message) => message.id === "run-failed-r1")).toHaveLength(0);
  });

  it("adds a stopped notice with retry context when no partial answer arrived", () => {
    store().addMessage({ id: "trigger", username: "alice", content: "explain the release", timestamp: 100 });
    store().receiveBotRun(run());
    store().receiveBotRun(run({ status: "cancelled" }));
    const notice = store().messages.find((message) => message.id === "run-stopped-r1");
    expect(notice?.username).toBe("system");
    expect(JSON.parse(notice!.content)).toEqual({
      key: "system.botReplyStopped",
      params: { content: "explain the release" },
    });
  });

  it("does not replay a stopped notice for a snapshot-first cancellation", () => {
    store().receiveBotRun(run({ status: "cancelled" }));
    expect(store().messages.filter((message) => message.id === "run-stopped-r1")).toHaveLength(0);
  });

  it("adds an interim bot message without closing the active run", () => {
    store().receiveBotRun(run({ private: true, to: "alice" }));
    store().addPrivateBotMessage({
      id: "interim-1",
      username: "TokenBot",
      content: "收到",
      timestamp: 124,
      private: true,
      to: "alice",
    });
    expect(store().privateBotMessages.map((message) => message.id)).toEqual(["interim-1"]);
    expect(store().botRuns.r1.status).toBe("running");
  });

  it("keeps a single failure notice when the failed event is repeated", () => {
    store().receiveBotRun(run());
    store().receiveBotRun(run({ status: "failed" }));
    store().receiveBotRun(run({ status: "failed" }));
    expect(store().messages.filter((message) => message.id === "run-failed-r1")).toHaveLength(1);
  });

  it.each(["terminal-first", "final-first", "done-first"])("deduplicates %s completion without reviving the stream", (order) => {
    store().receiveBotRun(run()); store().receiveBotStream(chunk());
    if (order === "terminal-first") store().receiveBotRun(run({ status: "completed" }));
    if (order === "done-first") store().receiveBotStream(chunk({ kind: "done" }));
    const completed = final({ parts: [{ type: "text", text: "Complete" }, source, source] });
    store().addMessage(completed);
    store().receiveBotRun(run({ status: "completed" }));
    store().addMessage(completed); store().setHistory([completed]);
    store().receiveBotStream(chunk()); store().receiveBotRun(run({ status: "queued" }));
    expect(store().messages).toHaveLength(1);
    expect(store().messages[0]).toMatchObject({ id: "m1", content: "Complete", streaming: false });
    expect(store().messages[0].parts).toEqual([{ type: "text", text: "Complete" }, source]);
    expect(store().botRuns.r1.status).toBe("completed");
  });

  it("normalizes omitted public flags from the Go omitempty wire format", () => {
    store().receiveBotRun(run({ private: undefined, room_id: undefined }));
    expect(store().botRuns.r1.private).toBe(false);
    store().receiveBotStream(chunk());
    expect(store().messages).toHaveLength(1);
    store().receiveBotRun(run({ private: undefined, status: "cancelled" }));
    expect(store().messages).toHaveLength(1);
    expect(store().messages[0]).toMatchObject({ id: "m1", stopped: true, streaming: false });
  });

  it("keeps two users' runs distinct and makes progression monotonic", () => {
    store().receiveBotRun(run());
    store().receiveBotRun(run({ run_id: "r2", message_id: "m2", stream_id: "r2", requested_by: "bob", status: "queued" }));
    store().receiveBotRun(run({ status: "queued" }));
    store().setRunCancelPending("r2", true);
    store().setRunCancelPending("r1", true);
    expect(store().botRuns.r1).toMatchObject({ status: "running", cancelPending: true });
    expect(store().botRuns.r2.cancelPending).toBeFalsy();
    store().receiveBotRun(run({ status: "cancelled" }));
    expect(store().botRuns.r2.status).toBe("queued");
  });

  it("isolates private run metadata, drafts and final messages by owner", () => {
    store().receiveBotRun(run({ private: true, requested_by: "bob", to: "bob" }));
    store().receiveBotStream(chunk({ private: true, to: "bob" }));
    store().addPrivateBotMessage(final({ private: true, to: "bob" }));
    expect(store().botRuns).toEqual({});
    expect(store().privateBotMessages).toEqual([]);
    store().receiveBotRun(run({ private: true, to: "alice" }));
    store().receiveBotStream(chunk({ private: true, to: "alice", kind: "reasoning", content: "Private thought" }));
    expect(store().messages).toEqual([]);
    expect(store().privateBotMessages[0].parts?.[0].type).toBe("reasoning");
    store().receiveBotStream(chunk()); // Wrong conversation for a known run.
    store().addMessage(final());
    expect(store().messages).toEqual([]);
    store().receiveBotRun(run({ private: true, to: "alice", status: "cancelled" }));
    store().receiveBotStream(chunk({ private: true, to: "alice" }));
    expect(store().privateBotMessages).toHaveLength(1);
    expect(store().privateBotMessages[0]).toMatchObject({ stopped: true, streaming: false });
  });

  it("projects public live and historical messages to text only", () => {
    store().receiveBotRun(run());
    store().receiveBotStream(chunk({ kind: "reasoning", content: "Secret" }));
    store().receiveBotStream(chunk({ kind: "tool_call", tool_call: { id: "tool", name: "search", arguments: "Secret" } }));
    expect(store().messages).toEqual([]);
    const message = final({ parts: [{ type: "reasoning", text: "Secret" }, { type: "tool-call", result: "Secret" }, { type: "text", text: "Complete" }] });
    store().addMessage(message);
    store().setHistory([{ ...message, id: "old", run_id: undefined }]);
    expect(store().messages.every((item) => item.parts?.every((part) => part.type === "text"))).toBe(true);
  });

  it("clears disconnect drafts, restores only snapshot runs, and keeps cancellation fences", () => {
    store().receiveBotRun(run()); store().receiveBotStream(chunk());
    store().receiveBotRun(run({ run_id: "cancelled", message_id: "gone", status: "cancelled" }));
    store().setConnected(false);
    expect(store().messages).toEqual([]);
    expect(store().botRuns.r1).toBeUndefined();
    store().setConnected(true);
    store().receiveBotRun(run());
    store().receiveBotRun(run({ run_id: "cancelled", message_id: "gone", status: "queued" }));
    store().receiveBotStream(chunk());
    store().setHistory([final()]); // Persisted history replaces the identical draft ID.
    store().receiveBotStream(chunk());
    expect(store().messages).toHaveLength(1);
    expect(store().messages[0].content).toBe("Complete");
    expect(store().botRuns.cancelled.status).toBe("cancelled");
  });

  it("clears private state and all run metadata on account change or logout", () => {
    store().receiveBotRun(run({ private: true, to: "alice" }));
    store().receiveBotStream(chunk({ private: true, to: "alice" }));
    store().setUsername("bob");
    expect(store().privateBotMessages).toEqual([]);
    expect(store().botRuns).toEqual({});
    store().addPrivateBotMessage(final({ run_id: undefined }));
    store().reset();
    expect(store().privateBotMessages).toEqual([]);
  });

  it("restores safe sources from refresh history and keeps private sources isolated", () => {
    store().setHistory([final({ parts: [source, source, { type: "reasoning", text: "private" }] })]);
    expect(store().messages[0].parts).toEqual([source]);
    store().reset(); store().setUsername("alice");
    store().setHistory([final({ parts: [source, source] })]);
    store().setPrivateBotHistory([
      final({ id: "private", run_id: "private-run", private: true, to: "alice", parts: [{ ...source, title: "Private source" }] }),
      final({ id: "other", run_id: "other-run", private: true, to: "bob", parts: [{ ...source, title: "Other user's source" }] }),
    ]);
    expect(store().messages[0].parts).toEqual([source]);
    expect(store().privateBotMessages).toHaveLength(1);
    expect(store().privateBotMessages[0].parts?.[0].title).toBe("Private source");
    store().setUsername("bob");
    expect(store().privateBotMessages).toEqual([]);
  });

  it("a cancelled private run cannot restore sources through late final or history", () => {
    store().receiveBotRun(run({ private: true, to: "alice" }));
    store().receiveBotStream(chunk({ private: true, to: "alice" }));
    store().receiveBotRun(run({ private: true, to: "alice", status: "cancelled" }));
    const late = final({ private: true, to: "alice", parts: [source] });
    store().receiveBotStream(chunk({ private: true, to: "alice", kind: "done", parts: [source] }));
    store().addPrivateBotMessage(late);
    store().setPrivateBotHistory([late]);
    expect(store().privateBotMessages).toHaveLength(1);
    expect(store().privateBotMessages[0]).toMatchObject({ stopped: true, content: "Hello" });
    expect(store().privateBotMessages[0].parts).toEqual([{ type: "text", text: "Hello" }]);
    expect(store().messages).toEqual([]);
  });

  it("supports older servers with no run metadata and final before done", () => {
    store().receiveBotStream({ type: "stream", username: "TokenBot", stream_id: "old", content: "Draft" });
    store().addMessage(final({ id: "legacy", message_id: undefined, run_id: undefined, stream_id: "old" }));
    store().receiveBotStream({ type: "stream", username: "TokenBot", stream_id: "old", kind: "done" });
    expect(store().messages).toHaveLength(1);
    expect(store().messages[0].id).toBe("legacy");
  });
});

describe("Burst final delivery", () => {
  it("renders every segment of a burst final (done first, then one message per segment)", () => {
    // Mirrors the production sequence: run event, stream chunks, done carrying
    // the first segment, then one message event per burst segment.
    store().receiveBotRun(run({ private: true, to: "alice" }));
    store().receiveBotStream(chunk({ private: true, to: "alice", content: "第一段" }));
    store().receiveBotStream(chunk({ private: true, to: "alice", kind: "done", parts: [{ type: "text", text: "第一段" }] }));
    store().addPrivateBotMessage(final({ private: true, to: "alice", content: "第一段", parts: [{ type: "text", text: "第一段" }] }));
    store().addPrivateBotMessage(final({ id: "m2", message_id: "m2", stream_id: "", content: "第二段", private: true, to: "alice", parts: [{ type: "text", text: "第二段" }] }));
    store().addPrivateBotMessage(final({ id: "m3", message_id: "m3", stream_id: "", content: "第三段", private: true, to: "alice", parts: [{ type: "text", text: "第三段" }] }));
    const texts = store().privateBotMessages.map((m) => `${message_id_safe(m)}:${m.content}`);
    function message_id_safe(m: { id: string }) { return m.id; }
    expect(texts).toEqual(["m1:第一段", "m2:第二段", "m3:第三段"]);
    expect(store().privateBotMessages.every((m) => !m.streaming)).toBe(true);
  });
});
