import type { BotRunStatus, ChatMessage, ChatPart, WSBotRunEvent, WSStreamEvent } from "@/lib/api";
import { applyStreamEvent, partsToText } from "@/lib/streamParts";
import { sanitizeMessageParts } from "@/lib/messageSources";

export interface BotRun extends WSBotRunEvent {
  private: boolean;
  cancelPending?: boolean;
}

export interface BotRunState {
  username: string;
  messages: ChatMessage[];
  privateBotMessages: ChatMessage[];
  botRuns: Record<string, BotRun>;
  // Terminal fences last for this signed-in session, including reconnects.
  closedBotRuns: Record<string, BotRunStatus>;
}

export const isActiveRun = (status: BotRunStatus) => status === "queued" || status === "running";
export const isStreamingMessage = (message: ChatMessage) => message.streaming ?? message.id.startsWith("stream-");

/** Composer stop targets the viewer's running request before their queue. */
export function ownActiveRun(runs: Record<string, BotRun>, username: string, privateChat: boolean): BotRun | undefined {
  return Object.values(runs)
    .filter((run) => isActiveRun(run.status) && run.requested_by === username && run.private === privateChat)
    .sort((a, b) => Number(b.status === "running") - Number(a.status === "running") ||
      (a.timestamp ?? 0) - (b.timestamp ?? 0) || a.run_id.localeCompare(b.run_id))[0];
}

/** Public messages expose only text and safe server-registered sources. */
export function publicMessage(message: ChatMessage): ChatMessage {
  return message.parts ? { ...message, parts: sanitizeMessageParts(message.parts, true) } : message;
}

export function privateMessage(message: ChatMessage): ChatMessage {
  return message.parts ? { ...message, parts: sanitizeMessageParts(message.parts) } : message;
}

function isDraftOf(message: ChatMessage, runId: string): boolean {
  return isStreamingMessage(message) &&
    (message.run_id === runId || message.stream_id === runId || message.id === `stream-${runId}`);
}

function clearDrafts(state: BotRunState, runId: string): Pick<BotRunState, "messages" | "privateBotMessages"> {
  const keep = (message: ChatMessage) => !isDraftOf(message, runId);
  return { messages: state.messages.filter(keep), privateBotMessages: state.privateBotMessages.filter(keep) };
}

/**
 * A user-requested stop keeps whatever the model already produced as a
 * terminal, visibly-stopped answer so the effort is not thrown away. Late
 * stream events and finals stay fenced by `closedBotRuns`.
 */
function terminalizeDrafts(list: ChatMessage[], runId: string): ChatMessage[] {
  return list.map((message) =>
    isDraftOf(message, runId) ? { ...message, streaming: false, stopped: true } : message,
  );
}

/**
 * A failed run used to just make its draft vanish. Leave one calm inline
 * notice so the request never ends in silence. Operates on the already-cleared
 * transcript so the notice cannot resurrect the rejected draft.
 */
function appendFailureNotice(list: ChatMessage[], runId: string, triggerContent?: string): ChatMessage[] {
  const id = `run-failed-${runId}`;
  if (list.some((message) => message.id === id)) return list;
  const notice: ChatMessage = {
    id,
    username: "system",
    content: JSON.stringify({
      key: "system.botReplyFailed",
      ...(triggerContent ? { params: { content: triggerContent } } : {}),
    }),
    timestamp: Date.now(),
    dedupeKey: id,
  };
  return [...list, notice].slice(-500);
}

function appendStoppedNotice(list: ChatMessage[], runId: string, triggerContent?: string): ChatMessage[] {
  const id = `run-stopped-${runId}`;
  if (list.some((message) => message.id === id)) return list;
  const notice: ChatMessage = {
    id,
    username: "system",
    content: JSON.stringify({
      key: "system.botReplyStopped",
      ...(triggerContent ? { params: { content: triggerContent } } : {}),
    }),
    timestamp: Date.now(),
    dedupeKey: id,
  };
  return [...list, notice].slice(-500);
}

export function receiveRun(state: BotRunState, event: WSBotRunEvent): Partial<BotRunState> {
  if (!event.run_id || !event.message_id || !["queued", "running", "completed", "failed", "cancelled"].includes(event.status)) return {};
  if (event.private && (event.requested_by !== state.username || (event.to && event.to !== state.username))) return {};
  const privateRun = event.private === true;
  const previous = state.botRuns[event.run_id];
  const closed = state.closedBotRuns[event.run_id];
  if (closed && (isActiveRun(event.status) || event.status !== closed)) return {};
  if (previous?.status === "running" && event.status === "queued") return {};
  // Run identity and conversation cannot change between events.
  if (previous && (previous.private !== privateRun || previous.requested_by !== event.requested_by || previous.message_id !== event.message_id)) return {};
  const active = isActiveRun(event.status);
  const listKey = privateRun ? "privateBotMessages" : "messages";
  const sourceList = state[listKey];
  const trigger = sourceList.find(
    (message) => message.id === event.trigger_message_id || message.message_id === event.trigger_message_id,
  );
  const cleared = clearDrafts(state, event.run_id);
  if (active) {
    return {
      botRuns: { ...state.botRuns, [event.run_id]: { ...event, private: privateRun, cancelPending: previous?.cancelPending } },
    };
  }
  const terminalList = event.status === "cancelled"
    ? terminalizeDrafts(sourceList, event.run_id)
    : cleared[listKey];
  // Only a live transition earns a transcript notice. A run replayed by a
  // reconnect snapshot must not re-append one.
  let notices = terminalList;
  if (previous && event.status === "failed") {
    notices = appendFailureNotice(terminalList, event.run_id, trigger?.content);
  } else if (previous && event.status === "cancelled" && !terminalList.some((message) => message.stopped)) {
    notices = appendStoppedNotice(terminalList, event.run_id, trigger?.content);
  }
  return {
    botRuns: { ...state.botRuns, [event.run_id]: { ...event, private: privateRun, cancelPending: false } },
    ...cleared,
    [listKey]: notices,
    closedBotRuns: { ...state.closedBotRuns, [event.run_id]: event.status },
  };
}

export function receiveStream(state: BotRunState, event: WSStreamEvent): Partial<BotRunState> {
  if (!event.username || event.username === state.username) return {};
  const privateRun = event.private === true;
  if (privateRun && event.to && event.to !== state.username) return {};
  const runId = event.run_id || event.stream_id;
  const run = runId ? state.botRuns[runId] : undefined;
  if (run && (run.private !== privateRun || (run.private && run.requested_by !== state.username))) return {};
  if (runId && state.closedBotRuns[runId]) return {};
  const id = event.message_id || run?.message_id || `stream-${event.stream_id || event.username}`;
  const key = privateRun ? "privateBotMessages" : "messages";
  const messages = state[key];
  const previous = messages.find((message) => message.id === id);
  if (previous && !isStreamingMessage(previous)) return {};
  const finished = event.kind === "done" || event.kind === "error" || event.done === true;
  if (finished) {
    return {
      [key]: messages.filter((message) => message.id !== id || !isStreamingMessage(message)),
      ...(runId ? { closedBotRuns: { ...state.closedBotRuns, [runId]: event.kind === "error" ? "failed" : "completed" } } : {}),
      ...(run ? { botRuns: { ...state.botRuns, [run.run_id]: { ...run, status: event.kind === "error" ? "failed" : "completed", cancelPending: false } } } : {}),
    };
  }
  if (!privateRun && event.kind && !["text"].includes(event.kind)) return {};
  const parts: ChatPart[] = applyStreamEvent(previous?.parts ?? [], event);
  if (!parts.length) return {};
  const replyToId = event.reply_to_id || run?.trigger_message_id;
  const trigger = messages.find((message) => message.id === replyToId);
  const draft: ChatMessage = {
    id, run_id: event.run_id, stream_id: event.stream_id, message_id: event.message_id,
    username: event.username, private: privateRun, to: event.to, room_id: event.room_id,
    thread_id: event.thread_id ?? run?.thread_id ?? "",
    reply_to_id: replyToId, reply_to_content: trigger?.content, reply_to_user: trigger?.username,
    content: partsToText(parts), parts, streaming: true, timestamp: previous?.timestamp ?? Date.now(),
  };
  return { [key]: previous ? messages.map((message) => message.id === id ? draft : message) : [...messages, draft].slice(-500) };
}

/** Final events may follow completed snapshots, but never cancellation/failure. */
export function acceptFinal(state: BotRunState, message: ChatMessage): boolean {
  if (message.private && message.to && message.username !== state.username && message.to !== state.username) return false;
  const run = message.run_id ? state.botRuns[message.run_id] : undefined;
  if (run && run.private !== (message.private === true)) return false;
  const status = message.run_id ? state.closedBotRuns[message.run_id] : undefined;
  return status !== "cancelled" && status !== "failed";
}

export function finishMessage(state: BotRunState, message: ChatMessage): Partial<BotRunState> {
  const runId = message.run_id || message.stream_id;
  if (!runId) return {};
  const run = state.botRuns[runId];
  return {
    ...clearDrafts(state, runId),
    closedBotRuns: { ...state.closedBotRuns, [runId]: "completed" },
    ...(run ? { botRuns: { ...state.botRuns, [runId]: { ...run, status: "completed", cancelPending: false } } } : {}),
  };
}

export function clearActiveRuns(state: BotRunState): Partial<BotRunState> {
  return {
    botRuns: Object.fromEntries(Object.entries(state.botRuns).filter(([, run]) => !isActiveRun(run.status))),
    messages: state.messages.filter((message) => !isStreamingMessage(message)),
    privateBotMessages: state.privateBotMessages.filter((message) => !isStreamingMessage(message)),
  };
}
