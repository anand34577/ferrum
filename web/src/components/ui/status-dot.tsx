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
  pulse = false,
  className,
}: {
  status: "ok" | "warn" | "error" | "brand" | "muted"
  pulse?: boolean
  className?: string
}) {
  const color = dotColor[status]

  return (
    <span className={cn("relative inline-flex h-2.5 w-2.5 shrink-0 items-center justify-center", className)} aria-hidden="true">
      {pulse && status !== "muted" && (
        <span
          className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60"
          style={{ background: color }}
        />
      )}
      <span
        className="relative inline-block h-2 w-2 rounded-full ring-2 ring-[var(--bg-surface)]"
        style={{
          background: color,
          boxShadow: status !== "muted" ? `0 0 8px ${color}` : undefined,
        }}
      />
    </span>
  )
}
