import type { LucideIcon } from "lucide-react"
import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

interface EmptyStateProps {
  icon?: LucideIcon
  /** One line, sentence case — what is (not) here. */
  title: string
  /** Why it's empty and/or what to do next. Optional. */
  description?: ReactNode
  /** Primary call-to-action, usually a Button that creates the first item. */
  action?: ReactNode
  className?: string
}

/** The one empty-state treatment: centered icon, title, hint, action. Use it
 * anywhere a list, table or grid can be legitimately empty. */
export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center rounded-lg border border-dashed border-[var(--border)] px-6 py-12 text-center",
        className,
      )}
    >
      {Icon && (
        <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-[var(--bg-muted)]">
          <Icon className="h-5 w-5 text-[var(--text-faint)]" aria-hidden />
        </div>
      )}
      <p className="text-sm font-medium">{title}</p>
      {description && <p className="mt-1 max-w-sm text-xs leading-relaxed text-[var(--text-muted)]">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}
