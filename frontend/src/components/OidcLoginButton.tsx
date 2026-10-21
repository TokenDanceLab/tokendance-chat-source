import { useTranslation } from "@/i18n/context";
import { Button } from "@/components/ui/button";

/** Starts the server-side Account BFF flow. */
export function OidcLoginButton({ primary = false }: { primary?: boolean }) {
  const { t } = useTranslation();

  return (
    <Button
      asChild
      variant={primary ? "default" : "outline"}
      className="min-h-11 h-11 w-full rounded-xl text-name"
    >
      <a href="/api/oidc/login" data-visual="auth-modal-oidc">
        {t("auth.oidcLoginButton")}
      </a>
    </Button>
  );
}
