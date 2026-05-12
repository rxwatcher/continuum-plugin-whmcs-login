import type { CSSProperties } from "react";
import { Toaster as SonnerToaster, type ToasterProps } from "sonner";
import { getCachedTheme } from "@/lib/auth";

const LIGHT_THEMES = new Set(["cinema-light"]);

const Toaster = ({ ...props }: ToasterProps) => {
  const theme = getCachedTheme() ?? "";
  const sonnerTheme = LIGHT_THEMES.has(theme) ? "light" : "dark";

  return (
    <SonnerToaster
      theme={sonnerTheme}
      className="toaster group"
      toastOptions={{
        classNames: {
          toast: "!text-[var(--popover-foreground)]",
          title: "!text-[var(--popover-foreground)]",
          description: "!text-[var(--popover-foreground)]",
        },
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as CSSProperties
      }
      {...props}
    />
  );
};

export { Toaster };
