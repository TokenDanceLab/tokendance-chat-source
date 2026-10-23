import { memo, useCallback, useState, useMemo, useRef, useEffect } from "react";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI } from "@/lib/api";
import { STORAGE_KEYS } from "@/lib/storage";
import { Upload, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const RECENTS_KEY = STORAGE_KEYS.emojiRecents;
const MAX_RECENTS = 18;

function getRecents(): string[] {
  try {
    const stored = localStorage.getItem(RECENTS_KEY);
    return stored ? JSON.parse(stored) : [];
  } catch { return []; }
}

function saveRecents(emoji: string): string[] {
  const recents = getRecents().filter((e) => e !== emoji);
  recents.unshift(emoji);
  const trimmed = recents.slice(0, MAX_RECENTS);
  try { localStorage.setItem(RECENTS_KEY, JSON.stringify(trimmed)); } catch { /* ignore */ }
  return trimmed;
}

const CATEGORIES: { name: string; emojis: string[] }[] = [
  { name: "Smileys", emojis: ["😀", "😃", "😄", "😁", "😅", "🤣", "😂", "🙂", "😊", "😇", "😍", "🤩", "😘", "😗", "😋", "😛", "😜", "🤪", "😝", "🤗", "🤔", "😐", "😑", "😶", "😏", "😒", "🙄", "😌", "😔", "😪", "😴", "😷", "🤒", "🤕", "🥵", "🥶", "😵", "🤯", "🥳", "😎", "🤓", "😟", "😮", "😯", "😲", "😳", "🥺", "😦", "😨", "😰", "😢", "😭", "😱", "😡", "🤬", "😤"] },
  { name: "Gestures", emojis: ["👍", "👎", "👌", "✌️", "🤞", "🤟", "🤘", "🤙", "👋", "✋", "🖐", "👆", "👇", "👉", "👈", "🙌", "👏", "🙏", "🤝", "💪", "🦾", "🖕", "✍️", "🤲", "🫶", "🤌"] },
  { name: "Hearts", emojis: ["❤️", "🧡", "💛", "💚", "💙", "💜", "🖤", "🤍", "🤎", "💔", "💕", "💖", "💗", "💘", "💝", "💞", "💟", "♥️", "🩷", "🩵", "🩶"] },
  { name: "Objects", emojis: ["🎉", "🎊", "🎂", "🔥", "⭐", "🌟", "✨", "💯", "✅", "❌", "💤", "🎵", "🎶", "📌", "📍", "💡", "🔔", "🔕", "💬", "💭", "🚀", "🌈", "☀️", "🌙", "⚡", "💎", "🎁", "🏆", "🎯", "💰", "📢", "📣"] },
  { name: "Misc", emojis: ["👿", "💀", "💩", "🤡", "👻", "👽", "🤖", "😺", "😸", "😹", "😻", "🐶", "🐱", "🦊", "🐼", "🐨", "🐸", "🦄", "🐙", "🍕", "🍔", "🌮", "☕", "🍺", "🎮", "⚽", "🏀", "🧠", "👀", "👃", "🫀"] },
];

interface EmojiPickerProps {
  onSelect: (emoji: string) => void;
  onClose: () => void;
}

export const EmojiPicker = memo(function EmojiPicker({
  onSelect,
  onClose,
}: EmojiPickerProps) {
  const { t } = useTranslation();
  const [activeCat, setActiveCat] = useState(0);
  const [search, setSearch] = useState("");
  const [recents] = useState(getRecents);
  const [emojiError, setEmojiError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const customEmojis = useChatStore((s) => s.customEmojis);
  const username = useChatStore((s) => s.username);

  // Request emoji list on mount.
  useEffect(() => {
    chatAPI.sendCustomEmojiList();
  }, []);

  const categoryLabels = useMemo(() => ({
    Smileys: t("emoji.smileys"),
    Gestures: t("emoji.gestures"),
    Hearts: t("emoji.hearts"),
    Objects: t("emoji.objects"),
    Misc: t("emoji.misc"),
    Custom: t("emoji.custom"),
  }), [t]);

  const handleSelect = useCallback(
    (emoji: string) => {
      saveRecents(emoji);
      onSelect(emoji);
      onClose();
    },
    [onSelect, onClose],
  );

  const handleCustomSelect = useCallback(
    (name: string) => {
      onSelect(`:${name}:`);
      onClose();
    },
    [onSelect, onClose],
  );

  const handleUploadClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileChange = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (!file) return;

      const validTypes = ["image/png", "image/jpeg", "image/gif", "image/webp"];
      if (!validTypes.includes(file.type)) {
        setEmojiError("Invalid file type. Allowed: PNG, JPG, GIF, WebP");
        return;
      }

      if (file.size > 128 * 1024) {
        setEmojiError("File too large. Max 128KB");
        return;
      }

      setUploading(true);
      setEmojiError(null);

      const baseName = file.name.replace(/\.[^.]+$/, "");
      const emojiName = baseName.replace(/[^a-zA-Z0-9_]/g, "_").slice(0, 32) || "emoji";

      try {
        const url = await chatAPI.uploadEmoji(file, emojiName);
        if (url) {
          chatAPI.sendCustomEmojiAdd(emojiName, url);
        } else {
          setEmojiError("Upload failed");
        }
      } catch {
        setEmojiError("Upload failed");
      } finally {
        setUploading(false);
        if (fileInputRef.current) {
          fileInputRef.current.value = "";
        }
      }
    },
    [],
  );

  const handleDelete = useCallback(
    (name: string) => {
      chatAPI.sendCustomEmojiDelete(name);
    },
    [],
  );

  const searchRef = useRef<HTMLInputElement>(null);

  // The picker is a modal: focus moves in, Escape closes from anywhere, and
  // focus returns to the trigger when it unmounts.
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    searchRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, [onClose]);

  const filteredEmojis = useMemo(() => {
    if (!search.trim()) return CATEGORIES[activeCat].emojis;
    const lower = search.toLowerCase();
    const all: string[] = [];
    for (const cat of CATEGORIES) {
      for (const e of cat.emojis) {
        if (e.includes(lower) || (e.codePointAt(0)?.toString(16) || "").includes(lower)) {
          all.push(e);
        }
      }
    }
    return all.slice(0, 60);
  }, [search, activeCat]);

  return (
    <div
      data-visual="emoji-picker"
      className="fixed inset-0 z-[200] flex animate-in items-center justify-center bg-black/30 p-4 fade-in-0 duration-150"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div
        data-visual="emoji-picker-panel"
        role="dialog"
        aria-modal="true"
        aria-label={t("emoji.openPicker")}
        className="flex max-h-[460px] w-[360px] max-w-full animate-in flex-col overflow-hidden rounded-2xl border bg-popover text-popover-foreground shadow-xl fade-in-0 zoom-in-95 duration-150"
      >
        {/* Search bar */}
        <div className="flex items-center gap-2 px-3 pt-3 pb-2">
          <Input
            ref={searchRef}
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("emoji.search")}
            aria-label={t("emoji.search")}
            className="h-9 rounded-lg bg-muted/60 text-xs"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={t("emoji.close")}
            className="size-9 shrink-0 rounded-lg"
            onClick={onClose}
          >
            <X className="size-4" />
          </Button>
        </div>

        {/* Recent emojis */}
        {!search && recents.length > 0 && (
          <div className="border-b px-3 pb-2">
            <span className="mb-1 block text-micro uppercase tracking-[0.12em] text-muted-foreground/60">{t("emoji.recent")}</span>
            <div className="flex flex-wrap gap-1">
              {recents.map((emoji) => (
                <Button key={emoji} type="button" variant="ghost" size="icon-sm"
                  onClick={() => handleSelect(emoji)}
                  className="size-8 rounded-xl text-lg">
                  {emoji}
                </Button>
              ))}
            </div>
          </div>
        )}

        {/* Category tabs */}
        {!search && (
          <div className="flex flex-wrap gap-0.5 border-b px-3 pb-2">
            {CATEGORIES.map((cat, i) => (
              <Button key={cat.name} type="button" variant={i === activeCat ? "secondary" : "ghost"} size="xs"
                onClick={() => setActiveCat(i)}
                className="rounded-full px-2.5 text-micro">
                {categoryLabels[cat.name as keyof typeof categoryLabels] || cat.name}
              </Button>
            ))}
            <Button type="button" variant={-1 === activeCat ? "secondary" : "ghost"} size="xs"
              onClick={() => setActiveCat(-1)}
              className="rounded-full px-2.5 text-micro">
              {categoryLabels.Custom}
            </Button>
          </div>
        )}

        {/* Emoji grid or CustomEmoji inline grid */}
        {activeCat === -1 && !search ? (
          <div className="overflow-y-auto flex-1 p-3 custom-scrollbar">
            {/* Upload header */}
            <div className="flex items-center justify-between mb-2">
              <span className="text-micro text-muted-foreground/60">{t("emoji.custom")}</span>
              <Button
                type="button"
                variant="secondary"
                size="xs"
                onClick={handleUploadClick}
                disabled={uploading}
                className="rounded-full text-micro text-primary"
              >
                <Upload className="size-3" />
                {uploading ? "..." : t("emoji.uploadEmoji")}
              </Button>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/png,image/jpeg,image/gif,image/webp"
                onChange={handleFileChange}
                className="hidden"
              />
            </div>
            {emojiError && (
              <div className="mb-2 text-micro text-red-500">{emojiError}</div>
            )}
            {customEmojis.length === 0 ? (
              <div className="text-center py-8 text-xs text-muted-foreground/50">{t("emoji.noCustomEmoji")}</div>
            ) : (
              <div className="grid grid-cols-5 gap-2">
                {customEmojis.map((emoji) => (
                  <div key={emoji.name} className="group relative">
                    <Button
                      type="button"
                      variant="ghost"
                      onClick={() => handleCustomSelect(emoji.name)}
                      title={`:${emoji.name}:`}
                      className="h-auto w-full flex-col gap-1 rounded-xl p-1.5"
                    >
                      <div className="w-10 h-10 flex items-center justify-center">
                        <img
                          src={emoji.url}
                          alt={emoji.name}
                          className="max-w-full max-h-full object-contain"
                          loading="lazy"
                        />
                      </div>
                      <span className="text-[8px] text-muted-foreground/60 truncate w-full text-center leading-none">
                        :{emoji.name}:
                      </span>
                    </Button>
                    {emoji.uploader === username && (
                      <Button
                        type="button"
                        variant="destructive"
                        size="icon-xs"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDelete(emoji.name);
                        }}
                        className="absolute -right-1 -top-1 size-4 rounded-full opacity-0 transition-opacity group-hover:opacity-100"
                        title={t("emoji.deleteEmoji")}
                      >
                        <Trash2 className="size-2" />
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : (
        <div className="overflow-y-auto flex-1 p-3 custom-scrollbar">
          <div className="grid grid-cols-8 gap-0.5">
            {filteredEmojis.map((emoji) => (
              <Button key={emoji} type="button" variant="ghost" size="icon-sm"
                onClick={() => handleSelect(emoji)}
                className="size-9 rounded-xl text-lg">
                {emoji}
              </Button>
            ))}
          </div>
          {filteredEmojis.length === 0 && (
            <div className="text-center py-8 text-xs text-muted-foreground/50">{t("emoji.noResults")}</div>
          )}
        </div>
        )}
      </div>
    </div>
  );
});
