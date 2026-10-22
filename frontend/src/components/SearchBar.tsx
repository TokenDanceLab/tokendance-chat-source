import { useState, useEffect, useCallback, useRef } from "react";
import { Search, X } from "lucide-react";
import { chatAPI, type SearchResult } from "@/lib/api";
import { CHAT_EVENTS } from "@/lib/events";
import { useTranslation } from "@/i18n/context";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

/** Splits an FTS5 snippet into text and highlighted segments. */
function parseSnippetParts(raw: string): Array<{ text: string; highlight: boolean }> {
  const parts: Array<{ text: string; highlight: boolean }> = [];
  const regex = /<mark>([\s\S]*?)<\/mark>/g;
  let lastIdx = 0;
  let match: RegExpExecArray | null;
  while ((match = regex.exec(raw)) !== null) {
    if (match.index > lastIdx) {
      parts.push({ text: raw.slice(lastIdx, match.index), highlight: false });
    }
    parts.push({ text: match[1], highlight: true });
    lastIdx = match.index + match[0].length;
  }
  if (lastIdx < raw.length) {
    parts.push({ text: raw.slice(lastIdx), highlight: false });
  }
  return parts;
}

export function SearchBar() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [noResults, setNoResults] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Ctrl+K to open, Escape to close.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.key === "k" || e.key === "K") && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((prev) => !prev);
        setQuery("");
        setResults([]);
        setError(false);
      }
      if (e.key === "Escape" && open) {
        setOpen(false);
        setQuery("");
        setResults([]);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open]);

  useEffect(() => {
    const openFromSidebar = () => {
      setOpen(true);
      setQuery("");
      setResults([]);
      setError(false);
    };
    window.addEventListener(CHAT_EVENTS.openSearch, openFromSidebar);
    return () => window.removeEventListener(CHAT_EVENTS.openSearch, openFromSidebar);
  }, []);

  // Debounced search.
  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      setError(false);
      setNoResults(false);
      return;
    }
    setLoading(true);
    setError(false);
    setNoResults(false);
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      try {
        const r = await chatAPI.searchMessages(query.trim());
        setResults(r);
        if (r.length === 0) setNoResults(true);
      } catch {
        setError(true);
      } finally {
        setLoading(false);
      }
    }, 300);
    return () => clearTimeout(debounceRef.current);
  }, [query]);

  const handleClickResult = useCallback((r: SearchResult) => {
    window.dispatchEvent(
      new CustomEvent(CHAT_EVENTS.scrollToMessage, { detail: { id: r.id, content: r.content } }),
    );
    setOpen(false);
    setQuery("");
    setResults([]);
  }, []);

  return (
    <CommandDialog
      open={open}
      onOpenChange={setOpen}
      title={t("search.placeholder")}
      description={t("search.typeToSearch")}
      className="max-w-xl border bg-popover text-popover-foreground"
    >
      <Command shouldFilter={false} className="bg-transparent">
            <div className="flex items-center gap-2 border-b px-2">
              <CommandInput
                autoFocus
                value={query}
                onValueChange={setQuery}
                placeholder={t("search.placeholder")}
                aria-label={t("search.placeholder")}
                className="h-12 text-sm"
              />
              {loading ? (
                <Spinner className="mr-2 size-4 shrink-0 text-muted-foreground" />
              ) : query ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Clear search"
                  onClick={() => setQuery("")}
                  className="mr-2 rounded-md"
                >
                  <X className="size-3.5" />
                </Button>
              ) : (
                <span className="mr-2 hidden text-label text-muted-foreground sm:inline">
                  ESC
                </span>
              )}
            </div>

            <CommandList className="max-h-80">
              {!query && (
                <div className="px-4 py-10 text-center">
                  <Search className="mx-auto mb-2 size-5 text-muted-foreground/30" />
                  <p className="text-xs text-muted-foreground/60">{t("search.typeToSearch")}</p>
                </div>
              )}
              {error && !loading && query && (
                <div className="px-4 py-10 text-center">
                  <p className="text-xs text-destructive/70">{t("search.searchError")}</p>
                </div>
              )}
              {noResults && !loading && !error && query && (
                <CommandEmpty className="py-10 text-xs text-muted-foreground/60">
                  {t("search.notFound")}
                </CommandEmpty>
              )}
              {results.length > 0 && (
                <CommandGroup heading={t("search.toggleSearch")}>
                  {results.map((r) => (
                    <CommandItem
                      key={r.id}
                      value={r.id}
                      onSelect={() => handleClickResult(r)}
                      className="flex flex-col items-start gap-1 rounded-md px-3 py-2.5"
                    >
                      <span className="text-label font-medium text-muted-foreground">
                        {r.username}
                      </span>
                      <span className="line-clamp-2 text-xs text-foreground/85">
                        {r.snippet
                          ? parseSnippetParts(r.snippet).map((part, j) =>
                              part.highlight ? (
                                <mark
                                  key={j}
                                  className="rounded-sm bg-warning/35 px-0.5 text-foreground"
                                >
                                  {part.text}
                                </mark>
                              ) : (
                                <span key={j}>{part.text}</span>
                              ),
                            )
                          : r.content.substring(0, 120)}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
            </CommandList>

            <div className="flex items-center gap-2 border-t px-3 py-2">
              <kbd className="rounded-md border bg-muted px-1.5 py-0.5 font-mono text-micro text-muted-foreground">
                Ctrl+K
              </kbd>
              <span className="ml-auto text-micro text-muted-foreground/60">
                {t("search.toggleSearch")}
              </span>
            </div>
      </Command>
    </CommandDialog>
  );
}
