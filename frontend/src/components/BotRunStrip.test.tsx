import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BotRunStrip } from "@/components/BotRunStrip";
import { I18nProvider } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";

const mocks = vi.hoisted(() => ({ cancel: vi.fn(() => true), enabled: true }));
vi.mock("@/lib/api", () => ({ chatAPI: { cancelBotRun: mocks.cancel } }));
vi.mock("@/hooks/useServerConfig", () => ({ useServerConfig: () => ({ bot_name: "TokenBot", capabilities: { run_control: mocks.enabled } }) }));

const ownRun = { type: "bot_run" as const, run_id: "own", message_id: "own-message", stream_id: "own", trigger_message_id: "question", requested_by: "alice", room_id: "", private: false, status: "running" as const };
beforeEach(() => {
  mocks.enabled = true;
  mocks.cancel.mockClear();
  useChatStore.getState().reset();
  useChatStore.getState().setUsername("alice");
  localStorage.setItem("tokendance:lang", "en");
  useChatStore.getState().addMessage({ id: "question", username: "alice", content: "Explain this change", timestamp: 1 });
  useChatStore.getState().receiveBotRun(ownRun);
  useChatStore.getState().receiveBotRun({ ...ownRun, run_id: "other", message_id: "other-message", requested_by: "bob", status: "queued" });
});

describe("Bot run controls", () => {
  it("shows both public users but only the owner's accessible stop button", () => {
    render(<I18nProvider><BotRunStrip privateChat={false} connected /></I18nProvider>);
    expect(screen.getByText(/Reply to bob/)).toHaveTextContent("Queued");
    expect(screen.getByText(/Your request/)).toHaveTextContent("Replying");
    const button = screen.getByRole("button", { name: "Stop reply: Explain this change" });
    expect(screen.getAllByRole("button")).toHaveLength(1);
    button.focus(); expect(button).toHaveFocus();
    fireEvent.click(button);
    expect(mocks.cancel).toHaveBeenCalledExactlyOnceWith("own");
    expect(button).toBeDisabled();
    expect(screen.getByText(/Stopping/)).toBeVisible();
    act(() => useChatStore.getState().receiveBotRun({ ...ownRun, status: "cancelled" }));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(/Reply to bob/)).toBeVisible();
  });

  it("switches private/public views without exposing either conversation's runs", () => {
    useChatStore.getState().receiveBotRun({ ...ownRun, private: true, to: "alice", run_id: "private", message_id: "private-message", status: "queued" });
    const { rerender } = render(<I18nProvider><BotRunStrip privateChat={false} connected /></I18nProvider>);
    expect(screen.getAllByRole("button")).toHaveLength(1);
    rerender(<I18nProvider><BotRunStrip privateChat connected /></I18nProvider>);
    expect(screen.queryByText(/Reply to bob/)).not.toBeInTheDocument();
    expect(screen.getByText(/Your request/)).toHaveTextContent("Queued");
    fireEvent.click(screen.getByRole("button"));
    expect(mocks.cancel).toHaveBeenCalledWith("private");
  });

  it("gates cancellation on server capability and connection", () => {
    mocks.enabled = false;
    const { rerender } = render(<I18nProvider><BotRunStrip privateChat={false} connected /></I18nProvider>);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    mocks.enabled = true;
    rerender(<I18nProvider><BotRunStrip privateChat={false} connected={false} /></I18nProvider>);
    expect(screen.getByRole("button")).toBeDisabled();
    expect(mocks.cancel).not.toHaveBeenCalled();
  });
});
