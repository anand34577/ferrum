import { type TextareaHTMLAttributes, forwardRef } from "react"
import { cn } from "@/lib/utils"

/** Multi-line counterpart to Input — same border/surface/focus/disabled/
 * invalid language so a form mixing single- and multi-line fields reads as
 * one system. Defaults to the body font (prose fields: notes, descriptions);
 * pass `font-mono text-xs` at the call site for config/key-value textareas,
 * same as Input's own mono-by-default note documents the opposite split. */
export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, rows = 3, ...props }, ref) => (
    <textarea
      ref={ref}
      rows={rows}
      className={cn(
        "flex w-full resize-y rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 text-sm text-[var(--text)] transition-all duration-150",
        "placeholder:text-[var(--text-faint)] hover:border-[var(--border-strong)]",
        "focus-visible:outline-none focus-visible:border-[var(--ring)] focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_oklab,var(--ring)_25%,transparent)]",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-[invalid=true]:border-[var(--status-error)] aria-[invalid=true]:focus-visible:border-[var(--status-error)] aria-[invalid=true]:focus-visible:ring-[color-mix(in_oklab,var(--status-error)_25%,transparent)]",
        className,
      )}
      {...props}
    />
  ),
)
Textarea.displayName = "Textarea"
