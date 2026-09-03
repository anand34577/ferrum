import type { LucideIcon } from "lucide-react"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"
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
  className?: string
}

/** The one page header for the whole app: title, subtitle, actions. Every
 * authenticated page starts with this so hierarchy, spacing and responsive
 * wrapping are decided once, not sixteen times. */
export function PageHeader({ title, description, actions, icon: Icon, back, className }: PageHeaderProps) {
  return (
    <header className={cn("flex flex-wrap items-end justify-between gap-3", className)}>
      <div className="min-w-0">
        {back && (
          <Link
            to={back.to}
            className="mb-1 inline-flex items-center gap-1 text-xs font-medium text-[var(--text-muted)] transition-colors hover:text-[var(--text)]"
          >
            <span aria-hidden>←</span> {back.label}
          </Link>
        )}
        <h1 className="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-tight">
          {Icon && <Icon className="h-5.5 w-5.5 shrink-0 text-brand-500" aria-hidden />}
          <span className="break-words">{title}</span>
        </h1>
        {description && <p className="mt-0.5 text-sm text-[var(--text-muted)]">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}
