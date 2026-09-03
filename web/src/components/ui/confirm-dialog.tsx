import * as DialogPrimitive from "@radix-ui/react-dialog"
import { AlertTriangle } from "lucide-react"
import { createContext, type ReactNode, useCallback, useContext, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export interface ConfirmOptions {
  title: string
  description?: ReactNode
  /** Label for the confirming action. Default "Confirm". */
  confirmLabel?: string
  cancelLabel?: string
  /** Renders the confirm button in the destructive style. Default true —
   * most confirms in this app guard irreversible actions. */
  destructive?: boolean
}

type ConfirmFn = (options: ConfirmOptions) => Promise<boolean>

const ConfirmContext = createContext<ConfirmFn | null>(null)

/** App-wide replacement for `window.confirm`: `if (await confirm({ title: "Delete connection?" })) …`.
 * Focus lands on Cancel (the safe choice), Escape/overlay click also cancel. */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const [options, setOptions] = useState<ConfirmOptions | null>(null)
  const resolveRef = useRef<((ok: boolean) => void) | null>(null)

  const confirm = useCallback<ConfirmFn>((opts) => {
    setOptions(opts)
    setOpen(true)
    return new Promise<boolean>((resolve) => {
      resolveRef.current?.(false) // an interrupted previous confirm counts as cancelled
      resolveRef.current = resolve
    })
  }, [])

  const settle = useCallback((ok: boolean) => {
    setOpen(false)
    resolveRef.current?.(ok)
    resolveRef.current = null
  }, [])

  const destructive = options?.destructive ?? true

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <DialogPrimitive.Root open={open} onOpenChange={(next) => !next && settle(false)}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/50" />
          <DialogPrimitive.Content
            className={cn(
              "fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-6 shadow-lg",
              "data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95",
            )}
          >
            <div className="flex gap-3.5">
              {destructive && (
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)]">
                  <AlertTriangle className="h-4.5 w-4.5 text-[var(--status-error)]" aria-hidden />
                </div>
              )}
              <div className="min-w-0 flex-1">
                <DialogPrimitive.Title className="font-display text-base font-semibold">
                  {options?.title}
                </DialogPrimitive.Title>
                {options?.description && (
                  <DialogPrimitive.Description className="mt-1 text-sm leading-relaxed text-[var(--text-muted)]">
                    {options.description}
                  </DialogPrimitive.Description>
                )}
              </div>
            </div>
            <div className="mt-5 flex justify-end gap-2">
              <DialogPrimitive.Close asChild>
                <Button variant="secondary" autoFocus onClick={() => settle(false)}>
                  {options?.cancelLabel ?? "Cancel"}
                </Button>
              </DialogPrimitive.Close>
              <Button
                variant={destructive ? "destructive" : "default"}
                onClick={() => settle(true)}
                data-testid="confirm-accept"
              >
                {options?.confirmLabel ?? "Confirm"}
              </Button>
            </div>
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </ConfirmContext.Provider>
  )
}

export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext)
  if (!ctx) throw new Error("useConfirm must be used within ConfirmProvider")
  return ctx
}
