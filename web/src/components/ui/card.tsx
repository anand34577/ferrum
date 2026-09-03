import type { HTMLAttributes } from "react"
import { cn } from "@/lib/utils"

const railTint: Record<string, string> = {
  ok: "color-mix(in oklab, var(--status-ok) 7%, var(--bg-surface))",
  warn: "color-mix(in oklab, var(--status-warn) 7%, var(--bg-surface))",
  error: "color-mix(in oklab, var(--status-error) 7%, var(--bg-surface))",
  brand: "color-mix(in oklab, var(--color-brand-500) 6%, var(--bg-surface))",
}

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  /** Tints the card's background with a faint status hue — pair with a
   * <StatusDot> in the header for an explicit, accessible signal. Omit for
   * a plain neutral card. */
  rail?: "ok" | "warn" | "error" | "brand"
  /** Lifts on hover — use for cards that are themselves a click target. */
  interactive?: boolean
}

export function Card({ className, rail, interactive, style, ...props }: CardProps) {
  return (
    <div
      className={cn(
        "rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] shadow-xs",
        interactive &&
          "transition-[transform,box-shadow,border-color] duration-150 hover:-translate-y-0.5 hover:border-[var(--border-strong)] hover:shadow-md",
        className,
      )}
      style={rail ? { background: railTint[rail], ...style } : style}
      {...props}
    />
  )
}

export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col gap-1 p-4 pb-2", className)} {...props} />
}

export function CardTitle({
  className,
  as: Heading = "h3",
  ...props
}: HTMLAttributes<HTMLHeadingElement> & { as?: "h1" | "h2" | "h3" | "h4" }) {
  return (
    <Heading
      className={cn("font-display text-sm font-semibold tracking-tight text-[var(--text)]", className)}
      {...props}
    />
  )
}

export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-xs text-[var(--text-muted)]", className)} {...props} />
}

export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-4 pt-2", className)} {...props} />
}
