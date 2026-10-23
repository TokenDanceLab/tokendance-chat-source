import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantTypingBubble, BotRunTimeline } from "@/components/BotRunTimeline";
import { I18nProvider } from "@/i18n/context";
import type { ChatMessage } from "@/lib/api";
import { useChatStore } from "@/stores/chatStore";

const ownRun = { type: "bot_run" as const, run_id: "own", message_id: "own-message", stream_id: "own-stream", trigger_message_id: "question", requested_by: "alice", private: false, status: "running" as const };
beforeEach(() => {
  useChatStore.getState().reset();
  useChatStore.getState().setUsername("alice");
  localStorage.setItem("tokendance:lang", "en");
  useChatStore.getState().receiveBotRun(ownRun);
});
afterEach(() => vi.useRealTimers());

function timeline(privateChat = false, messages: ChatMessage[] = []) {
  return <I18nProvider><BotRunTimeline privateChat={privateChat} messages={messages} /></I18nProvider>;
}

describe("Bot timeline waiting state", () => {
  it("keeps public running/queue visible without duplicated controls or private identity leaks", () => {
    useChatStore.getState().receiveBotRun({ ...ownRun, run_id: "peer", message_id: "peer-message", requested_by: "bob", status: "queued" });
    useChatStore.getState().receiveBotRun({ ...ownRun, run_id: "private", message_id: "private-message", private: true, to: "alice" });
    const { container, rerender } = render(timeline());
    expect(container.querySelector('[data-run-id="own"] [data-visual="assistant-typing-bubble"]')).toBeVisible();
    expect(screen.getByText("Queued · Reply to bob")).toBeVisible();
    expect(container.querySelector('[data-run-id="private"]')).toBeNull();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    rerender(timeline(true));
    expect(container.querySelector('[data-run-id="own"]')).toBeNull();
    expect(container.querySelector('[data-run-id="peer"]')).toBeNull();
    expect(container.querySelector('[data-run-id="private"] svg')).toBeNull();
    expect(screen.queryByText(/Your request|Reply to/)).not.toBeInTheDocument();
  });

  it.each(["run_id", "stream_id", "id", "message_id"] as const)("replaces only the matching run's waiting bubble on first text via %s", (field) => {
    const { container, rerender } = render(timeline());
    const draft: ChatMessage = { id: "other-message", username: "TokenBot", content: "Other request", timestamp: 1, streaming: true };
    rerender(timeline(false, [draft]));
    expect(container.querySelector('[data-run-id="own"]')).toBeVisible();
    const identity = field === "run_id" ? ownRun.run_id : field === "stream_id" ? ownRun.stream_id : ownRun.message_id;
    rerender(timeline(false, [{ ...draft, [field]: identity }]));
    expect(container.querySelector('[data-run-id="own"]')).toBeNull();
  });

  it("does not mistake a sources-only or whitespace draft for visible answer text", () => {
    const source: ChatMessage = { id: ownRun.message_id, username: "TokenBot", content: "", timestamp: 1, streaming: true,
      parts: [{ type: "source", source_id: "s1", title: "Source", url: "https://example.com/", status: "discovered", cited: false }, { type: "text", text: " " }] };
    const { container, rerender } = render(timeline(false, [source]));
    expect(container.querySelector('[data-run-id="own"]')).toBeVisible();
    rerender(timeline(false, [{ ...source, parts: [...source.parts!, { type: "text", text: "First answer token" }] }]));
    expect(container.querySelector('[data-run-id="own"]')).toBeNull();
  });

  it("cycles the active dot every 400ms and disposes its timer", () => {
    vi.useFakeTimers();
    const { container, unmount } = render(<I18nProvider><AssistantTypingBubble /></I18nProvider>);
    const dots = container.querySelectorAll('[data-active]');
    expect([...dots].map((dot) => dot.getAttribute("data-active"))).toEqual(["true", "false", "false"]);
    act(() => vi.advanceTimersByTime(400));
    expect([...dots].map((dot) => dot.getAttribute("data-active"))).toEqual(["false", "true", "false"]);
    expect(dots[1]).toHaveStyle({ animationDelay: "150ms" });
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("uses static equally visible dots with no bounce or timer under reduced motion", () => {
    vi.useFakeTimers();
    const original = window.matchMedia;
    window.matchMedia = vi.fn((query) => ({ ...original(query), matches: query === "(prefers-reduced-motion: reduce)" }));
    try {
      const { container, unmount } = render(<I18nProvider><AssistantTypingBubble /></I18nProvider>);
      expect(container.querySelectorAll('.animate-bounce, .animate-in')).toHaveLength(0);
      expect(container.querySelectorAll('[data-active="true"]')).toHaveLength(3);
      expect(vi.getTimerCount()).toBe(0);
      unmount();
    } finally { window.matchMedia = original; }
  });
});
