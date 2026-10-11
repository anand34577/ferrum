import { ChevronRight, Plus } from "lucide-react"
import type { ReactNode } from "react"
import { Card, CardContent, CardTitle } from "@/components/ui/card"

/** A Card whose body is hidden behind its title until opened — for the
 * "create one of these" forms that sit above a list (backup jobs, HA
 * resources, pools, ...). Closed by default so the things that already
 * exist stay at the top of the page and the form is one click away, the
 * same way Alerts and Users reveal theirs from an "Add" button. Built on a
 * native <details> so open/closed state, keyboard toggling, and the
 * disclosure semantics come for free. */
export function CollapsibleCard({
  title,
  description,
  defaultOpen = false,
  className,
  children,
}: {
  title: ReactNode
  /** One line under the title, visible only while open. */
  description?: ReactNode
  defaultOpen?: boolean
  className?: string
  children: ReactNode
}) {
  return (
    <Card className={className}>
      <details open={defaultOpen} className="group/collapsible">
        <summary className="flex cursor-pointer list-none items-center gap-3 rounded-xl p-5 [&::-webkit-details-marker]:hidden hover:bg-[var(--bg-surface-hover)]/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--ring)]">
          <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-sm border border-[var(--border)] bg-[var(--bg-muted)] text-[var(--text-muted)]">
            <Plus className="h-3.5 w-3.5 group-open/collapsible:hidden" aria-hidden />
            <ChevronRight className="hidden h-3.5 w-3.5 rotate-90 group-open/collapsible:block" aria-hidden />
          </span>
          <CardTitle className="min-w-0 flex-1 truncate">{title}</CardTitle>
        </summary>
        {description && <p className="-mt-3 px-5 pb-2 text-xs leading-relaxed text-[var(--text-muted)]">{description}</p>}
        <CardContent className="pt-0">{children}</CardContent>
      </details>
    </Card>
  )
}
