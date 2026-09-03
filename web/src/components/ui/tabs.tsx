import * as TabsPrimitive from "@radix-ui/react-tabs"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const Tabs = TabsPrimitive.Root

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn(
        "inline-flex max-w-full items-center gap-1 overflow-x-auto rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/60 p-1 backdrop-blur-xs",
        className,
      )}
      {...props}
    />
  )
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "panel-label shrink-0 rounded-sm px-3 py-1.5 text-[11px] text-[var(--text-muted)] transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
        "hover:text-[var(--text)] data-[state=active]:border data-[state=active]:border-[var(--border)] data-[state=active]:bg-[var(--bg-surface)] data-[state=active]:text-[var(--text)]",
        className,
      )}
      {...props}
    />
  )
}

export function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      className={cn("mt-4 focus-visible:outline-none", className)}
      {...props}
    />
  )
}
