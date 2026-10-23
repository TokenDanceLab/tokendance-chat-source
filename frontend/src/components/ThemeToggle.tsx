import { Monitor, Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { Theme } from "@/lib/theme";

const icons = {
  dark: Moon,
  light: Sun,
  system: Monitor,
} satisfies Record<Theme, typeof Moon>;

export function ThemeToggle({ theme, onCycle }: { theme: Theme; onCycle: () => void }) {
  const Icon = icons[theme];

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={onCycle}
      className="size-9 rounded-full text-muted-foreground hover:text-foreground"
      aria-label={`Theme: ${theme}`}
      title={`Theme: ${theme}`}
    >
      <Icon className="size-4" />
    </Button>
  );
}
