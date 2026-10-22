import { memo, useEffect, useMemo, useState } from "react";
import { Avatar as ShadcnAvatar, AvatarFallback } from "@/components/ui/avatar";
import { cn, avatarIdentityStyle, hashString } from "@/lib/utils";
import { useTranslation } from "@/i18n/context";

interface AvatarProps {
  src?: string | null;
  name: string;
  /** Canonical handle seeds identity; display-name changes do not alter it. */
  identityName?: string;
  size?: "sm" | "md" | "lg";
  online?: boolean;
  className?: string;
  onClick?: () => void;
}

const sizeClasses = {
  sm: "h-7 w-7 text-label",
  md: "h-8 w-8 text-xs",
  lg: "h-12 w-12 text-lg",
};

const onlineDotSizes = {
  sm: "h-2 w-2 -bottom-0.5 -right-0.5",
  md: "h-2.5 w-2.5 -bottom-0.5 -right-0.5",
  lg: "h-3 w-3 -bottom-0.5 -right-0.5",
};

export const Avatar = memo(function Avatar({
  src,
  name,
  identityName = name,
  size = "md",
  online,
  className,
  onClick,
}: AvatarProps) {
  const { t } = useTranslation();
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const identityStyle = useMemo(() => avatarIdentityStyle(identityName), [identityName]);
  const guest = /^guest_/i.test(identityName);
  const guestSeed = hashString(identityName);
  const initial = useMemo(() => (name ? name.charAt(0) : "?"), [name]);
  const imageSrc = src && src !== failedSrc ? src : null;

  useEffect(() => {
    setFailedSrc(null);
  }, [src]);

  const avatar = (
    <span
      className={cn(
        "relative inline-flex flex-shrink-0 items-center justify-center",
        onClick && "cursor-pointer rounded-full",
        className,
      )}
      aria-label={name}
    >
      <ShadcnAvatar className={cn("border border-border", sizeClasses[size])}>
        {imageSrc ? (
          <img
            src={imageSrc}
            alt={name}
            className="size-full object-cover"
            loading="lazy"
            decoding="async"
            referrerPolicy="no-referrer"
            onError={() => setFailedSrc(imageSrc)}
          />
        ) : null}
        {!imageSrc && <AvatarFallback
          className={cn("font-medium text-white", sizeClasses[size])}
          style={{ ...identityStyle, background: "var(--chat-identity-avatar)" }}
        >
          {guest ? (
            <svg data-visual="guest-avatar" data-avatar-seed={guestSeed} viewBox="0 0 32 32" className="size-full" aria-hidden="true">
              <rect width="32" height="32" fill="var(--chat-identity-avatar)" />
              <path d={guestSeed % 2 ? "M0 25 32 7v25H0Z" : "M0 8 32 25v7H0Z"} fill="currentColor" opacity="0.1" />
              <circle cx={14 + guestSeed % 5} cy="11" r={4 + guestSeed % 2} fill="currentColor" opacity="0.9" />
              <path d="M7 29v-4a9 9 0 0 1 18 0v4Z" fill="currentColor" opacity="0.9" />
            </svg>
          ) : initial.toUpperCase()}
        </AvatarFallback>}
      </ShadcnAvatar>
      {online ? (
        <span
          className={cn(
            "absolute rounded-full border-2 border-card bg-[var(--success)]",
            onlineDotSizes[size],
          )}
          role="status"
          aria-label={t("a11y.online")}
        />
      ) : null}
    </span>
  );

  if (!onClick) return avatar;

  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-full outline-none focus-visible:ring-3 focus-visible:ring-ring/30"
      aria-label={name}
    >
      {avatar}
    </button>
  );
});
