import { useEffect, useRef, useMemo } from "react";
import type { ComponentType } from "react";
import { Copy, Reply, Trash2, CheckSquare, Pencil, Pin, Languages, SmilePlus } from "lucide-react";
import type { LucideProps } from "lucide-react";
import { useTranslation } from "@/i18n/context";
import type { ChatMessage } from "@/lib/api";
import { Button } from "@/components/ui/button";

interface MessageContextMenuProps {
  message: ChatMessage;
  isOwn: boolean;
  position: { x: number; y: number };
  onClose: () => void;
  onReply: () => void;
  onCopy: () => void;
  onDelete: () => void;
  onSelect: () => void;
  onEdit?: () => void;
  onPin?: () => void;
  onReact?: () => void;
  onTranslate?: () => void;
}

type MenuItem =
  | { kind: "divider" }
  | {
      kind?: "action";
      icon: ComponentType<LucideProps>;
      label: string;
      shortcut?: string;
      onClick: () => void;
      className: string;
    };

const divider = { kind: "divider" as const } satisfies MenuItem;

export function MessageContextMenu({
  message: _message,
  isOwn,
  position,
  onClose,
  onReply,
  onCopy,
  onDelete,
  onSelect,
  onEdit,
  onPin,
  onReact,
  onTranslate,
}: MessageContextMenuProps) {
  const { t } = useTranslation();
  const menuRef = useRef<HTMLDivElement>(null);

  // Close on outside click/touch
  useEffect(() => {
    const handler = (e: MouseEvent | TouchEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const timer = setTimeout(() => {
      document.addEventListener("mousedown", handler);
      document.addEventListener("touchstart", handler);
    }, 100);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("mousedown", handler);
      document.removeEventListener("touchstart", handler);
    };
  }, [onClose]);

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  // Calculate position to keep menu within viewport
  const menuStyle = useMemo(() => {
    const menuWidth = 220;
    // Estimate height based on visible items
    let itemCount = 3; // Reply, Copy, Select
    if (isOwn && onEdit) itemCount += 1;
    if (onPin) itemCount += 1;
    if (onReact) itemCount += 1;
    if (onTranslate) itemCount += 1;
    if (isOwn) itemCount += 1; // Delete
    itemCount += 2; // dividers
    const menuHeight = itemCount * 44 + 16;
    let { x, y } = position;
    if (x + menuWidth > window.innerWidth) x = window.innerWidth - menuWidth - 8;
    if (y + menuHeight > window.innerHeight) y = window.innerHeight - menuHeight - 8;
    if (x < 8) x = 8;
    if (y < 8) y = 8;
    return { left: x, top: y };
  }, [position, isOwn, onEdit, onPin, onReact, onTranslate]);

  const menuItems: MenuItem[] = [
    {
      icon: Reply,
      label: t("input.replyTo"),
      shortcut: "DblClick",
      onClick: onReply,
      className: "text-foreground/85",
    },
    {
      icon: Copy,
      label: t("transcript.contextCopy"),
      shortcut: "Ctrl+C",
      onClick: onCopy,
      className: "text-foreground/85",
    },
    ...(onReact
      ? [
          {
            icon: SmilePlus,
            label: t("message.react"),
            onClick: onReact,
            className: "text-foreground/85",
          },
        ]
      : []),
    ...(onTranslate
      ? [
          {
            icon: Languages,
            label: t("message.translate"),
            onClick: onTranslate,
            className: "text-foreground/85",
          },
        ]
      : []),
    divider,
    ...(isOwn && onEdit
      ? [
          {
            icon: Pencil,
            label: t("message.edit"),
            shortcut: "↑",
            onClick: onEdit,
            className: "text-foreground/85",
          },
        ]
      : []),
    ...(onPin
      ? [
          {
            icon: Pin,
            label: t("message.pin"),
            onClick: onPin,
            className: "text-foreground/85",
          },
        ]
      : []),
    {
      icon: CheckSquare,
      label: t("transcript.contextSelect"),
      onClick: onSelect,
      className: "text-foreground/85",
    },
    divider,
    ...(isOwn
      ? [
          {
            icon: Trash2,
            label: t("transcript.contextDelete"),
            onClick: onDelete,
            className: "text-destructive/85 hover:!text-destructive hover:!bg-destructive/10",
          },
        ]
      : []),
  ];

  return (
    <>
      {/* Backdrop */}
      <div
        className="context-menu-backdrop"
        onClick={onClose}
        onTouchEnd={onClose}
        aria-hidden="true"
      />
      {/* Menu */}
      <div
        ref={menuRef}
        role="menu"
        aria-label={t("message.contextMenu")}
        className="context-menu min-w-[228px] animate-in overflow-hidden rounded-2xl border bg-popover p-1 text-popover-foreground shadow-lg fade-in-0 zoom-in-95 duration-150"
        style={{
          left: menuStyle.left,
          top: menuStyle.top,
          "--origin-x": `${position.x}px`,
          "--origin-y": `${position.y}px`,
        } as React.CSSProperties}
      >
        {menuItems.map((item, idx) => {
          if (item.kind === "divider") {
            return <div key={idx} className="my-1 h-px bg-border" />;
          }
          return (
            <Button
              key={idx}
              type="button"
              role="menuitem"
              variant="ghost"
              onClick={item.onClick}
              className={`min-h-11 w-full justify-start gap-3 rounded-md px-3 text-sm font-normal ${item.className}`}
            >
              <item.icon className="size-4 shrink-0 opacity-70" />
              <span className="flex-1 text-left">{item.label}</span>
              {item.shortcut && (
                <kbd className="ml-4 font-mono text-micro tracking-wider text-muted-foreground/40">
                  {item.shortcut}
                </kbd>
              )}
            </Button>
          );
        })}
      </div>
    </>
  );
}
