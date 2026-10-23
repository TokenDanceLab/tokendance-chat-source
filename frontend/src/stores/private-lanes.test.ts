import { describe, it, expect } from "vitest";
import { act } from "@testing-library/react";
import { useChatStore, privateLaneOf } from "@/stores/chatStore";

// Minimal driver: seed private messages, render nothing, and verify the lane
// helper plus store history replacement directly.
describe("private lanes", () => {
  it("thread replies inherit the parent lane and lane replace keeps other lanes", () => {
    useChatStore.getState().reset();
    useChatStore.getState().setUsername("testuser");
    const parent = { id: "private-parent", username: "TokenBot", content: "p", timestamp: 1, to: "testuser" };
    const reply = { id: "private-parent-reply", username: "testuser", content: "r", timestamp: 2, thread_id: "private-parent", to: "TokenBot" };
    const byId = new Map([parent, reply].map((m) => [m.id, m] as const));
    expect(privateLaneOf(parent as never, byId as never)).toBe("");
    expect(privateLaneOf(reply as never, byId as never)).toBe("");
    useChatStore.setState({ privateBotMessages: [parent, reply] as never });
    const laneMsg = { id: "x", username: "testuser", content: "lane", timestamp: 3, to: "TokenBot", thread_id: "lane-1" };
    useChatStore.setState({ privateBotMessages: [parent, reply, laneMsg] as never });
    const all = useChatStore.getState().privateBotMessages;
    const byId2 = new Map(all.map((m) => [m.id, m] as const));
    expect(privateLaneOf(laneMsg as never, byId2 as never)).toBe("lane-1");
    // replace lane "" keeps the lane-1 message, keeps nothing stale
    act(() => useChatStore.getState().setPrivateBotHistory([parent, reply] as never, ""));
    const after = useChatStore.getState().privateBotMessages.map((m) => m.id);
    expect(after).toEqual(["private-parent", "private-parent-reply", "x"]);
  });

  it("a stale empty snapshot never swallows a just-echoed send (new-lane race)", () => {
    useChatStore.getState().reset();
    useChatStore.getState().setUsername("testuser");
    // The lane was just created: the load_private answer predates the send,
    // so its snapshot is empty while the echo is already in the store.
    const echo = { id: "echo-1", username: "testuser", content: "刚发的旁聊", timestamp: Date.now(), to: "TokenBot", thread_id: "lane-9" };
    useChatStore.setState({ privateBotMessages: [echo] as never });
    act(() => useChatStore.getState().setPrivateBotHistory([], "lane-9"));
    expect(useChatStore.getState().privateBotMessages.map((m) => m.id)).toEqual(["echo-1"]);
  });
});
