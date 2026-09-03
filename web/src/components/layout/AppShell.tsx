import {
  AlertTriangle,
  ClipboardList,
  Database,
  HardDrive,
  LayoutDashboard,
  Layers,
  LogOut,
  Menu,
  Moon,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Sun,
  Terminal,
  UserRound,
  Users,
  Waypoints,
} from "lucide-react"
import { type ReactNode, useState } from "react"
import { useNavigate } from "react-router-dom"
import { BrandMark } from "@/components/layout/BrandMark"
import { api } from "@/lib/api"
import { CommandPalette } from "@/components/layout/CommandPalette"
import { NotificationBell } from "@/components/layout/NotificationBell"
import { ShortcutsDialog } from "@/components/layout/ShortcutsDialog"
import { MobileDrawer, SidebarContent } from "@/components/layout/MobileDrawer"
import { useAuth } from "@/lib/auth"
import { useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Hint } from "@/components/ui/tooltip"
import { toast } from "sonner"

interface NavItemSpec {
  to: string
  label: string
  icon: typeof Server
  end?: boolean
  /** Hidden from non-admin users — the API enforces the same split. */
  adminOnly?: boolean
}

const navGroups: { label: string; items: NavItemSpec[] }[] = [
  {
    label: "Overview",
    items: [
      { to: "/", label: "Fleet Overview", icon: Waypoints, end: true },
      { to: "/dashboard", label: "Custom Dashboard", icon: LayoutDashboard },
      { to: "/inventory", label: "Inventory", icon: Server },
      { to: "/topology", label: "Topology", icon: Network },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { to: "/storage", label: "Storage", icon: Database },
      { to: "/pools", label: "Resource Pools", icon: Layers },
      { to: "/ha", label: "High Availability", icon: ShieldCheck },
    ],
  },
  {
    label: "Operations",
    items: [
      { to: "/backups", label: "Backups", icon: HardDrive },
      { to: "/firewall", label: "Firewall", icon: Shield },
      { to: "/alerts", label: "Alerts", icon: AlertTriangle },
      { to: "/tasks", label: "Task Center", icon: Terminal },
    ],
  },
  {
    label: "Administration",
    items: [
      { to: "/connections", label: "Connections", icon: Network, adminOnly: true },
      { to: "/users", label: "Users", icon: Users, adminOnly: true },
      { to: "/audit", label: "Audit Log", icon: ClipboardList, adminOnly: true },
      { to: "/settings", label: "Settings", icon: Settings, adminOnly: true },
    ],
  },
]

function visibleGroups(isAdmin: boolean) {
  if (isAdmin) return navGroups
  return navGroups
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.adminOnly) }))
    .filter((g) => g.items.length > 0)
}

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)
const paletteShortcutLabel = isMac ? "⌘K" : "Ctrl K"

export function AppShell({ children }: { children: ReactNode }) {
  const { user, refresh } = useAuth()
  const { effectiveTheme, toggle } = useTheme()
  const navigate = useNavigate()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

  const groups = visibleGroups(user?.isAdmin ?? false)

  async function logout() {
    try {
      await api.post("/auth/logout")
    } catch {
      toast.error("Sign out failed — the server didn't respond. You're still signed in here.")
    } finally {
      // Clear local state regardless: if the server is unreachable the
      // session may still be alive there, but the UI must not pretend
      // the sign-out silently failed.
      refresh()
      navigate("/")
    }
  }

  const initial = user?.username?.trim()?.charAt(0)?.toUpperCase() || "?"

  return (
    <div className="flex h-full">
      <CommandPalette />
      <ShortcutsDialog />

      {/* Skip link — first tab stop for keyboard users; #main-content is a
          focus target via tabIndex so focus actually moves with it. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[600] focus:rounded-md focus:bg-[var(--sidebar-bg)] focus:px-3 focus:py-2 focus:text-sm focus:text-white"
      >
        Skip to content
      </a>

      {/* Desktop sidebar — fixed dark instrument rail */}
      <aside
        className={cn(
          "hidden shrink-0 flex-col bg-[var(--sidebar-bg)] transition-[width] duration-200 md:flex",
          collapsed ? "md:w-16" : "md:w-60",
        )}
      >
        <div className={cn("flex h-14 shrink-0 items-center gap-2.5 border-b border-[var(--sidebar-border)]", collapsed ? "justify-center px-2" : "px-4")}>
          <BrandMark />
          {!collapsed && (
            <div className="min-w-0">
              <p className="font-display text-base font-semibold leading-tight tracking-tight text-[var(--sidebar-text)]">Ferrum</p>
              <p className="font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--sidebar-text-muted)]">Fleet control</p>
            </div>
          )}
        </div>
        <SidebarContent groups={groups} collapsed={collapsed} />
        <button
          onClick={() => setCollapsed((c) => !c)}
          className="hidden shrink-0 items-center gap-2 border-t border-[var(--sidebar-border)] px-4 py-2.5 text-xs font-medium text-[var(--sidebar-text-muted)] transition-colors hover:text-[var(--sidebar-text)] md:flex"
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {collapsed ? <PanelLeftOpen className="mx-auto h-3.5 w-3.5" /> : <><PanelLeftClose className="h-3.5 w-3.5" /> Collapse</>}
        </button>
      </aside>

      {/* Mobile drawer — Radix Dialog (focus trap, aria-modal, Escape) */}
      <MobileDrawer open={mobileOpen} onOpenChange={setMobileOpen}>
        {(close) => <SidebarContent groups={groups} collapsed={false} onNavigate={close} />}
      </MobileDrawer>

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-14 shrink-0 items-center justify-between gap-2 border-b border-[var(--border)] bg-[var(--bg-surface)] px-4">
          <div className="flex min-w-0 items-center gap-1">
            <button
              onClick={() => setMobileOpen(true)}
              className="flex h-9 w-9 items-center justify-center rounded-md text-[var(--text-muted)] hover:bg-[var(--bg-muted)] hover:text-[var(--text)] md:hidden"
              aria-label="Open menu"
            >
              <Menu className="h-4.5 w-4.5" />
            </button>
            <button
              onClick={() => document.dispatchEvent(new CustomEvent("ferrum:open-command-palette"))}
              className="hidden items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg)]/60 px-3 py-1.5 text-xs text-[var(--text-muted)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text)] sm:flex"
              aria-label="Open command palette"
            >
              <Search className="h-3.5 w-3.5" /> Search…
              <kbd className="ml-2 rounded border border-[var(--border)] bg-[var(--bg-surface)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--text-faint)]">
                {paletteShortcutLabel}
              </kbd>
            </button>
          </div>

          <div className="flex items-center gap-1.5">
            <NotificationBell />
            <Hint label={effectiveTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"}>
              <button
                onClick={toggle}
                className="flex h-9 w-9 items-center justify-center rounded-md text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)]"
                aria-label={effectiveTheme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
              >
                {effectiveTheme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
              </button>
            </Hint>

            <DropdownMenu>
              <DropdownMenuTrigger
                className="flex items-center gap-2.5 rounded-md px-1.5 py-1 transition-colors hover:bg-[var(--bg-muted)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
                aria-label="Account menu"
              >
                <span className="flex h-7 w-7 items-center justify-center rounded-full bg-brand-600 font-display text-xs font-bold text-white" aria-hidden>
                  {initial}
                </span>
                <span className="hidden text-sm font-medium sm:block">{user?.username}</span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel>{user?.username}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => navigate("/profile")}>
                  <UserRound className="h-3.5 w-3.5 text-[var(--text-muted)]" /> Profile & security
                </DropdownMenuItem>
                {user?.isAdmin && (
                  <DropdownMenuItem onSelect={() => navigate("/settings")}>
                    <Settings className="h-3.5 w-3.5 text-[var(--text-muted)]" /> Settings
                  </DropdownMenuItem>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => void logout()} className="text-[var(--status-error)] data-[highlighted]:bg-[color-mix(in_oklab,var(--status-error)_10%,transparent)]">
                  <LogOut className="h-3.5 w-3.5" /> Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        {/* Extra bottom padding so fully-scrolled content never sits flush
            against the window edge. No h-full on the wrapper: a full-height
            wrapper makes taller pages overflow it, and that overflow paints
            right over the padding — the exact "content glued to the screen
            bottom" effect this padding exists to prevent. */}
        <main id="main-content" tabIndex={-1} className="flex-1 overflow-y-auto p-4 pb-10 outline-none md:p-6 md:pb-12">
          <div className="mx-auto w-full max-w-[1720px]">{children}</div>
        </main>
      </div>
    </div>
  )
}
