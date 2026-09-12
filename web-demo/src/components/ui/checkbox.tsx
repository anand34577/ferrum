import * as CheckboxPrimitive from "@radix-ui/react-checkbox"
import { Check } from "lucide-react"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export function Checkbox({ className, ...props }: ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      className={cn(
        "relative flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border border-[var(--border-strong)] bg-[var(--bg-surface)] transition-colors",
        // Invisible hit-area expansion: the 16px visual stays, but the
        // clickable/pressable region meets touch-target minimums.
        "before:absolute before:-inset-2 before:content-['']",
        "hover:border-brand-500 data-[state=checked]:border-brand-600 data-[state=checked]:bg-brand-600 data-[state=indeterminate]:border-brand-600 data-[state=indeterminate]:bg-brand-600",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator className="text-white">
        {props.checked === "indeterminate" ? (
          <span className="h-0.5 w-2 rounded-full bg-white" />
        ) : (
          <Check className="h-3 w-3" strokeWidth={3} />
        )}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}
