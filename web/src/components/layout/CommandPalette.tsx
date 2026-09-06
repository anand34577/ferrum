import { useQuery } from "@tanstack/react-query"
import { Command } from "cmdk"
import {
  AlertTriangle,
  Box,
  ClipboardList,
  Container,
  CornerDownLeft,
  Database,
  HardDrive,
  Keyboard,
  LayoutDashboard,
  Layers,
  Monitor,
  Moon,
  Network,
  Search,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Sun,
  Terminal,
  Users,
  Waypoints,
} from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { api, type ClusterResource, type ConnectionInventory } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { useTheme, type ThemePreference } from "@/lib/theme"

const pages = [
  { to: "/", label: "Fleet Overview", icon: Waypoints },
  { to: "/dashboard", label: "Custom Dashboard", icon: LayoutDashboard },
  { to: "/inventory", label: "Inventory", icon: Server },
  { to: "/topology", label: "Topology", icon: Network },
  { to: "/storage", label: "Storage", icon: Database },
  { to: "/pools", label: "Resource Pools", icon: Layers },
  { to: "/backups", label: "Backups", icon: HardDrive },
  { to: "/ha", label: "High Availability", icon: ShieldCheck },
  { to: "/firewall", label: "Firewall", icon: Shield },
  { to: "/alerts", label: "Alerts", icon: AlertTriangle },
  { to: "/tasks", label: "Task Center", icon: Terminal },
  { to: "/connections", label: "Connections", icon: Network, adminOnly: true },
  { to: "/users", label: "Users", icon: Users, adminOnly: true },
  { to: "/audit", label: "Audit Log", icon: ClipboardList, adminOnly: true },
  { to: "/settings", label: "Settings", icon: Settings, adminOnly: true },
]

export function CommandPalette() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const { user } = useAuth()
  const { theme, setTheme } = useTheme()

  const themeCommands: { value: ThemePreference; label: string; icon: typeof Sun }[] = [
    { value: "light", label: "Light theme", icon: Sun },
    { value: "dark", label: "Dark theme", icon: Moon },
    { value: "system", label: "System theme (follow OS)", icon: Monitor },
  ]

  const { data: inventory } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    enabled: open,
    staleTime: 10_000,
  })

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setOpen((o) => !o)
      }
      if (e.key === "Escape") setOpen(false)
    }
    // The header's Search button opens the palette through this event
    // instead of faking a synthetic Ctrl+K keypress.
    function onOpen() {
      setOpen(true)
    }
    document.addEventListener("keydown", onKeyDown)
    document.addEventListener("ferrum:open-command-palette", onOpen)
    return () => {
      document.removeEventListener("keydown", onKeyDown)
      document.removeEventListener("ferrum:open-command-palette", onOpen)
    }
  }, [])

  // Derived lists are memoized: rebuilding them on every render walked the
  // full inventory twice per keystroke.
  const guests = useMemo<Array<{ connId: string; guest: ClusterResource }>>(() => {
    const out: Array<{ connId: string; guest: ClusterResource }> = []
    for (const conn of inventory ?? []) {
      for (const r of conn.resources ?? []) {
        if (r.type === "qemu" || r.type === "lxc") out.push({ connId: conn.connectionId, guest: r })
      }
    }
    return out
  }, [inventory])

  const nodes = useMemo<Array<{ connId: string; node: ClusterResource }>>(() => {
    const out: Array<{ connId: string; node: ClusterResource }> = []
    for (const conn of inventory ?? []) {
      for (const r of conn.resources ?? []) {
        if (r.type === "node") out.push({ connId: conn.connectionId, node: r })
      }
    }
    return out
  }, [inventory])

  const visiblePages = useMemo(() => pages.filter((p) => !p.adminOnly || user?.isAdmin), [user?.isAdmin])

  function go(to: string) {
    navigate(to)
    setOpen(false)
  }

  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)
  const modKey = isMac ? "⌘" : "Ctrl"

  return (
    <Command.Dialog
      open={open}
      onOpenChange={setOpen}
      label="Command palette"
      overlayClassName="fixed inset-0 z-50 bg-black/65 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in data-[state=closed]:animate-out data-[state=closed]:fade-out"
      contentClassName="fixed left-1/2 top-[12vh] z-50 w-[calc(100vw-2rem)] max-w-xl -translate-x-1/2 overflow-hidden rounded-lg border border-[var(--border)] bg-[var(--bg-elevated)] shadow-2xl dark:shadow-[0_24px_64px_rgba(0,0,0,0.85),inset_0_1px_0_rgba(255,255,255,0.08)] data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95 data-[state=open]:slide-in-from-top-4 data-[state=closed]:animate-out data-[state=closed]:fade-out data-[state=closed]:zoom-out-95"
    >
      <div
        className="flex items-center gap-2.5 border-b border-[var(--border)] px-4 py-3"
        cmdk-input-wrapper=""
      >
        <Search className="h-4 w-4 shrink-0 text-brand-500" aria-hidden />
        {/* Same focus treatment as ui/Input — a rounded ring that follows the
            input's radius, not the global square outline. */}
        <Command.Input
          autoFocus
          placeholder="Jump to a page, node, or guest…"
          className="h-9 w-full rounded-lg bg-transparent px-2 text-sm outline-none placeholder:text-[var(--text-muted)] focus-visible:outline-none"
        />
        <kbd className="shrink-0 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--text-faint)]">
          Esc
        </kbd>
      </div>
      {/* Scroll only when the results genuinely don't fit — the cap tracks the
          viewport instead of a fixed 24rem. */}
      <Command.List className="max-h-[min(24rem,calc(100dvh-16rem))] overflow-y-auto p-2">
        <Command.Empty className="px-3 py-8 text-center text-sm text-[var(--text-muted)]">
          Nothing matches — try a page, node, or guest name.
        </Command.Empty>

        <Command.Group
          heading="Pages"
          className="text-xs text-[var(--text-muted)] [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em]"
        >
          {visiblePages.map((p) => (
            <Command.Item
              key={p.to}
              value={p.label}
              onSelect={() => go(p.to)}
              className="group flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-[var(--text)] data-[selected=true]:bg-[var(--bg-muted)]"
            >
              <p.icon className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
              <span className="truncate">{p.label}</span>
              <CornerDownLeft className="ml-auto h-3.5 w-3.5 shrink-0 text-[var(--text-faint)] opacity-0 group-data-[selected=true]:opacity-100" aria-hidden />
            </Command.Item>
          ))}
        </Command.Group>

        <Command.Group
          heading="Help"
          className="text-xs text-[var(--text-muted)] [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em]"
        >
          <Command.Item
            value="keyboard shortcuts"
            onSelect={() => {
              setOpen(false)
              document.dispatchEvent(new CustomEvent("ferrum:open-shortcuts"))
            }}
            className="group flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-[var(--text)] data-[selected=true]:bg-[var(--bg-muted)]"
          >
            <Keyboard className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
            <span className="truncate">Keyboard shortcuts</span>
            <kbd className="ml-auto shrink-0 rounded-sm border border-[var(--border)] bg-[var(--bg-surface)] px-1 py-px font-mono text-[10px] text-[var(--text-faint)]">?</kbd>
          </Command.Item>
        </Command.Group>

        <Command.Group
          heading="Appearance"
          className="text-xs text-[var(--text-muted)] [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em]"
        >
          {themeCommands.map((t) => (
            <Command.Item
              key={t.value}
              value={`${t.label} theme`}
              onSelect={() => {
                setTheme(t.value)
                setOpen(false)
              }}
              className="group flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-[var(--text)] data-[selected=true]:bg-[var(--bg-muted)]"
            >
              <t.icon className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
              <span className="truncate">{t.label}</span>
              {theme === t.value && (
                <span className="ml-auto shrink-0 rounded-sm bg-[var(--bg-muted)] px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-[var(--text-faint)]">
                  current
                </span>
              )}
            </Command.Item>
          ))}
        </Command.Group>

        {nodes.length > 0 && (
          <Command.Group
            heading="Nodes"
            className="text-xs text-[var(--text-muted)] [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em]"
          >
            {nodes.map(({ connId, node }) => (
              <Command.Item
                key={`${connId}/${node.node}`}
                value={`node ${node.node}`}
                onSelect={() => go(`/nodes/${connId}/${node.node}`)}
                className="group flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-[var(--text)] data-[selected=true]:bg-[var(--bg-muted)]"
              >
                <Server className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
                <span className="truncate">{node.node}</span>
                <CornerDownLeft className="ml-auto h-3.5 w-3.5 shrink-0 text-[var(--text-faint)] opacity-0 group-data-[selected=true]:opacity-100" aria-hidden />
              </Command.Item>
            ))}
          </Command.Group>
        )}

        {guests.length > 0 && (
          <Command.Group
            heading="Guests"
            className="text-xs text-[var(--text-muted)] [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em]"
          >
            {guests.map(({ guest }) => (
              <Command.Item
                key={guest.id}
                value={`${guest.name} ${guest.vmid} ${guest.tags?.replace(/[;,]/g, " ") ?? ""}`}
                onSelect={() => {
                  navigate("/inventory", { state: { focusGuestId: guest.id, focusGuestName: guest.name } })
                  setOpen(false)
                }}
                className="group flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-[var(--text)] data-[selected=true]:bg-[var(--bg-muted)]"
              >
                {guest.type === "lxc" ? (
                  <Container className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
                ) : (
                  <Box className="h-4 w-4 shrink-0 text-[var(--text-muted)] group-data-[selected=true]:text-brand-500" aria-hidden />
                )}
                <span className="truncate">{guest.name}</span>
                {guest.tags && <span className="truncate text-xs text-[var(--text-faint)]">{guest.tags.replace(/[;,]/g, ", ")}</span>}
                <span className="ml-auto shrink-0 font-mono text-xs text-[var(--text-muted)] tabular">#{guest.vmid}</span>
                <CornerDownLeft className="h-3.5 w-3.5 shrink-0 text-[var(--text-faint)] opacity-0 group-data-[selected=true]:opacity-100" aria-hidden />
              </Command.Item>
            ))}
          </Command.Group>
        )}
      </Command.List>

      <div className="flex items-center gap-4 border-t border-[var(--border)] bg-[var(--bg-surface)] px-4 py-2 text-[10px] text-[var(--text-faint)]">
        <span className="flex items-center gap-1.5">
          <kbd className="rounded-sm border border-[var(--border)] bg-[var(--bg-elevated)] px-1 py-px font-mono">↑↓</kbd> navigate
        </span>
        <span className="flex items-center gap-1.5">
          <kbd className="rounded-sm border border-[var(--border)] bg-[var(--bg-elevated)] px-1 py-px font-mono">↵</kbd> open
        </span>
        <span className="ml-auto flex items-center gap-1.5">
          <kbd className="rounded-sm border border-[var(--border)] bg-[var(--bg-elevated)] px-1 py-px font-mono">{modKey} K</kbd> toggle
        </span>
      </div>
    </Command.Dialog>
  )
}
