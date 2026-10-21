import type { ChatPart, ChatSourcePart } from "@/lib/api";

/** Source links are external HTTP(S) URLs, never executable or relative URLs. */
export function safeSourceURL(value: unknown): string | null {
  if (typeof value !== "string" || !/^https?:\/\//i.test(value) || /[\s\\]/.test(value) ||
    Array.from(value).some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return null;
  try {
    const url = new URL(value);
    if (!url.hostname || url.username || url.password) return null;
    return url.href;
  } catch {
    return null;
  }
}

/** Rebuild the narrow public DTO. Raw tool fields must never ride on a source. */
export function messageSources(parts: readonly unknown[]): ChatSourcePart[] {
  const sources: ChatSourcePart[] = [];
  for (const value of parts) {
    if (!value || typeof value !== "object") continue;
    const part = value as Record<string, unknown>;
    if (part.type !== "source" || typeof part.source_id !== "string" || !part.source_id.trim() ||
      typeof part.title !== "string" || !["discovered", "fetched", "failed"].includes(String(part.status))) continue;
    const url = safeSourceURL(part.url);
    if (!url) continue;
    const source: ChatSourcePart = {
      type: "source", source_id: part.source_id, title: part.title.trim() || new URL(url).hostname,
      url, status: part.status as ChatSourcePart["status"], cited: part.status === "fetched" && part.cited === true,
    };
    const index = sources.findIndex((item) => item.source_id === source.source_id || item.url === source.url);
    if (index < 0) {
      sources.push(source);
    } else if (sources[index].url === source.url) {
      const previous = sources[index];
      const rank = { discovered: 0, failed: 1, fetched: 2 };
      sources[index] = {
        ...(rank[source.status] > rank[previous.status] ? source : previous),
        source_id: previous.source_id,
        cited: previous.cited || source.cited,
      };
    }
  }
  return sources;
}

export function sanitizeMessageParts(parts: ChatPart[], publicOnly = false): ChatPart[] {
  const content = parts.flatMap((part): ChatPart[] => {
    if (!part || typeof part !== "object") return [];
    if (part.type === "text") return [{ type: "text", text: typeof part.text === "string" ? part.text : "" }];
    if (!publicOnly && part.type === "reasoning") return [part];
    if (!publicOnly && part.type === "tool-call" && !["web_search", "web_fetch"].includes(part.tool_name ?? "")) return [part];
    return [];
  });
  // One tool loop emits a reasoning segment per round. Merge every segment
  // into the first one so a multi-round answer does not stack identical
  // "思考过程" headers around each tool card.
  const merged: ChatPart[] = [];
  let reasoningIndex = -1;
  for (const part of content) {
    if (part.type !== "reasoning") {
      merged.push(part);
      continue;
    }
    const text = part.text ?? "";
    if (reasoningIndex < 0) {
      reasoningIndex = merged.length;
      merged.push({ ...part, text });
    } else {
      const existing = merged[reasoningIndex];
      merged[reasoningIndex] = {
        ...existing,
        text: [existing.text, text].filter((value) => value && value.trim()).join("\n\n"),
      };
    }
  }
  return [...merged, ...messageSources(parts)];
}
