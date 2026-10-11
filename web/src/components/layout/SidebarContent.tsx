import { ChevronDown, type LucideIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { NavLink, useLocation } from "react-router-dom"
import { cn } from "@/lib/utils"
import { Hint } from "@/components/ui/tooltip"
import { prefetchRoute } from "@/lib/routes"

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
  const navRef = useRef<HTMLElement>(null)
  // Which edges have more items scrolled out of view. On a laptop-height
  // window the last section (Administration) sits below the fold with
  // nothing to say so — the thin scrollbar is easy to miss on a dark rail,
  // and it is hidden outright when the rail is collapsed.
  const [overflow, setOverflow] = useState({ above: false, below: false })

  useEffect(() => {
    const nav = navRef.current
    if (!nav) return
    const measure = () => {
      const above = nav.scrollTop > 4
      const below = nav.scrollTop + nav.clientHeight < nav.scrollHeight - 4
      setOverflow((prev) => (prev.above === above && prev.below === below ? prev : { above, below }))
    }
    measure()
    nav.addEventListener("scroll", measure, { passive: true })
    // Fires for window resizes and for the rail collapsing/expanding alike.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure)
    observer?.observe(nav)
    return () => {
      nav.removeEventListener("scroll", measure)
      observer?.disconnect()
    }
  }, [groups, collapsed])

  // Arriving on a page whose nav item is scrolled out of view (a deep link
  // to Settings, the command palette) brings that item into view, so the
  // rail always shows where you are.
  useEffect(() => {
    navRef.current?.querySelector('[aria-current="page"]')?.scrollIntoView?.({ block: "nearest" })
  }, [pathname])

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
    {/* When collapsed the rail is exactly one icon wide: hide the scrollbar
        (wheel/scroll still work) so it can't push the icons off-center. */}
    <nav ref={navRef} className={cn("flex-1 scroll-py-12 overflow-y-auto py-3", collapsed && "no-scrollbar")} aria-label="Main navigation">
      {groups.map((group) => (
        <div key={group.label} className={cn("mb-4 px-2.5 last:mb-0", collapsed && "px-2")}>
          {/* Full-opacity muted text: at 70% these 10px labels fell to ~3.7:1 on the rail. */}
          {!collapsed && (
            <div className="panel-label mb-1.5 px-2.5 text-[10px] tracking-[0.1em] text-[var(--sidebar-text-muted)]">
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
              // Node detail pages live under /nodes but belong to Inventory —
              // without this, no nav item is lit while on a host page.
              const active = item.end
                ? pathname === item.to
                : pathname.startsWith(item.to) || (item.to === "/inventory" && pathname.startsWith("/nodes/"))
              // The active item picks up the account's accent color on its
              // icon instead of a flat highlight — the one place in the
              // chrome that visibly reflects the accent choice on every page,
              // since the sidebar itself stays a fixed dark rail.
              const linkClass = cn(
                "nav-item group relative flex items-center gap-2.5 rounded-md px-2.5 py-[7px] font-display text-sm font-medium transition-all duration-150 select-none",
                collapsed && "justify-center px-2",
                active
                  ? "nav-item-active bg-[var(--sidebar-active-bg)] text-[var(--sidebar-active-text)]"
                  : "text-[var(--sidebar-text-muted)] hover:bg-[var(--sidebar-active-bg)] hover:text-[var(--sidebar-text)]",
              )
              const link = (
                <NavLink
                  to={item.to}
                  end={item.end}
                  onClick={onNavigate}
                  onPointerEnter={() => prefetchRoute(item.to)}
                  onFocus={() => prefetchRoute(item.to)}
                  className={linkClass}
                  aria-label={collapsed ? item.label : undefined}
                  aria-current={active ? "page" : undefined}
                >
                  <item.icon
                    className={cn(
                      "h-4 w-4 shrink-0 transition-all duration-150 group-hover:scale-110",
                      active ? "text-brand-400" : "text-[var(--sidebar-text-muted)] group-hover:text-[var(--sidebar-text)]",
                    )}
                    aria-hidden
                  />
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
    {/* Fades the edge that has more behind it — the standard "this scrolls" cue. */}
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-x-0 top-0 h-8 bg-gradient-to-b from-[var(--sidebar-bg)] to-transparent transition-opacity duration-200",
        overflow.above ? "opacity-100" : "opacity-0",
      )}
    />
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-x-0 bottom-0 flex h-12 items-end justify-center bg-gradient-to-t from-[var(--sidebar-bg)] from-30% to-transparent pb-1 transition-opacity duration-200",
        overflow.below ? "opacity-100" : "opacity-0",
      )}
    >
      <ChevronDown className="h-3.5 w-3.5 text-[var(--sidebar-text-muted)]" />
    </div>
    </div>
  )
}
