import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
import { ChevronRight } from "lucide-react"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const DropdownMenu = DropdownMenuPrimitive.Root
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger
export const DropdownMenuSub = DropdownMenuPrimitive.Sub

export function DropdownMenuSubTrigger({ className, children, ...props }: ComponentProps<typeof DropdownMenuPrimitive.SubTrigger>) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      className={cn(
        "flex cursor-pointer select-none items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm outline-none transition-colors",
        "data-[highlighted]:bg-[var(--bg-muted)] data-[state=open]:bg-[var(--bg-muted)]",
        className,
      )}
      {...props}
    >
      {children}
      <ChevronRight className="ml-auto h-3.5 w-3.5 text-[var(--text-muted)]" />
    </DropdownMenuPrimitive.SubTrigger>
  )
}

export function DropdownMenuSubContent({ className, ...props }: ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.SubContent
        sideOffset={4}
        className={cn(
          "z-[300] min-w-40 overflow-hidden rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] p-1 shadow-md",
          className,
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

export function DropdownMenuContent({ className, sideOffset = 6, children, ...props }: ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "z-[300] min-w-40 overflow-hidden rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] p-1 shadow-md",
          "data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95",
          className,
        )}
        {...props}
      >
        {children}
      </DropdownMenuPrimitive.Content>
    </DropdownMenuPrimitive.Portal>
  )
}

export function DropdownMenuItem({ className, ...props }: ComponentProps<typeof DropdownMenuPrimitive.Item>) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(
        "flex cursor-pointer select-none items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm outline-none transition-colors",
        "data-[highlighted]:bg-[var(--bg-muted)] data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
        className,
      )}
      {...props}
    />
  )
}

export function DropdownMenuLabel({ className, ...props }: ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn("px-2.5 py-1.5 font-mono text-[10px] font-medium uppercase tracking-wider text-[var(--text-faint)]", className)}
      {...props}
    />
  )
}

export function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return <DropdownMenuPrimitive.Separator className={cn("-mx-1 my-1 h-px bg-[var(--border)]", className)} {...props} />
}
