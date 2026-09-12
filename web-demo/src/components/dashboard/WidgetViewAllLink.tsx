import { Link } from "react-router-dom"

/** The "top N of everything" widgets (leaderboards, comparisons) cut their
 * list before rendering — this is the one place that admits it, the same way
 * NotificationBell does for alerts: only renders once something was actually
 * cut, and always says where the rest lives instead of just disappearing. */
export function WidgetViewAllLink({ to, shown, total }: { to: string; shown: number; total: number }) {
  if (total <= shown) return null
  return (
    <Link
      to={to}
      className="mt-1.5 block shrink-0 text-center text-[11px] font-medium text-brand-600 hover:underline dark:text-brand-400"
    >
      View all {total} &rarr;
    </Link>
  )
}
