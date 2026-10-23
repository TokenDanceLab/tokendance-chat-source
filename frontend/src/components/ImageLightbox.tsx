import { useEffect, useCallback, useRef } from "react";
import { X, ZoomIn, ZoomOut } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "@/i18n/context";
import { Button } from "@/components/ui/button";

interface ImageLightboxProps {
  imageUrl: string;
  onClose: () => void;
}

export function ImageLightbox({ imageUrl, onClose }: ImageLightboxProps) {
  const { t } = useTranslation();
  const [scale, setScale] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const imageWrapperRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const dragRef = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    },
    [onClose],
  );

  const handleWheel = useCallback((e: WheelEvent) => {
    e.preventDefault();
    setScale((s) => {
      const delta = e.deltaY > 0 ? -0.15 : 0.15;
      const next = Math.max(0.5, Math.min(3, s + delta));
      if (next <= 1) setOffset({ x: 0, y: 0 });
      return next;
    });
  }, []);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    closeRef.current?.focus();
    document.addEventListener("keydown", handleKeyDown);
    const wrapper = imageWrapperRef.current;
    wrapper?.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      wrapper?.removeEventListener("wheel", handleWheel);
      document.body.style.overflow = previousOverflow;
      previous?.focus?.();
    };
  }, [handleKeyDown, handleWheel]);

  const zoomIn = () => setScale((s) => Math.min(s + 0.25, 3));
  const zoomOut = () => setScale((s) => {
    const next = Math.max(s - 0.25, 0.5);
    if (next <= 1) setOffset({ x: 0, y: 0 });
    return next;
  });

  const startDrag = (e: React.PointerEvent) => {
    if (scale <= 1) return;
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
    dragRef.current = { x: e.clientX, y: e.clientY, ox: offset.x, oy: offset.y };
  };
  const moveDrag = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag) return;
    setOffset({ x: drag.ox + (e.clientX - drag.x), y: drag.oy + (e.clientY - drag.y) });
  };
  const endDrag = () => { dragRef.current = null; };

  return (
    <div
      className="fixed inset-0 z-50 flex animate-in items-center justify-center fade-in-0 duration-150"
      role="dialog"
      aria-modal="true"
      aria-label={t("a11y.imagePreview")}
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/80"
        onClick={onClose}
      />

      {/* Controls */}
      <div className="absolute top-4 right-4 flex items-center gap-2 z-10">
        <Button
          type="button"
          variant="secondary"
          size="icon-sm"
          onClick={zoomOut}
          className="size-11 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white sm:size-9"
          aria-label={t("a11y.zoomOut")}
        >
          <ZoomOut className="size-4" />
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="icon-sm"
          onClick={zoomIn}
          className="size-11 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white sm:size-9"
          aria-label={t("a11y.zoomIn")}
        >
          <ZoomIn className="size-4" />
        </Button>
        <Button
          ref={closeRef}
          type="button"
          variant="secondary"
          size="icon-sm"
          onClick={onClose}
          className="size-11 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white sm:size-9"
          aria-label={t("a11y.close")}
        >
          <X className="size-4" />
        </Button>
      </div>

      {/* Image */}
      <div
        ref={imageWrapperRef}
        className="relative flex max-w-[90vw] max-h-[90vh] items-center justify-center"
      >
        <img
          src={imageUrl}
          alt="Full-size image"
          className={scale > 1 ? "max-w-[90vw] max-h-[90vh] cursor-grab object-contain transition-transform duration-200 active:cursor-grabbing" : "max-w-full max-h-full object-contain cursor-zoom-in transition-transform duration-200"}
          style={{ transform: `translate(${offset.x}px, ${offset.y}px) scale(${scale})` }}
          draggable={false}
          onPointerDown={startDrag}
          onPointerMove={moveDrag}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onClick={(e) => e.stopPropagation()}
        />
      </div>
    </div>
  );
}
