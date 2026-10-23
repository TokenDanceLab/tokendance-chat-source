import { useEffect } from "react";

/**
 * iOS Safari does not shrink `100dvh` when the software keyboard opens — the
 * visual viewport shrinks instead, so a shell sized in dvh keeps its original
 * height and the composer floats above the keyboard. Track the visual viewport
 * into `--app-height` so the chat shell matches exactly what the user sees.
 * Chrome on Android gets the same value via `interactive-widget=resizes-content`.
 */
export function useAppHeight() {
  useEffect(() => {
    const viewport = window.visualViewport;
    if (!viewport) return;
    const root = document.documentElement;
    const apply = () => {
      root.style.setProperty("--app-height", `${Math.round(viewport.height)}px`);
    };
    apply();
    viewport.addEventListener("resize", apply);
    viewport.addEventListener("scroll", apply);
    return () => {
      viewport.removeEventListener("resize", apply);
      viewport.removeEventListener("scroll", apply);
      root.style.removeProperty("--app-height");
    };
  }, []);
}
