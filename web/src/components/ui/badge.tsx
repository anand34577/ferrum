import { cva, type VariantProps } from "class-variance-authority"
import type { HTMLAttributes } from "react"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap",
  {
    variants: {
      variant: {
        default: "border border-[var(--border)] bg-[var(--bg-muted)] text-[var(--text)]",
        ok: "bg-[color-mix(in_oklab,var(--status-ok)_15%,transparent)] text-[var(--status-ok)]",
        warn: "bg-[color-mix(in_oklab,var(--status-warn)_15%,transparent)] text-[var(--status-warn)]",
        error: "bg-[color-mix(in_oklab,var(--status-error)_15%,transparent)] text-[var(--status-error)]",
        info: "bg-[color-mix(in_oklab,var(--status-info)_15%,transparent)] text-[var(--status-info)]",
        brand: "bg-[color-mix(in_oklab,var(--color-brand-500)_15%,transparent)] text-[var(--color-brand-600)] dark:text-[var(--color-brand-400)]",
      },
    },
    defaultVariants: { variant: "default" },
  },
)

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}
