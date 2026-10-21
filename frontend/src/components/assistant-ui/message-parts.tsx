import { useState, type ReactNode } from "react";
import { Brain, ChevronDown, Wrench } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Spinner } from "@/components/ui/spinner";
import type { ChatPart } from "@/lib/api";
import { useTranslation } from "@/i18n/context";
import { cn } from "@/lib/utils";

/** Collapsible reasoning block, collapsed again once the reply completes. */
export function ReasoningBlock({ text, streaming }: { text: string; streaming: boolean }) {
  const { t } = useTranslation();
  const [manual, setManual] = useState<boolean | null>(null);
  const open = manual ?? streaming;
  if (!text.trim()) return null;

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
  const open = manual ?? running;
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
}: {
  parts: ChatPart[];
  streaming: boolean;
  renderText: (text: string) => ReactNode;
}) {
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
        if (!part.text?.trim()) return null;
        return (
          <div key={`text-${index}`} className="markdown-body select-text">
            {renderText(part.text)}
          </div>
        );
      })}
    </div>
  );
}
