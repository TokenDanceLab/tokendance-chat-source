import { createRoot } from "react-dom/client";
import { I18nProvider } from "./i18n/context";
import App from "./App";
import "./index.css";
import { applyTheme, getStoredTheme, THEME_STORAGE_KEY } from "@/lib/theme";
import { CHAT_EVENTS } from "@/lib/events";

// Theme initialization — runs before React to avoid flash
function initTheme() {
  applyTheme(getStoredTheme());
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (localStorage.getItem(THEME_STORAGE_KEY) === "system") applyTheme("system");
  });
}
initTheme();

// Register service worker for PWA offline support.
if ("serviceWorker" in navigator && import.meta.env.PROD) {
  // A long-lived tab keeps its old controller until it navigates. Tell the UI
  // when a fresh worker takes over so the user can refresh deliberately,
  // instead of silently pinning a stale bundle for days.
  const hadController = Boolean(navigator.serviceWorker.controller);
  let notified = false;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (!hadController || notified) return;
    notified = true;
    window.dispatchEvent(new CustomEvent(CHAT_EVENTS.updateReady));
  });
  navigator.serviceWorker.register("/sw.js").then((registration) => {
    window.setInterval(() => {
      registration.update().catch(() => {
        // Offline or transient failure — the next check will retry.
      });
    }, 15 * 60 * 1000);
  }).catch(() => {
    // SW registration failed — app still works online.
  });
}

import { ErrorBoundary } from "@/components/ErrorBoundary";
import { chatAPI } from "@/lib/api";

// Expose chatAPI for E2E tests (sends via existing WS, avoids kick mechanism).
// Only available in dev mode or when ?e2e query param is present.
if (import.meta.env.DEV || new URLSearchParams(window.location.search).has('e2e')) {
  (window as any).__chatAPI = chatAPI;
}

createRoot(document.getElementById("root")!).render(
  <I18nProvider>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </I18nProvider>,
);
