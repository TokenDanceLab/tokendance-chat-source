import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { ChatLayout } from "@/components/ChatLayout";
import { AuthModal } from "@/components/AuthModal";
import { useChatStore } from "@/stores/chatStore";
import { useWebSocket } from "@/hooks/useWebSocket";
import { fetchPublicMessages, fetchSession, JoinError, type SessionResponse } from "@/lib/api";
import { STORAGE_KEYS } from "@/lib/storage";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { useTranslation } from "@/i18n/context";
import { CHAT_EVENTS } from "@/lib/events";

const USERNAME_STORAGE_KEY = STORAGE_KEYS.username;
const AUTH_STORAGE_KEY = STORAGE_KEYS.auth;
// The server binds the first handle a browser joins with to its HttpOnly
// guest cookie, so the same browser (any tab) returns as the same temporary
// identity. localStorage is only a fast-path hint; the cookie is authoritative.
const GUEST_STORAGE_KEY = STORAGE_KEYS.guestName;

function generateGuestName(): string {
  return `guest_${Math.random().toString(36).slice(2, 10)}`;
}

function App() {
  const { t } = useTranslation();
  const { connect } = useWebSocket();
  const setView = useChatStore((s) => s.setView);
  const setStoreUsername = useChatStore((s) => s.setUsername);
  const setGuest = useChatStore((s) => s.setGuest);
  const setShowAuthModal = useChatStore((s) => s.setShowAuthModal);
  const setHistory = useChatStore((s) => s.setHistory);
  const initialSearchRef = useRef(window.location.search);

  useEffect(() => {
    const showUpdate = () => {
      toast(t("system.updateReady"), {
        id: "app-update",
        duration: Infinity,
        action: { label: t("system.refresh"), onClick: () => window.location.reload() },
      });
    };
    window.addEventListener(CHAT_EVENTS.updateReady, showUpdate);
    return () => window.removeEventListener(CHAT_EVENTS.updateReady, showUpdate);
  }, [t]);

  useEffect(() => {
    let cancelled = false;

    async function tryAutoConnect(name: string) {
      try {
        if (cancelled) return;
        setStoreUsername(name);
        setGuest(false);
        setView("chat");
        await connect(name);
      } catch {
        if (cancelled) return;
        // Connection failed — clear stale auth and show login modal.
        localStorage.removeItem(AUTH_STORAGE_KEY);
        localStorage.removeItem(USERNAME_STORAGE_KEY);
        setShowAuthModal(true);
      }
    }

    async function loadPublicPreview() {
      const messages = await fetchPublicMessages(100);
      if (!cancelled) {
        setHistory(messages);
      }
    }

    async function tryGuestConnect(boundHandle?: string) {
      let name = boundHandle?.trim() || "";
      if (!name) {
        try { name = localStorage.getItem(GUEST_STORAGE_KEY) || ""; } catch { /* quota */ }
      }
      if (!name) name = generateGuestName();
      try { localStorage.setItem(GUEST_STORAGE_KEY, name); } catch { /* quota */ }
      try {
        if (cancelled) return;
        localStorage.removeItem(AUTH_STORAGE_KEY);
        localStorage.removeItem(USERNAME_STORAGE_KEY);
        localStorage.removeItem(STORAGE_KEYS.legacySessionToken);
        setStoreUsername(name);
        setGuest(true);
        setView("chat");
        await connect(name);
      } catch (error) {
        if (cancelled) return;
        // The cookie is authoritative. If the stored hint no longer matches
        // (another browser claimed it, or this cookie was bound elsewhere),
        // adopt the bound handle — once — instead of dropping the identity.
        if (error instanceof JoinError && error.code === "GUEST_SESSION_REQUIRED") {
          // The upgrade arrived before a guest cookie existed (for example a
          // 429 on /api/session). Fetching the session establishes it; retry
          // once with the same handle.
          await fetchSession().catch(() => null);
          try {
            await connect(name);
            return;
          } catch { /* fall through to preview */ }
        }
        if (error instanceof JoinError && (error.code === "GUEST_IDENTITY_MISMATCH" || error.code === "GUEST_NAME_TAKEN")) {
          const session = await fetchSession().catch(() => null);
          const retry = session?.guest_username?.trim();
          if (retry && retry !== name) {
            try {
              localStorage.setItem(GUEST_STORAGE_KEY, retry);
              setStoreUsername(retry);
              await connect(retry);
              return;
            } catch { /* fall through to preview */ }
          }
          if (!retry && error.code === "GUEST_NAME_TAKEN") {
            const fresh = generateGuestName();
            try {
              localStorage.setItem(GUEST_STORAGE_KEY, fresh);
              setStoreUsername(fresh);
              await connect(fresh);
              return;
            } catch { /* fall through to preview */ }
          }
        }
        setStoreUsername("");
        setGuest(false);
        await loadPublicPreview();
      }
    }

    async function initialize() {
      // Remove the pre-BFF browser token from older releases.
      localStorage.removeItem(STORAGE_KEYS.legacySessionToken);

      // 1. Account callback
      const params = new URLSearchParams(initialSearchRef.current);
      const oidcSuccess = params.get("oidc_success");
      const oidcError = params.get("oidc_error");
      const oidcLoggedOut = params.get("oidc_logged_out") === "1";
      const oidcLogoutError = params.get("oidc_logout_error") === "1";

      if (oidcLoggedOut) {
        window.history.replaceState({}, "", "/");
        toast.success(t("auth.accountLoggedOut"));
      } else if (oidcLogoutError) {
        window.history.replaceState({}, "", "/");
        toast.error(t("auth.accountLogoutFailed"));
      }

      if (oidcSuccess === "1") {
        window.history.replaceState({}, "", "/");
        const session = await fetchSession().catch(
          () => ({ authenticated: false } as SessionResponse),
        );
        if (cancelled) return;
        if (session.authenticated && session.username) {
          localStorage.setItem(AUTH_STORAGE_KEY, "true");
          localStorage.setItem(USERNAME_STORAGE_KEY, session.username);
          await tryAutoConnect(session.username);
          return;
        }
        setShowAuthModal(true);
        return;
      } else if (oidcError) {
        window.history.replaceState({}, "", "/");
        setShowAuthModal(true);
        await loadPublicPreview();
        return;
      }

      // 2. The HttpOnly session cookie is the sole authority for signed-in state.
      const session = await fetchSession().catch(
        () => ({ authenticated: false } as SessionResponse),
      );
      if (cancelled) return;
      if (session.authenticated && session.username) {
        localStorage.setItem(AUTH_STORAGE_KEY, "true");
        localStorage.setItem(USERNAME_STORAGE_KEY, session.username);
        await tryAutoConnect(session.username);
        return;
      }

      localStorage.removeItem(AUTH_STORAGE_KEY);
      localStorage.removeItem(USERNAME_STORAGE_KEY);
      setShowAuthModal(false);
      await tryGuestConnect(session.guest_username);
    }

    const timer = window.setTimeout(() => {
      if (!cancelled) {
        void initialize();
      }
    }, 0);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <TooltipProvider delayDuration={200}>
      <ChatLayout />
      <AuthModal />
      <Toaster position="top-center" />
    </TooltipProvider>
  );
}

export default App;
