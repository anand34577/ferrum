import * as TabsPrimitive from "@radix-ui/react-tabs"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const Tabs = TabsPrimitive.Root

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn("inline-flex max-w-full items-center gap-1 overflow-x-auto rounded-md bg-[var(--bg-muted)] p-1", className)}
      {...props}
    />
  )
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "shrink-0 rounded-sm px-3 py-1.5 text-sm font-medium text-[var(--text-muted)] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
        "hover:text-[var(--text)] data-[state=active]:bg-[var(--bg-surface)] data-[state=active]:text-[var(--text)] data-[state=active]:shadow-xs",
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
