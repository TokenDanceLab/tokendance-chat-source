import { memo, useEffect, useMemo, useState } from "react";
import { Avatar as ShadcnAvatar, AvatarFallback } from "@/components/ui/avatar";
import { cn, avatarIdentityStyle } from "@/lib/utils";
import { useTranslation } from "@/i18n/context";

interface AvatarProps {
  src?: string | null;
  name: string;
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
  size = "md",
  online,
  className,
  onClick,
}: AvatarProps) {
  const { t } = useTranslation();
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const identityStyle = useMemo(() => avatarIdentityStyle(name), [name]);
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
        <AvatarFallback
          className={cn("font-medium text-white", sizeClasses[size])}
          style={{ ...identityStyle, background: "var(--chat-identity-avatar)" }}
        >
          {initial.toUpperCase()}
        </AvatarFallback>
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
