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
  const imageWrapperRef = useRef<HTMLDivElement>(null);

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
      return Math.max(0.5, Math.min(3, s + delta));
    });
  }, []);

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    const wrapper = imageWrapperRef.current;
    wrapper?.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      wrapper?.removeEventListener("wheel", handleWheel);
    };
  }, [handleKeyDown, handleWheel]);

  const zoomIn = () => setScale((s) => Math.min(s + 0.25, 3));
  const zoomOut = () => setScale((s) => Math.max(s - 0.25, 0.5));

  return (
    <div className="fixed inset-0 z-50 flex animate-in items-center justify-center fade-in-0 duration-150">
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
          className="size-9 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
          aria-label={t("a11y.zoomOut")}
        >
          <ZoomOut className="size-4" />
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="icon-sm"
          onClick={zoomIn}
          className="size-9 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
          aria-label={t("a11y.zoomIn")}
        >
          <ZoomIn className="size-4" />
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="icon-sm"
          onClick={onClose}
          className="size-9 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
          aria-label={t("a11y.close")}
        >
          <X className="size-4" />
        </Button>
      </div>

      {/* Image */}
      <div
        ref={imageWrapperRef}
        className="relative max-w-[90vw] max-h-[90vh] overflow-hidden"
      >
        <img
          src={imageUrl}
          alt="Full-size image"
          className="max-w-full max-h-full object-contain cursor-zoom-in transition-transform duration-200"
          style={{ transform: `scale(${scale})` }}
          draggable={false}
          onClick={(e) => e.stopPropagation()}
        />
      </div>
    </div>
  );
}
