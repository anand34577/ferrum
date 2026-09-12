import { AlertTriangle } from "lucide-react"

/** Persistent inline error for a form submission — unlike a toast, it stays
 * visible until the next attempt or a successful save. The backend reports
 * one message per failure (not per-field), so this is the granularity
 * available; it at least survives longer than a toast that auto-dismisses
 * before a slower reader finishes it. */
export function FormError({ message }: { message?: string | null }) {
  if (!message) return null
  return (
    <div role="alert" className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--status-error)_30%,var(--border))] bg-[color-mix(in_oklab,var(--status-error)_8%,transparent)] px-3 py-2.5 text-sm text-[var(--status-error)]">
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
      <span>{message}</span>
    </div>
  )
}
