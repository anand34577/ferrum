import { cva, type VariantProps } from "class-variance-authority"
import { Loader2 } from "lucide-react"
import { type ButtonHTMLAttributes, forwardRef } from "react"
import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "inline-flex cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-[color,background-color,border-color,box-shadow,transform] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] disabled:pointer-events-none disabled:opacity-50 active:scale-[0.98] select-none",
  {
    variants: {
      variant: {
        // A flat annunciator switch, not a glossy gradient pill: solid panel
        // color, one hairline top highlight (the physical bezel edge), state
        // change is a brightness step — never a two-tone gradient sweep.
        default:
          "border border-brand-700 bg-brand-600 text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.16)] hover:bg-brand-700 active:bg-brand-800",
        secondary:
          "border border-[var(--border)] bg-[var(--bg-surface)] text-[var(--text)] hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text)] active:bg-[var(--bg-muted)]",
        ghost:
          "text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text)] active:bg-[var(--bg-muted)]",
        // A ghost button reserved for destructive row actions (delete/remove).
        // The color only appears on hover/press so a toolbar of these still
        // reads calm — same treatment the hand-rolled className copies of
        // this pattern used across pages, centralized here so they can't drift.
        "ghost-danger":
          "text-[var(--text-muted)] hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)] active:bg-[color-mix(in_oklab,var(--status-error)_20%,transparent)]",
        destructive:
          "border border-[color-mix(in_oklab,var(--status-error)_60%,black)] bg-[var(--status-error)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.14)] hover:brightness-110 active:brightness-95",
        outline:
          "border border-[var(--border)] bg-transparent text-[var(--text)] hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)] active:bg-[var(--bg-muted)]",
      },
      size: {
        default: "h-9 px-4",
        sm: "h-8 gap-1.5 px-3 text-xs",
        lg: "h-10 px-6",
        // Visual size stays small (36/32px) but the hit area is expanded to the
        // 44px touch-target minimum via an invisible ::before, same pattern as
        // Checkbox/Switch — nothing to click, just a bigger place to click it.
        icon: "relative h-9 w-9 before:absolute before:-inset-1 before:content-['']",
        "icon-sm": "relative h-8 w-8 before:absolute before:-inset-1.5 before:content-['']",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
)

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  /** Shows a spinner and blocks interaction while the pending work runs. */
  loading?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, loading, disabled, children, ...props }, ref) => (
    <button
      ref={ref}
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />}
      {children}
    </button>
  ),
)
Button.displayName = "Button"
