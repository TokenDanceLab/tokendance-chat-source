import type { ChatPart, WSStreamEvent } from "@/lib/api";

/**
 * Folds one stream event into the assistant-ui parts of the in-flight reply.
 * `done` carries the authoritative final parts; `error` replaces the draft
 * with the error text so the user still sees what happened.
 */
export function applyStreamEvent(parts: ChatPart[], event: WSStreamEvent): ChatPart[] {
  const kind = event.kind ?? "text";

  if (kind === "done") {
    return event.parts && event.parts.length > 0 ? event.parts : parts;
  }
  if (kind === "error") {
    return [{ type: "text", text: event.content ?? "" }];
  }
  if (kind === "reasoning" || kind === "text") {
    if (!event.content) return parts;
    return appendDelta(parts, kind, event.content);
  }
  if (kind === "tool_call" || kind === "tool_result") {
    if (!event.tool_call) return parts;
    return upsertToolCall(parts, event.tool_call);
  }
  return parts;
}

/** Concatenates text parts; reasoning and tool output stay out of `content`. */
export function partsToText(parts: ChatPart[]): string {
  return parts
    .filter((part) => part.type === "text")
    .map((part) => part.text ?? "")
    .join("");
}

function appendDelta(parts: ChatPart[], type: "text" | "reasoning", delta: string): ChatPart[] {
  const last = parts[parts.length - 1];
  if (last?.type === type) {
    return [...parts.slice(0, -1), { ...last, text: (last.text ?? "") + delta }];
  }
  return [...parts, { type, text: delta }];
}

function upsertToolCall(
  parts: ChatPart[],
  call: NonNullable<WSStreamEvent["tool_call"]>,
): ChatPart[] {
  const next: ChatPart = {
    type: "tool-call",
    tool_call_id: call.id,
    tool_name: call.name,
    arguments: call.arguments ?? "",
    result: call.result,
    status: call.status ?? "running",
  };
  const index = parts.findIndex(
    (part) => part.type === "tool-call" && part.tool_call_id === call.id,
  );
  if (index === -1) return [...parts, next];
  return parts.map((part, i) => (i === index ? next : part));
}
