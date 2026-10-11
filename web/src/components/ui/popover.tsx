import * as PopoverPrimitive from "@radix-ui/react-popover"
import type { ComponentProps } from "react"
import { cn } from "@/lib/utils"

export const Popover = PopoverPrimitive.Root
export const PopoverTrigger = PopoverPrimitive.Trigger
export const PopoverClose = PopoverPrimitive.Close

/** Free-form floating panel (date pickers, inline forms). Use DropdownMenu
 * for a list of actions and Select for choosing one value — this is for
 * content that is neither. Same surface and layer (300) as the dropdown. */
export function PopoverContent({ className, align = "start", sideOffset = 6, ...props }: ComponentProps<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          "z-[300] max-w-[calc(100vw-1rem)] rounded-lg border border-[var(--border)] bg-[var(--bg-elevated)] shadow-lg outline-none",
          "data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95",
          "data-[state=closed]:animate-out data-[state=closed]:fade-out data-[state=closed]:zoom-out-95",
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  )
}
