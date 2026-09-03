import { cn } from "@/lib/utils"

const dotColor: Record<string, string> = {
  ok: "var(--status-ok)",
  warn: "var(--status-warn)",
  error: "var(--status-error)",
  brand: "var(--color-brand-500)",
  muted: "var(--text-faint)",
}

/** A small solid-color dot for at-a-glance status — a modern, borderless
 * alternative to a colored rule/stripe. Pair with a text label; the dot
 * alone isn't accessible. */
export function StatusDot({
  status,
  className,
}: {
  status: "ok" | "warn" | "error" | "brand" | "muted"
  className?: string
}) {
  return (
    <span
      className={cn("inline-block h-2 w-2 shrink-0 rounded-full", className)}
      style={{ background: dotColor[status] }}
      aria-hidden="true"
    />
  )
}
