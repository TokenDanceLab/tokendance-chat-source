import { useMemo, useState, type ReactNode } from "react";
import { Brain, ChevronDown, ExternalLink, Globe, Wrench } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Spinner } from "@/components/ui/spinner";
import type { ChatPart } from "@/lib/api";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { cn } from "@/lib/utils";
import { messageSources, sanitizeMessageParts } from "@/lib/messageSources";
import { useStreamingText } from "@/hooks/useStreamingText";

/** Labels describe tool access, not whether a page's claims are true. */
export function SourceList({ parts }: { parts: ChatPart[] }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const sources = messageSources(parts);
  if (!sources.length) return null;
  const visible = expanded ? sources : sources.slice(0, 3);
  return (
    <section aria-label={t("message.sources")} data-visual="message-sources" className="mt-3 min-w-0 border-t border-border pt-3">
      <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Globe className="size-3.5 shrink-0" aria-hidden="true" />
        <span>{t("message.sources")} · {sources.length}</span>
      </div>
      <ul className="m-0 grid min-w-0 list-none gap-1 p-0">
        {visible.map((source) => (
          <li key={source.source_id} className="min-w-0">
            <a href={source.url} target="_blank" rel="noopener noreferrer"
              className="flex min-h-11 min-w-0 items-start gap-2 rounded-md px-2 py-2 text-xs transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={`${source.title} · ${t(`message.source${source.status === "fetched" ? "Fetched" : source.status === "failed" ? "Failed" : "Discovered"}`)}${source.cited ? ` · ${t("message.sourceCited")}` : ""} · ${t("message.sourceOpen")}`}>
              <div className="min-w-0 flex-1">
                <div className="font-medium leading-5 text-foreground [overflow-wrap:anywhere]">{source.title}</div>
                <div className="mt-0.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 leading-5 text-muted-foreground">
                  <span className="min-w-0 [overflow-wrap:anywhere]">{new URL(source.url).hostname}</span>
                  <span>{t(`message.source${source.status === "fetched" ? "Fetched" : source.status === "failed" ? "Failed" : "Discovered"}`)}</span>
                  {source.cited && <span>{t("message.sourceCited")}</span>}
                </div>
              </div>
              <ExternalLink className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            </a>
          </li>
        ))}
        {sources.length > 3 && (
          <li>
            <button
              type="button"
              onClick={() => setExpanded((value) => !value)}
              className="min-h-8 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {expanded
                ? t("message.sourcesShowLess")
                : t("message.sourcesShowAll", { count: sources.length - 3 })}
            </button>
          </li>
        )}
      </ul>
    </section>
  );
}

/** Collapsible reasoning block, collapsed again once the reply completes. */
export function ReasoningBlock({ text, streaming }: { text: string; streaming: boolean }) {
  const { t } = useTranslation();
  const showReasoning = useChatStore((state) => state.showReasoning);
  const [manual, setManual] = useState<boolean | null>(null);
  // Keep the trace folded while the answer streams: Muse-style calm surface,
  // with the spinner in the header signalling progress.
  const open = manual ?? false;
  if (!text.trim() || !showReasoning) return null;

  return (
    <Collapsible open={open} onOpenChange={setManual} className="w-full">
      <CollapsibleTrigger className="group flex w-full items-center gap-1.5 rounded-md py-1 text-left text-xs text-muted-foreground transition-colors hover:text-foreground">
        {streaming ? <Spinner className="size-3" /> : <Brain className="size-3.5" />}
        <span className="font-medium">{t("message.reasoning")}</span>
        <ChevronDown className="size-3.5 opacity-60 transition-transform group-data-[state=open]:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent className="data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:slide-out-to-top-1 data-[state=open]:slide-in-from-top-1">
        <div className="mt-1 border-l border-border pl-3 text-xs leading-6 text-muted-foreground whitespace-pre-wrap">
          {text}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function formatArguments(raw?: string): string {
  if (!raw) return "";
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

const TOOL_LABELS: Record<string, string> = {
  get_current_time: "message.toolCurrentTime",
  search_public_messages: "message.toolSearchPublic",
  search_private_messages: "message.toolSearchPrivate",
};

/** Tool call card: name, live status, arguments, and result on demand. */
export function ToolCallCard({ part }: { part: ChatPart }) {
  const { t } = useTranslation();
  const running = part.status === "running";
  const failed = part.status === "error";
  const [manual, setManual] = useState<boolean | null>(null);
  // Keep internal arguments and results folded like Muse's activity rail; the
  // header still shows the live spinner while the call runs.
  const open = manual ?? false;
  const hasBody = Boolean(part.arguments || part.result);

  return (
    <Collapsible
      open={open}
      onOpenChange={setManual}
      className="w-full rounded-lg border border-border bg-muted/20"
    >
      <CollapsibleTrigger
        disabled={!hasBody}
        className="group flex w-full items-center gap-2 px-2.5 py-2 text-left text-xs disabled:cursor-default"
      >
        <Wrench className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">
          {part.tool_name && TOOL_LABELS[part.tool_name]
            ? t(TOOL_LABELS[part.tool_name])
            : (part.tool_name ?? "tool")}
        </span>
        <span
          className={cn(
            "flex shrink-0 items-center gap-1 text-label",
            failed ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {running && <Spinner className="size-3" />}
          {running ? t("message.toolRunning") : failed ? t("message.toolFailed") : t("message.toolComplete")}
        </span>
        {hasBody && (
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" />
        )}
      </CollapsibleTrigger>
      <CollapsibleContent className="data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0">
        <div className="space-y-2 border-t border-border px-2.5 py-2">
          {part.arguments && (
            <div>
              <p className="mb-1 text-label text-muted-foreground">
                {t("message.toolArguments")}
              </p>
              <pre className="max-h-40 overflow-auto rounded-md bg-background/60 p-2 font-mono text-label leading-5 whitespace-pre-wrap break-words">
                {formatArguments(part.arguments)}
              </pre>
            </div>
          )}
          {part.result && (
            <div>
              <p className="mb-1 text-label text-muted-foreground">
                {t("message.toolResult")}
              </p>
              <pre className="max-h-48 overflow-auto rounded-md bg-background/60 p-2 font-mono text-label leading-5 whitespace-pre-wrap break-words">
                {formatArguments(part.result)}
              </pre>
            </div>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

/** Renders assistant-ui parts in order: reasoning, tool cards, and text. */
export function AssistantParts({
  parts,
  streaming,
  renderText,
  publicOnly = false,
}: {
  parts: ChatPart[];
  streaming: boolean;
  renderText: (text: string, streaming?: boolean) => ReactNode;
  publicOnly?: boolean;
}) {
  parts = sanitizeMessageParts(parts, publicOnly);
  const lastTextIndex = parts.reduce(
    (last, part, index) => (part.type === "text" ? index : last),
    -1,
  );

  return (
    <div className="flex w-full flex-col gap-1.5">
      {parts.map((part, index) => {
        if (part.type === "reasoning") {
          return (
            <ReasoningBlock
              key={`reasoning-${index}`}
              text={part.text ?? ""}
              streaming={streaming && index > lastTextIndex}
            />
          );
        }
        if (part.type === "tool-call") {
          return <ToolCallCard key={part.tool_call_id ?? `tool-${index}`} part={part} />;
        }
        if (part.type !== "text" || !part.text?.trim()) return null;
        return <TextPart
          key={`text-${index}`}
          text={part.text}
          streaming={streaming && index === lastTextIndex}
          renderText={renderText}
        />;
      })}
      {!streaming && <SourceList parts={parts} />}
    </div>
  );
}

/**
 * Parses Markdown only when the displayed prefix crosses a word boundary.
 * Keeping the rendered element memoized avoids paying that parse again for raw
 * WebSocket deltas that have not reached the display buffer yet.
 */

// The model may emit the burst-split marker while the draft is still
// streaming; the server removes it when the final burst is stored. Never let
// the raw marker line reach the reader mid-stream.
const SPLIT_MARKER_LINE = /^\s*<msg-break>\s*$/gm;
function stripSplitMarkers(text: string): string {
  return text.includes("<msg-break>") ? text.replace(SPLIT_MARKER_LINE, "") : text;
}

function TextPart({
  text,
  streaming,
  renderText,
}: {
  text: string;
  streaming: boolean;
  renderText: (text: string, streaming?: boolean) => ReactNode;
}) {
  const { displayedText, isFullyComplete } = useStreamingText(text, streaming);
  // Muse lets the final word fade before it swaps to the richer Markdown
  // surface. Waiting for that same beat prevents a late structural jump.
  const isEnhancing = isFullyComplete;
  const content = useMemo(
    () => renderText(stripSplitMarkers(displayedText), !isEnhancing),
    [displayedText, isEnhancing, renderText],
  );
  if (!displayedText) return null;
  return (
    <div
      className="markdown-body select-text"
      data-streaming={!isFullyComplete || undefined}
    >
      {content}
    </div>
  );
}
