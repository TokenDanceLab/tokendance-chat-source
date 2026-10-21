import { useTranslation } from "@/i18n/context";
import { Button } from "@/components/ui/button";

/** Starts the server-side Account BFF flow. */
export function OidcLoginButton({ primary = false }: { primary?: boolean }) {
  const { t } = useTranslation();

  return (
    <Button
      asChild
      variant={primary ? "default" : "outline"}
      className="min-h-11 h-auto w-full gap-2 whitespace-normal rounded-xl py-2 text-name"
    >
      <a href="/api/oidc/login" data-visual="auth-modal-oidc">
        <span
          aria-hidden="true"
          data-brand="tokendance-account"
          className="size-[18px] shrink-0 bg-current"
          style={{
            // Canonical family silhouette; 120px artwork in its 160px canvas.
            // The foreground follows the button across variants and themes.
            mask: "url('/brand/tokendance-mark-white-transparent.svg') center / 24px 24px no-repeat",
          }}
        />
        <span className="min-w-0">{t("auth.oidcLoginButton")}</span>
      </a>
    </Button>
  );
}
