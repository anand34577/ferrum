import { cva, type VariantProps } from "class-variance-authority"
import { Loader2 } from "lucide-react"
import { type ButtonHTMLAttributes, forwardRef } from "react"
import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-[color,background-color,border-color,box-shadow,transform] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] disabled:pointer-events-none disabled:opacity-50 active:scale-[0.98]",
  {
    variants: {
      variant: {
        default: "bg-brand-600 text-white shadow-xs hover:bg-brand-700",
        secondary: "border border-[var(--border)] bg-[var(--bg-surface)] text-[var(--text)] shadow-xs hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)]",
        ghost: "text-[var(--text-muted)] hover:bg-[var(--bg-muted)] hover:text-[var(--text)]",
        destructive: "bg-[var(--status-error)] text-white shadow-xs hover:brightness-90",
        outline: "border border-[var(--border)] bg-transparent text-[var(--text)] hover:bg-[var(--bg-muted)]",
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
