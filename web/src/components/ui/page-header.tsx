import type { LucideIcon } from "lucide-react"
import { RefreshCw } from "lucide-react"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Hint } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /** Right-aligned action cluster (buttons, selects, menus). */
  actions?: ReactNode
  /** Optional leading icon — rendered at title size, muted-brand. */
  icon?: LucideIcon
  /** Optional breadcrumb-style back link rendered above the title. */
  back?: { to: string; label: string }
  /** Shows a manual refresh button before the actions cluster. The handler
   * should re-fetch the page's queries; `refreshing` spins the icon while
   * the refetch is in flight. Polling cadences differ per page and several
   * pages don't poll at all, so every page offers this one honest "get the
   * latest data now" control. */
  onRefresh?: () => void
  refreshing?: boolean
  className?: string
}

/** The one page header for the whole app: title, subtitle, actions. Every
 * authenticated page starts with this so hierarchy, spacing and responsive
 * wrapping are decided once, not sixteen times. */
export function PageHeader({ title, description, actions, icon: Icon, back, onRefresh, refreshing, className }: PageHeaderProps) {
  return (
    <header className={cn("flex flex-wrap items-end justify-between gap-3 pb-2", className)}>
      <div className="min-w-0">
        {back && (
          <Link
            to={back.to}
            className="mb-2 inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)]"
          >
            <span aria-hidden className="text-[var(--text-faint)]">←</span> {back.label}
          </Link>
        )}
        <h1 className="panel-label flex items-center gap-3 text-2xl leading-tight tracking-tight text-[var(--text)]">
          {Icon && (
            <div className="corner-frame flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-brand-700 bg-[color-mix(in_oklab,var(--color-brand-500)_12%,transparent)] text-brand-500">
              <Icon className="h-5 w-5" aria-hidden />
            </div>
          )}
          <span className="break-words">{title}</span>
        </h1>
        {description && <p className="mt-2 max-w-2xl text-xs leading-relaxed text-[var(--text-muted)]">{description}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {onRefresh && (
          <Hint label="Refresh now">
            <Button
              size="icon-sm"
              variant="ghost"
              onClick={onRefresh}
              disabled={refreshing}
              aria-busy={refreshing || undefined}
              aria-label="Refresh now"
            >
              <RefreshCw className={cn("h-4 w-4", refreshing && "animate-spin")} />
            </Button>
          </Hint>
        )}
        {actions}
      </div>
    </header>
  )
}
