import { AlertTriangle, RotateCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface ErrorStateProps {
  title?: string
  message?: React.ReactNode
  /** When provided, renders a "Try again" button wired to the caller's retry. */
  onRetry?: () => void
  className?: string
}

/** For failed queries/loads: distinct from empty (something broke) with an
 * explicit recovery action. */
export function ErrorState({ title = "Couldn't load this content", message, onRetry, className }: ErrorStateProps) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-center justify-center rounded-lg border border-[color-mix(in_oklab,var(--status-error)_30%,var(--border))] px-6 py-10 text-center",
        className,
      )}
    >
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)]">
        <AlertTriangle className="h-5 w-5 text-[var(--status-error)]" aria-hidden />
      </div>
      <p className="text-sm font-medium">{title}</p>
      {message && <p className="mt-1 max-w-sm text-xs leading-relaxed text-[var(--text-muted)]">{message}</p>}
      {onRetry && (
        <Button variant="secondary" size="sm" className="mt-4" onClick={onRetry}>
          <RotateCw className="h-3.5 w-3.5" /> Try again
        </Button>
      )}
    </div>
  )
}
