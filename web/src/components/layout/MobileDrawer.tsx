import * as DialogPrimitive from "@radix-ui/react-dialog"
import { X } from "lucide-react"
import type { ReactNode } from "react"
import { BrandMark } from "./BrandMark"
import { SidebarContent } from "./SidebarContent"

/**
 * Mobile navigation drawer, built on Radix Dialog so it gets the a11y
 * machinery the old hand-rolled overlay lacked for free: focus trap,
 * aria-modal, Escape handling, scroll lock, and background inertness.
 */
export function MobileDrawer({ open, onOpenChange, children }: { open: boolean; onOpenChange: (open: boolean) => void; children: (close: () => void) => ReactNode }) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[200] bg-black/50" />
        <DialogPrimitive.Content
          aria-label="Navigation menu"
          className="fixed inset-y-0 left-0 z-[210] flex w-64 flex-col bg-[var(--sidebar-bg)] shadow-lg outline-none"
        >
          <DialogPrimitive.Title className="sr-only">Navigation menu</DialogPrimitive.Title>
          <div className="flex h-14 shrink-0 items-center justify-between border-b border-[var(--sidebar-border)] pl-4 pr-2">
            <div className="flex items-center gap-2.5">
              <BrandMark />
              <span className="font-display text-base font-semibold tracking-tight text-[var(--sidebar-text)]">Ferrum</span>
            </div>
            <DialogPrimitive.Close
              className="flex h-9 w-9 items-center justify-center rounded-md text-[var(--sidebar-text-muted)] hover:bg-[var(--sidebar-active-bg)] hover:text-[var(--sidebar-text)]"
              aria-label="Close menu"
            >
              <X className="h-4 w-4" />
            </DialogPrimitive.Close>
          </div>
          {children(() => onOpenChange(false))}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

// Re-exported so the drawer and the desktop rail share one nav definition.
export { SidebarContent }
