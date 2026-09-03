import type { LucideIcon } from "lucide-react"
import { NavLink, useLocation } from "react-router-dom"
import { cn } from "@/lib/utils"
import { Hint } from "@/components/ui/tooltip"

interface NavItemSpec {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  adminOnly?: boolean
}

/**
 * The nav item list shared by the desktop rail and the mobile drawer — one
 * definition so collapsed/expanded and desktop/mobile stay visually and
 * behaviorally identical instead of drifting into two hand-maintained copies.
 */
export function SidebarContent({
  groups,
  collapsed,
  onNavigate,
}: {
  groups: { label: string; items: NavItemSpec[] }[]
  collapsed: boolean
  /** Called after a nav link is activated — the mobile drawer uses this to close itself. */
  onNavigate?: () => void
}) {
  const { pathname } = useLocation()

  return (
    // When collapsed the rail is exactly one icon wide: hide the scrollbar
    // (wheel/scroll still work) so it can't push the icons off-center.
    <nav className={cn("flex-1 overflow-y-auto py-3", collapsed && "no-scrollbar")} aria-label="Main navigation">
      {groups.map((group) => (
        <div key={group.label} className={cn("mb-4 px-2.5 last:mb-0", collapsed && "px-2")}>
          {!collapsed && (
            <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-wider text-[var(--sidebar-text-muted)]">
              {group.label}
            </div>
          )}
          <ul className="space-y-0.5">
            {group.items.map((item) => {
              // Active state is computed here, not via NavLink's function
              // className: collapsed items render inside a Radix tooltip
              // (asChild), and its Slot stringifies a function className —
              // which produced a garbage class attribute (no justify-center,
              // both style branches applied at once) and knocked the icons
              // off-center. A plain string survives the merge.
              const active = item.end ? pathname === item.to : pathname.startsWith(item.to)
              // The active item picks up the account's accent color on its
              // icon instead of a flat highlight — the one place in the
              // chrome that visibly reflects the accent choice on every page,
              // since the sidebar itself stays a fixed dark rail.
              const linkClass = cn(
                "flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm font-medium transition-colors",
                collapsed && "justify-center",
                active
                  ? "bg-[var(--sidebar-active-bg)] text-[var(--sidebar-active-text)]"
                  : "text-[var(--sidebar-text-muted)] hover:bg-[var(--sidebar-active-bg)] hover:text-[var(--sidebar-text)]",
              )
              const link = (
                <NavLink
                  to={item.to}
                  end={item.end}
                  onClick={onNavigate}
                  className={linkClass}
                  aria-label={collapsed ? item.label : undefined}
                  aria-current={active ? "page" : undefined}
                >
                  <item.icon className={cn("h-4 w-4 shrink-0", active && "text-brand-400")} aria-hidden />
                  {!collapsed && <span className="truncate">{item.label}</span>}
                </NavLink>
              )
              return (
                <li key={item.to}>{collapsed ? <Hint label={item.label} side="right">{link}</Hint> : link}</li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}
