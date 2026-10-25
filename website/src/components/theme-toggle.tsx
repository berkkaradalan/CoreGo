"use client";

import { useTheme } from "fumadocs-ui/provider/base";
import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * Switches between dark and light mode. Both icons are rendered and CSS
 * shows the right one, so server and client markup always match.
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <Button
      variant="ghost"
      size="icon"
      className={className}
      onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
      aria-label="Toggle dark mode"
    >
      <Sun className="hidden dark:block" />
      <Moon className="dark:hidden" />
    </Button>
  );
}
