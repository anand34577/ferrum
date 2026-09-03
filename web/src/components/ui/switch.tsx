import * as SwitchPrimitive from "@radix-ui/react-switch"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export function Switch({ className, ...props }: ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full bg-[var(--border-strong)] transition-colors",
        // Invisible hit-area expansion around the small track.
        "before:absolute before:-inset-2 before:content-['']",
        "data-[state=checked]:bg-brand-600",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb className="block h-4 w-4 translate-x-0.5 rounded-full bg-white shadow-sm transition-transform data-[state=checked]:translate-x-4" />
    </SwitchPrimitive.Root>
  )
}
