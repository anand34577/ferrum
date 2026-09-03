import { cva, type VariantProps } from "class-variance-authority"
import type { HTMLAttributes } from "react"
import { cn } from "@/lib/utils"

// Annunciator chip, not a pill: a rectangular bordered tag with tracked caps,
// the way a caution/status placard reads on an instrument panel.
const badgeVariants = cva(
  "panel-label inline-flex items-center gap-1.5 rounded-sm border px-2 py-0.5 text-[10px] tracking-wider whitespace-nowrap transition-colors select-none",
  {
    variants: {
      variant: {
        default: "border border-[var(--border)] bg-[var(--bg-muted)] text-[var(--text-muted)]",
        ok: "border border-[color-mix(in_oklab,var(--status-ok)_28%,transparent)] bg-[color-mix(in_oklab,var(--status-ok)_10%,transparent)] text-[var(--status-ok)]",
        warn: "border border-[color-mix(in_oklab,var(--status-warn)_28%,transparent)] bg-[color-mix(in_oklab,var(--status-warn)_10%,transparent)] text-[var(--status-warn)]",
        error: "border border-[color-mix(in_oklab,var(--status-error)_28%,transparent)] bg-[color-mix(in_oklab,var(--status-error)_10%,transparent)] text-[var(--status-error)]",
        info: "border border-[color-mix(in_oklab,var(--status-info)_28%,transparent)] bg-[color-mix(in_oklab,var(--status-info)_10%,transparent)] text-[var(--status-info)]",
        brand: "border border-[color-mix(in_oklab,var(--color-brand-500)_28%,transparent)] bg-[color-mix(in_oklab,var(--color-brand-500)_10%,transparent)] text-brand-600 dark:text-brand-400",
        outline: "border border-[var(--border)] bg-transparent text-[var(--text-muted)]",
      },
    },
    defaultVariants: { variant: "default" },
  },
)

const dotColor: Record<string, string> = {
  default: "var(--text-faint)",
  ok: "var(--status-ok)",
  warn: "var(--status-warn)",
  error: "var(--status-error)",
  info: "var(--status-info)",
  brand: "var(--color-brand-500)",
  outline: "var(--text-faint)",
}

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {
  dot?: boolean
}

export function Badge({ className, variant = "default", dot, children, ...props }: BadgeProps) {
  const v = variant ?? "default"
  return (
    <span className={cn(badgeVariants({ variant }), className)} {...props}>
      {dot && (
        <span
          className="h-1.5 w-1.5 shrink-0 rounded-full"
          style={{ background: dotColor[v] }}
          aria-hidden="true"
        />
      )}
      {children}
    </span>
  )
}
