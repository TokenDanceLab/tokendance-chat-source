import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";
import type { CSSProperties } from "react";

// Match the custom --text-* sizes in index.css so they never replace text colors.
const twMerge = extendTailwindMerge({
  extend: { theme: { text: ["micro", "label", "name"] } },
});

export type TFunction = (key: string, params?: Record<string, string | number>) => string;

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatTime(timestamp: number, t: TFunction): string {
  const date = new Date(timestamp);
  const now = Date.now();
  const diffMs = now - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);

  // Less than 1 minute
  if (diffSec < 60) return t("profile.justNow");

  // Less than 1 hour
  if (diffMin < 60) return t("profile.minutesAgo", { n: String(diffMin) });

  // Less than 4 hours: "HH:mm"
  if (diffHour < 4) {
    return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  // Different year: "YY-MM-DD"
  if (date.getFullYear() !== new Date().getFullYear()) {
    const yy = date.getFullYear().toString().slice(-2);
    return `${yy}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  }

  // Same year, older: "MM-DD"
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function formatFullTime(timestamp: number): string {
  const date = new Date(timestamp);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function formatDate(timestamp: number, t: TFunction): string {
  const date = new Date(timestamp);
  const today = new Date();
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);

  if (date.toDateString() === today.toDateString()) {
    return t("profile.today");
  }
  if (date.toDateString() === yesterday.toDateString()) {
    return t("profile.yesterday");
  }
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function pad(n: number): string {
  return n.toString().padStart(2, "0");
}

/**
 * Consistent string hashing used for avatar colors across all components.
 */
export function hashString(str: string): number {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  return Math.abs(hash);
}

export function avatarIdentityStyle(username: string): CSSProperties {
  const hue = hashString(username) % 360;
  return {
    "--chat-identity-hue": `${hue}`,
    "--chat-identity-avatar": `hsl(${hue} 22% 42%)`,
  } as CSSProperties;
}

/** JS scrolling cannot be disabled by the CSS reduced-motion rule. */
export function scrollBehavior(): ScrollBehavior {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return "smooth";
  }
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth";
}

/** Single-line, Markdown-stripped text for reply chips and run previews. */
export function plainTextPreview(content: string, max = 80): string {
  const text = content
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[#>*_~|]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
}
