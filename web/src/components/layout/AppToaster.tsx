import type { CSSProperties } from "react"
import { Toaster } from "sonner"
import { useTheme } from "@/lib/theme"

/**
 * Sonner ships its own hardcoded light/dark palette (`richColors`) — with
 * zero integration it renders correctly in neither our light/dark toggle nor
 * any of the twelve looks, so a toast could pop up in Sonner's stock white
 * card while the rest of the app is, say, Brutalist-dark. Passing `theme`
 * fixes the light/dark half; the CSS custom properties below repoint every
 * toast color and the corner radius at our own tokens so it always matches
 * the active look and accent, not just light vs. dark.
 */
export function AppToaster() {
  const { effectiveTheme } = useTheme()
  return (
    <Toaster
      theme={effectiveTheme}
      position="top-right"
      toastOptions={{ className: "font-sans" }}
      style={
        {
          "--normal-bg": "var(--bg-elevated)",
          "--normal-border": "var(--border)",
          "--normal-text": "var(--text)",
          "--success-bg": "color-mix(in oklab, var(--status-ok) 10%, var(--bg-elevated))",
          "--success-border": "color-mix(in oklab, var(--status-ok) 35%, var(--border))",
          "--success-text": "var(--status-ok)",
          "--error-bg": "color-mix(in oklab, var(--status-error) 10%, var(--bg-elevated))",
          "--error-border": "color-mix(in oklab, var(--status-error) 35%, var(--border))",
          "--error-text": "var(--status-error)",
          "--warning-bg": "color-mix(in oklab, var(--status-warn) 10%, var(--bg-elevated))",
          "--warning-border": "color-mix(in oklab, var(--status-warn) 35%, var(--border))",
          "--warning-text": "var(--status-warn)",
          "--info-bg": "color-mix(in oklab, var(--status-info) 10%, var(--bg-elevated))",
          "--info-border": "color-mix(in oklab, var(--status-info) 35%, var(--border))",
          "--info-text": "var(--status-info)",
          "--border-radius": "var(--radius-lg)",
        } as CSSProperties
      }
    />
  )
}
