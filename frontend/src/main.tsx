import { createRoot } from "react-dom/client";
import { I18nProvider } from "./i18n/context";
import App from "./App";
import "./index.css";
import { applyTheme, getStoredTheme, THEME_STORAGE_KEY } from "@/lib/theme";

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
  navigator.serviceWorker.register("/sw.js").catch(() => {
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
