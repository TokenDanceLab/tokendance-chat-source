import { useState, useCallback, type FormEvent, type KeyboardEvent } from "react";
import { X, Eye, EyeOff } from "lucide-react";
import { useTranslation } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useServerConfig } from "@/hooks/useServerConfig";
import { loginUser, registerUser, logout, ChatError, ErrorCode } from "@/lib/api";
import { OidcLoginButton } from "./OidcLoginButton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";

const USERNAME_STORAGE_KEY = "tokendance:username";
const AUTH_STORAGE_KEY = "tokendance:auth";

type TabView = "guest" | "login" | "register";

function getErrorMessage(err: unknown, t: (key: string) => string): string {
  if (err instanceof ChatError) {
    switch (err.code) {
      case ErrorCode.TIMEOUT:
        return t("error.timeout");
      case ErrorCode.CLOSED:
        return t("error.closed");
      case ErrorCode.CANNOT_CONNECT:
        return t("error.cannotConnect");
    }
  }
  if (err instanceof Error) return err.message;
  return t("error.unknown");
}

export function AuthModal() {
  const { t } = useTranslation();
  const show = useChatStore((s) => s.showAuthModal);
  const setShowAuthModal = useChatStore((s) => s.setShowAuthModal);
  const setView = useChatStore((s) => s.setView);
  const setStoreUsername = useChatStore((s) => s.setUsername);
  const setGuest = useChatStore((s) => s.setGuest);
  const { connect } = useWebSocket();
  const serverConfig = useServerConfig();
  const accountOnly = serverConfig?.oidc_enabled === true;

  const [tab, setTab] = useState<TabView>("guest");
  const [guestName, setGuestName] = useState("");
  const [loginUsername, setLoginUsername] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [regUsername, setRegUsername] = useState("");
  const [regPassword, setRegPassword] = useState("");
  const [regConfirmPassword, setRegConfirmPassword] = useState("");
  const [regInviteCode, setRegInviteCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const close = useCallback(() => {
    if (!loading) setShowAuthModal(false);
  }, [loading, setShowAuthModal]);

  const handleJoinSuccess = useCallback(
    async (name: string, persistAuth = false) => {
      try {
        await connect(name);
        if (persistAuth) {
          localStorage.setItem(AUTH_STORAGE_KEY, "true");
        }
        localStorage.setItem(USERNAME_STORAGE_KEY, name);
        setStoreUsername(name);
        setView("chat");
        setLoading(false);
        setShowAuthModal(false);
      } catch (err) {
        if (persistAuth) {
          await logout().catch(() => undefined);
          localStorage.removeItem(AUTH_STORAGE_KEY);
          localStorage.removeItem(USERNAME_STORAGE_KEY);
        }
        setError(getErrorMessage(err, t));
        setLoading(false);
      }
    },
    [connect, setStoreUsername, setView, setShowAuthModal, t],
  );

  // ── Guest join ──
  const handleGuestJoin = useCallback(
    async (e?: FormEvent) => {
      e?.preventDefault();
      const trimmed = guestName.trim();
      if (!trimmed) { setError(t("join.errorEmpty")); return; }
      if (trimmed.length < 2) { setError(t("join.errorTooShort")); return; }
      if (trimmed.length > 20) { setError(t("join.errorTooLong")); return; }
      if (!/^[一-龥a-zA-Z0-9_]+$/.test(trimmed)) { setError(t("join.errorInvalidChars")); return; }
      setError("");
      setLoading(true);
      setGuest(true);
      await handleJoinSuccess(trimmed);
    },
    [guestName, handleJoinSuccess, t, setGuest],
  );

  // ── Login ──
  const handleLogin = useCallback(
    async (e?: FormEvent) => {
      e?.preventDefault();
      if (!loginUsername.trim() || !loginPassword) {
        setError(t("auth.fillAllFields")); return;
      }
      setError("");
      setLoading(true);
      try {
        const result = await loginUser(loginUsername.trim(), loginPassword);
        setGuest(false);
        await handleJoinSuccess(result.username, true);
      } catch (err) {
        setLoading(false);
        if (err instanceof ChatError && err.code === ErrorCode.AUTH_FAILED) {
          setError(t("auth.loginFailed"));
        } else {
          setError(err instanceof Error ? err.message : t("auth.loginFailed"));
        }
      }
    },
    [loginUsername, loginPassword, handleJoinSuccess, t, setGuest],
  );

  // ── Register ──
  const handleRegister = useCallback(
    async (e?: FormEvent) => {
      e?.preventDefault();
      const uname = regUsername.trim();
      if (!uname || !regPassword || !regConfirmPassword || !regInviteCode.trim()) {
        setError(t("auth.fillAllFields")); return;
      }
      if (regPassword.length < 8) { setError(t("auth.passwordMinLength")); return; }
      if (regPassword !== regConfirmPassword) { setError(t("auth.confirmNotMatch")); return; }
      setError("");
      setLoading(true);
      try {
        const result = await registerUser(uname, regPassword, regInviteCode.trim());
        setGuest(false);
        await handleJoinSuccess(result.username, true);
      } catch (err) {
        setLoading(false);
        const msg = err instanceof Error ? err.message : "";
        if (msg.includes("invalid") || msg.includes("Invalid")) setError(t("auth.invalidCode"));
        else if (msg.includes("exhausted")) setError(t("auth.codeUsed"));
        else if (msg.includes("already") || msg.includes("exists") || msg.includes("taken")) setError(msg);
        else setError(msg || t("auth.loginFailed"));
      }
    },
    [regUsername, regPassword, regConfirmPassword, regInviteCode, handleJoinSuccess, t, setGuest],
  );

  const title = accountOnly
    ? t("auth.oidcLoginButton")
    : tab === "guest"
      ? t("join.buttonGuest")
      : tab === "login"
        ? t("auth.login")
        : t("auth.register");
  const inputClass =
    "h-11 w-full rounded-xl border-border/70 bg-muted/40 px-3.5 text-[15px] focus-visible:border-ring/50 focus-visible:ring-2 focus-visible:ring-ring/15 disabled:opacity-50";
  const primaryButtonClass =
    "flex min-h-[46px] w-full items-center justify-center gap-2 rounded-xl bg-primary text-[15px] font-semibold text-primary-foreground hover:bg-primary/90 disabled:bg-muted disabled:text-muted-foreground";
  const inlineAuthSwitchClass = "inline-flex min-h-11 min-w-11 items-center justify-center rounded-lg px-2 align-middle font-medium text-primary hover:underline";

  return (
    <Dialog open={show} onOpenChange={(open) => { if (!open) close(); }}>
      <DialogContent
        showCloseButton={false}
        data-visual="auth-modal"
        className="max-h-[calc(100dvh-2rem)] max-w-[400px] gap-0 overflow-y-auto rounded-2xl border bg-card p-0 text-card-foreground sm:max-w-[400px]"
      >
        <DialogTitle className="sr-only">{title}</DialogTitle>
        <DialogDescription className="sr-only">{t("join.subtitle")}</DialogDescription>

        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={close}
          disabled={loading}
          data-visual="auth-modal-close"
          className="absolute right-3 top-3 z-10 size-11 text-muted-foreground disabled:opacity-50"
          aria-label={t("a11y.close")}
        >
          <X className="size-4" />
        </Button>

        <div className="flex items-center gap-3 px-5 pb-4 pr-16 pt-6">
          <div className="flex size-11 items-center justify-center overflow-hidden rounded-lg border bg-white">
            <img src="/tokendance-icon-rounded.svg" alt="TokenDance" className="size-10" draggable={false} />
          </div>
          <div className="min-w-0">
            <p className="truncate text-base font-semibold">TokenDance Chat</p>
            <p className="truncate text-xs text-muted-foreground">{t("join.subtitle")}</p>
          </div>
        </div>

        {!accountOnly && (
          <Tabs
            value={tab}
            onValueChange={(value) => {
              setTab(value as TabView);
              setError("");
            }}
          >
            <TabsList
              data-visual="auth-modal-tabs"
              className="grid w-full grid-cols-3 rounded-none border-y bg-muted/30 p-0"
            >
              {(["guest", "login", "register"] as TabView[]).map((tv) => (
                <TabsTrigger
                  key={tv}
                  value={tv}
                  data-visual="auth-modal-tab"
                  data-active={tab === tv ? "true" : "false"}
                  disabled={loading}
                  className="min-h-11 rounded-none text-[14px] data-[state=active]:bg-card data-[state=active]:shadow-none"
                >
                  {tv === "guest" ? t("join.buttonGuest") : tv === "login" ? t("auth.login") : t("auth.register")}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        )}

        <div data-visual="auth-modal-content" className="p-5">

          {accountOnly && (
            <div className="space-y-5">
              <OidcLoginButton primary />
              <div className="flex items-center gap-3 text-[12px] text-muted-foreground">
                <span className="h-px flex-1 bg-border" />
                {t("auth.or")}
                <span className="h-px flex-1 bg-border" />
              </div>
              <form onSubmit={handleGuestJoin} className="space-y-3">
                <p className="text-name leading-relaxed text-muted-foreground">{t("join.welcomeHint")}</p>
                <Input
                  type="text"
                  value={guestName}
                  onChange={(e) => setGuestName(e.target.value)}
                  placeholder={t("join.placeholder")}
                  aria-label={t("join.placeholder")}
                  disabled={loading}
                  autoComplete="off"
                  className={inputClass}
                />
                <Button
                  type="submit"
                  variant="outline"
                  disabled={loading || !guestName.trim()}
                  data-visual="auth-modal-guest"
                  className="h-11 w-full rounded-xl text-name"
                >
                  {loading ? <Spinner className="size-4" /> : null}
                  {t("join.buttonJoin")}
                </Button>
              </form>
            </div>
          )}

          {/* ── Guest Tab ── */}
          {!accountOnly && tab === "guest" && (
            <form onSubmit={handleGuestJoin} className="space-y-4">
              <p className="text-name leading-relaxed text-muted-foreground">{t("join.welcomeHint")}</p>
              <Input
                type="text"
                value={guestName}
                onChange={(e) => setGuestName(e.target.value)}
                onKeyDown={(e: KeyboardEvent) => e.key === "Enter" && handleGuestJoin()}
                placeholder={t("join.placeholder")}
                aria-label={t("join.placeholder")}
                disabled={loading}
                autoFocus
                className={inputClass}
              />
              <Button
                type="submit"
                disabled={loading || !guestName.trim()}
                data-visual="auth-modal-primary"
                className={primaryButtonClass}
              >
                {loading ? <Spinner className="size-4" /> : null}
                {t("join.buttonJoin")}
              </Button>
            </form>
          )}

          {/* ── Login Tab ── */}
          {!accountOnly && tab === "login" && (
            <form onSubmit={handleLogin} className="space-y-4">
              <Input
                type="text"
                value={loginUsername}
                onChange={(e) => setLoginUsername(e.target.value)}
                placeholder={t("auth.username")}
                aria-label={t("auth.username")}
                disabled={loading}
                autoFocus
                autoComplete="username"
                className={inputClass}
              />
              <div className="relative">
                <Input
                  type={showPassword ? "text" : "password"}
                  value={loginPassword}
                  onChange={(e) => setLoginPassword(e.target.value)}
                  placeholder={t("auth.password")}
                  aria-label={t("auth.password")}
                  disabled={loading}
                  autoComplete="current-password"
                  className={`${inputClass} pr-12`}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setShowPassword(!showPassword)}
                  data-visual="auth-modal-password-toggle"
                  className="absolute right-0 top-0 h-11 w-11 rounded-xl text-muted-foreground"
                  aria-label={showPassword ? t("a11y.hidePassword") : t("a11y.showPassword")}
                >
                  {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </Button>
              </div>
              <Button
                type="submit"
                disabled={loading || !loginUsername.trim() || !loginPassword}
                data-visual="auth-modal-primary"
                className={primaryButtonClass}
              >
                {loading ? <Spinner className="size-4" /> : null}
                {t("auth.loginButton")}
              </Button>
              <p className="text-center text-name text-muted-foreground">
                {t("auth.noAccount")}{" "}
                <Button type="button" variant="link" data-visual="auth-modal-inline-switch" onClick={() => { setTab("register"); setError(""); }} className={inlineAuthSwitchClass}>
                  {t("auth.register")}
                </Button>
              </p>
            </form>
          )}

          {/* ── Register Tab ── */}
          {!accountOnly && tab === "register" && (
            <form onSubmit={handleRegister} className="space-y-3">
              <Input
                type="text"
                value={regUsername}
                onChange={(e) => setRegUsername(e.target.value)}
                placeholder={t("auth.username")}
                aria-label={t("auth.username")}
                disabled={loading}
                autoFocus
                autoComplete="username"
                className={inputClass}
              />
              <Input
                type="password"
                value={regPassword}
                onChange={(e) => setRegPassword(e.target.value)}
                placeholder={t("auth.password")}
                aria-label={t("auth.password")}
                disabled={loading}
                autoComplete="new-password"
                className={inputClass}
              />
              <Input
                type="password"
                value={regConfirmPassword}
                onChange={(e) => setRegConfirmPassword(e.target.value)}
                placeholder={t("auth.confirmPassword")}
                aria-label={t("auth.confirmPassword")}
                disabled={loading}
                autoComplete="new-password"
                className={inputClass}
              />
              <Input
                type="text"
                value={regInviteCode}
                onChange={(e) => setRegInviteCode(e.target.value)}
                placeholder={t("auth.inviteCode")}
                aria-label={t("auth.inviteCode")}
                disabled={loading}
                className={inputClass}
              />
              <Button
                type="submit"
                disabled={loading || !regUsername.trim() || !regPassword || !regConfirmPassword || !regInviteCode.trim()}
                data-visual="auth-modal-primary"
                className={primaryButtonClass}
              >
                {loading ? <Spinner className="size-4" /> : null}
                {t("auth.registerButton")}
              </Button>
              <p className="text-center text-name text-muted-foreground">
                {t("auth.haveAccount")}{" "}
                <Button type="button" variant="link" data-visual="auth-modal-inline-switch" onClick={() => { setTab("login"); setError(""); }} className={inlineAuthSwitchClass}>
                  {t("auth.login")}
                </Button>
              </p>
            </form>
          )}

          {/* ── Error ── */}
          {error && (
            <p className="mt-3 rounded-xl border border-destructive/20 bg-destructive/8 p-3 text-center text-name text-destructive" role="alert">
              {error}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
