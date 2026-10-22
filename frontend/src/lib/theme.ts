import { STORAGE_KEYS } from "./storage";

export type Theme = "dark" | "light" | "system";

export const THEME_STORAGE_KEY = STORAGE_KEYS.theme;

export function getStoredTheme(): Theme {
  const stored = localStorage.getItem(THEME_STORAGE_KEY);
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "light";
}

export function applyTheme(theme: Theme) {
  const root = document.documentElement;
  const prefersDark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  root.classList.toggle("dark", prefersDark);
  root.classList.toggle("light", !prefersDark);
}

export function nextTheme(theme: Theme): Theme {
  return theme === "light" ? "dark" : theme === "dark" ? "system" : "light";
}
