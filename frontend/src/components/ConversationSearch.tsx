import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { Search, ChevronUp, ChevronDown, X } from "lucide-react";
import { useTranslation } from "@/i18n/context";
import { CHAT_EVENTS } from "@/lib/events";
import { cn, formatTime } from "@/lib/utils";
import type { ChatMessage } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";

interface ConversationSearchProps {
  open: boolean;
  messages: ChatMessage[];
  onClose: () => void;
  onHighlightChange: (term: string) => void;
}

const MAX_RESULTS = 20;

/** Render a content snippet with the first match of query highlighted. */
function renderSnippet(content: string, query: string): React.ReactNode {
  const lower = content.toLowerCase();
  const q = query.toLowerCase();
  const idx = lower.indexOf(q);
  if (idx === -1) return content.slice(0, 120);
  const start = Math.max(0, idx - 40);
  const end = Math.min(content.length, idx + query.length + 40);
  const prefix = start > 0 ? "..." : "";
  const suffix = end < content.length ? "..." : "";
  return (
    <>
      {prefix}
      {content.slice(start, idx)}
      <mark className="rounded-sm bg-warning/35 px-0.5 text-foreground">
        {content.slice(idx, idx + query.length)}
      </mark>
      {content.slice(idx + query.length, end)}
      {suffix}
    </>
  );
}

export function ConversationSearch({ open, messages, onClose, onHighlightChange }: ConversationSearchProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);

  const conversationMessages = messages;

  // Client-side filtering. Newest hits win when a query matches more than the
// display cap.
  const results = useMemo(() => {
    if (!query.trim()) return [];
    const q = query.toLowerCase();
    const matched: ChatMessage[] = [];
    for (let i = conversationMessages.length - 1; i >= 0; i--) {
      const m = conversationMessages[i];
      if (m.username === "system" || m.deleted) continue;
      if (m.content.toLowerCase().includes(q) || m.username.toLowerCase().includes(q)) {
        matched.push(m);
        if (matched.length >= MAX_RESULTS) break;
      }
    }
    return matched;
  }, [query, conversationMessages]);

  // Reset selection when results change; update parent highlight term.
  useEffect(() => { setSelectedIndex(0); }, [results.length]);
  useEffect(() => {
    onHighlightChange(open && query.trim() ? query.trim() : "");
  }, [open, query, onHighlightChange]);

  // Keep the keyboard-selected result inside the result list viewport.
  useEffect(() => {
    const list = resultsRef.current;
    if (!list) return;
    const selected = list.querySelector<HTMLElement>(`[data-result-index="${selectedIndex}"]`);
    selected?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex]);

  // Focus input on open; reset on close.
  useEffect(() => {
    if (open) { setTimeout(() => inputRef.current?.focus(), 50); }
    else { setQuery(""); setSelectedIndex(0); }
  }, [open]);

  // Listen for external focus request (e.g., double CTRL+F when already open).
  useEffect(() => {
    const handler = () => { inputRef.current?.focus(); inputRef.current?.select(); };
    window.addEventListener(CHAT_EVENTS.focusConversationSearch, handler);
    return () => window.removeEventListener(CHAT_EVENTS.focusConversationSearch, handler);
  }, []);

  const scrollToMessage = useCallback((messageId: string) => {
    window.dispatchEvent(new CustomEvent(CHAT_EVENTS.scrollToMessage, { detail: { id: messageId } }));
    // Keep the panel open so the match stays highlighted; Escape closes it.
  }, []);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setSelectedIndex((i) => Math.min(i + 1, results.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setSelectedIndex((i) => Math.max(i - 1, 0)); }
    else if (e.key === "Enter" && results.length > 0) { const r = results[selectedIndex]; if (r) scrollToMessage(r.id); }
    else if (e.key === "Escape") { onClose(); }
  }, [results, selectedIndex, scrollToMessage, onClose]);

  if (!open) return null;

  const trimmedQuery = query.trim();

  return (
    <div className="border-b bg-muted/30 px-4 py-2 animate-in fade-in-0 slide-in-from-top-2 duration-200">
      <InputGroup className="mx-auto h-10 max-w-2xl rounded-lg border bg-card">
        <InputGroupAddon>
          <Search className="size-4 text-muted-foreground" />
        </InputGroupAddon>
        <InputGroupInput
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t("search.inConversation")}
          aria-label={t("search.inConversation")}
          className="text-sm"
        />
        {trimmedQuery && (
          <span className="mr-1 shrink-0 text-label tabular-nums text-muted-foreground">
            {results.length > 0
              ? t(results.length >= MAX_RESULTS ? "search.matchCountCapped" : "search.matchCount", { n: results.length })
              : t("search.noMatchesInConversation")}
          </span>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => setSelectedIndex((i) => Math.max(i - 1, 0))}
          disabled={results.length === 0}
          aria-label={t("a11y.prevResult")}
          className="rounded-md"
        >
          <ChevronUp className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => setSelectedIndex((i) => Math.min(i + 1, results.length - 1))}
          disabled={results.length === 0}
          aria-label={t("a11y.nextResult")}
          className="rounded-md"
        >
          <ChevronDown className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={onClose}
          aria-label={t("a11y.closeSearch")}
          className="mr-1 rounded-md"
        >
          <X className="size-3.5" />
        </Button>
      </InputGroup>

      {trimmedQuery && results.length > 0 && (
        <div ref={resultsRef} className="mx-auto mt-2 max-h-64 max-w-2xl overflow-y-auto rounded-lg border bg-card p-1 scrollbar-thin">
          {results.map((r, i) => (
            <Button
              key={r.id}
              type="button"
              variant="ghost"
              data-result-index={i}
              onClick={() => scrollToMessage(r.id)}
              className={cn(
                "h-auto w-full flex-col items-start gap-0.5 rounded-md px-3 py-2 text-left font-normal",
                i === selectedIndex && "bg-accent text-accent-foreground",
              )}
            >
              <div className="mb-0.5 flex items-center gap-2">
                <span className="text-xs font-medium text-muted-foreground">{r.username}</span>
                <span className="text-micro text-muted-foreground/50">{formatTime(r.timestamp, t)}</span>
              </div>
              <p className="line-clamp-2 text-xs text-foreground/80">
                {renderSnippet(r.content, trimmedQuery)}
              </p>
            </Button>
          ))}
        </div>
      )}

      {trimmedQuery && results.length === 0 && (
        <div className="py-4 text-center text-xs text-muted-foreground/60">
          {t("search.noMatchesInConversation")}
        </div>
      )}
    </div>
  );
}
