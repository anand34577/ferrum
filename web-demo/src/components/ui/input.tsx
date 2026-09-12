import { type InputHTMLAttributes, forwardRef } from "react"
import { cn } from "@/lib/utils"

/** Every text input sets in mono deliberately — part of the app's technical-
 * instrument register (tracked-caps badges, mono metrics elsewhere). Prose
 * fields (Description/Notes) opt back into the body font at the call site
 * the same way DatacenterOptionsForm's textarea already does. */
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        "flex h-9 w-full rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 font-mono text-sm text-[var(--text)] transition-all duration-150",
        "placeholder:text-[var(--text-faint)] hover:border-[var(--border-strong)]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:border-transparent",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-[invalid=true]:border-[var(--status-error)] aria-[invalid=true]:focus-visible:ring-[var(--status-error)]",
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = "Input"
