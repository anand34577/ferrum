import * as SwitchPrimitive from "@radix-ui/react-switch"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

// A panel toggle switch, not an iOS-style pill: a rectangular track with a
// squared thumb that slides between two engraved end-stops.
export function Switch({ className, ...props }: ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-sm border border-[var(--border-strong)] bg-[var(--bg-muted)] transition-colors",
        // Invisible hit-area expansion around the small track.
        "before:absolute before:-inset-2 before:content-['']",
        "data-[state=checked]:border-brand-700 data-[state=checked]:bg-brand-600",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb className="block h-3.5 w-3.5 translate-x-0.5 rounded-sm bg-white shadow-sm transition-transform data-[state=checked]:translate-x-4" />
    </SwitchPrimitive.Root>
  )
}
