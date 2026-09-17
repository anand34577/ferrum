import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, ChevronDown, Eraser, LayoutDashboard, Pencil, Plus, Save, SlidersHorizontal, Trash2 } from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { Responsive as ResponsiveGridLayout, useContainerWidth, type Layout } from "react-grid-layout"
import "react-grid-layout/css/styles.css"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { WidgetChrome } from "@/components/dashboard/WidgetChrome"
import { widgetRegistry } from "@/components/dashboard/widgetRegistry"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError } from "@/lib/api"
import { useConnections } from "@/lib/fleet"
import { cn } from "@/lib/utils"
import {
  LAYOUT_VERSION,
  migrateLayout,
  WIDGET_CATALOG,
  widgetDefaultSettings,
  type DashboardFull,
  type DashboardSummary,
  type WidgetSettings,
  type WidgetSpec,
  type WidgetType,
} from "@/lib/dashboardTypes"

/** Create/rename — the only two places a dashboard needs a name typed in. */
function NameDashboardForm({
  initial,
  title,
  onSave,
  onCancel,
}: {
  initial: string
  title: string
  onSave: (name: string) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(initial)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    onSave(trimmed)
  }

  return (
    <DialogContent className="max-w-sm">
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>Only you see this — it's just a label for the switcher.</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="dashboard-name">Name</Label>
          <Input id="dashboard-name" autoFocus maxLength={60} value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
          <Button type="submit" disabled={!name.trim()}>Save</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  )
}

function NameDashboardDialog({
  open,
  onOpenChange,
  initial,
  title,
  onSave,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial: string
  title: string
  onSave: (name: string) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <NameDashboardForm
          key={initial}
          initial={initial}
          title={title}
          onSave={(name) => {
            onSave(name)
            onOpenChange(false)
          }}
          onCancel={() => onOpenChange(false)}
        />
      )}
    </Dialog>
  )
}

export function DashboardPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const { width, containerRef, mounted } = useContainerWidth()

  const [dashboards, setDashboards] = useState<DashboardSummary[] | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [nameDialog, setNameDialog] = useState<"create" | "rename" | null>(null)
  const dirty = useRef(false)
  const pickedInitial = useRef(false)
  const nextWidgetId = useRef(1)

  // Shares the query-cache entry ThemeProvider already populates (same key) —
  // this doesn't cost a second network round trip in the common case.
  const prefsQuery = useQuery({
    queryKey: ["auth", "preferences"],
    queryFn: () => api.get<{ activeDashboardId?: string }>("/auth/me/preferences"),
    staleTime: 60_000,
  })

  function loadDashboardList() {
    api
      .get<DashboardSummary[]>("/dashboards/")
      .then((list) => {
        setDashboards(list)
      })
      .catch((err) => toast.error(err instanceof ApiError ? err.message : "Failed to load dashboards"))
  }
  useEffect(loadDashboardList, [])

  // Once both the list and the saved preference have arrived, open the
  // remembered dashboard if it still exists, else the oldest one. Runs once
  // per mount — later list changes (create/delete) manage activeId directly.
  useEffect(() => {
    if (pickedInitial.current || !dashboards || dashboards.length === 0 || prefsQuery.isLoading) return
    pickedInitial.current = true
    const preferred = prefsQuery.data?.activeDashboardId
    setActiveId(preferred && dashboards.some((d) => d.id === preferred) ? preferred : dashboards[0].id)
  }, [dashboards, prefsQuery.data, prefsQuery.isLoading])

  const widgetsQuery = useQuery({
    queryKey: ["dashboard", activeId],
    queryFn: () => (activeId ? api.get<DashboardFull>(`/dashboards/${activeId}`) : Promise.resolve(null)),
    enabled: Boolean(activeId),
  })

  // Synchronize local editable layout with loaded dashboard when activeId or query changes
  const loadedWidgets = widgetsQuery.data?.widgets ?? null
  const loadedVersion = widgetsQuery.data?.version
  const [widgets, setWidgets] = useState<WidgetSpec[] | null>(null)
  const prevLoadedRef = useRef<WidgetSpec[] | null>(null)

  if (loadedWidgets !== prevLoadedRef.current) {
    prevLoadedRef.current = loadedWidgets
    setWidgets(loadedWidgets && migrateLayout(loadedVersion, loadedWidgets))
  }

  const loadError = widgetsQuery.isError

  // onLayoutChange saves lazily (only on "Done"/unmount via dirty.current) —
  // without this, a drag right before a tab close or refresh is silently lost.
  useEffect(() => {
    function onBeforeUnload(e: BeforeUnloadEvent) {
      if (!dirty.current) return
      e.preventDefault()
    }
    window.addEventListener("beforeunload", onBeforeUnload)
    return () => window.removeEventListener("beforeunload", onBeforeUnload)
  }, [])

  function saveLayout(next: WidgetSpec[], targetId = activeId) {
    if (!targetId) return
    dirty.current = false
    api.put(`/dashboards/${targetId}`, { version: LAYOUT_VERSION, widgets: next }).catch((err: unknown) => {
      // Save failed — the edit is still only local, so put the dirty flag
      // back or it's lost for good on next unload/switch with no retry.
      dirty.current = true
      toast.error(err instanceof ApiError ? err.message : "Failed to save dashboard layout — your changes are not saved yet.")
    })
  }

  function switchDashboard(id: string) {
    if (id === activeId) return
    if (dirty.current && widgets) saveLayout(widgets) // flush pending edits to the dashboard we're leaving
    setEditing(false)
    setActiveId(id)
    api.put("/auth/me/preferences", { activeDashboardId: id }).catch((err) => console.warn("failed to save active dashboard preference", err))
    queryClient.setQueryData<{ activeDashboardId?: string } | undefined>(["auth", "preferences"], (old) =>
      old ? { ...old, activeDashboardId: id } : old,
    )
  }

  async function createDashboard(name: string) {
    try {
      const created = await api.post<DashboardFull>("/dashboards/", { name })
      setDashboards((prev) => [...(prev ?? []), { id: created.id, name: created.name, updatedAt: new Date().toISOString() }])
      switchDashboard(created.id)
      toast.success(`Created "${created.name}"`)
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Failed to create dashboard")
    }
  }

  async function renameDashboard(name: string) {
    if (!activeId) return
    try {
      await api.put(`/dashboards/${activeId}`, { name })
      setDashboards((prev) => prev?.map((d) => (d.id === activeId ? { ...d, name } : d)) ?? null)
      toast.success(`Renamed to "${name}"`)
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Failed to rename dashboard")
    }
  }

  async function deleteDashboard() {
    if (!activeId || !dashboards) return
    const current = dashboards.find((d) => d.id === activeId)
    const ok = await confirm({
      title: `Delete "${current?.name}"?`,
      description: "Every widget on this dashboard is removed. This cannot be undone.",
      confirmLabel: "Delete",
    })
    if (!ok) return
    try {
      await api.delete(`/dashboards/${activeId}`)
      const remaining = dashboards.filter((d) => d.id !== activeId)
      setDashboards(remaining)
      const next = remaining[0]?.id ?? null
      setActiveId(next)
      if (next) api.put("/auth/me/preferences", { activeDashboardId: next }).catch((err) => console.warn("failed to save active dashboard preference", err))
      toast.success("Dashboard deleted")
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Failed to delete dashboard")
    }
  }

  function onLayoutChange(_current: Layout, allLayouts: Partial<Record<string, Layout>>) {
    if (!widgets) return
    // Only the desktop (12-column) layout is saved — the tablet/phone layouts
    // are derived and must not overwrite it when a small screen re-compacts.
    const lg = allLayouts.lg
    if (!lg) return
    let changed = false
    const next = widgets.map((w) => {
      const item = lg.find((l) => l.i === w.id)
      if (!item) return w
      if (item.x !== w.x || item.y !== w.y || item.w !== w.w || item.h !== w.h) changed = true
      return { ...w, x: item.x, y: item.y, w: item.w, h: item.h }
    })
    if (!changed) return
    setWidgets(next)
    dirty.current = true
  }

  function addWidget(type: WidgetType, connection?: string) {
    if (!widgets) return
    const spec = WIDGET_CATALOG.find((w) => w.type === type)
    if (!spec) return
    const maxY = widgets.reduce((m, w) => Math.max(m, w.y + w.h), 0)
    const settings = { ...widgetDefaultSettings(type) }
    if (connection) settings.connection = connection
    const widgetId = `${type}-${widgets.length + 1}-${nextWidgetId.current++}`
    const next = [
      ...widgets,
      { id: widgetId, type, x: 0, y: maxY, w: spec.defaultSize.w, h: spec.defaultSize.h, settings },
    ]
    setWidgets(next)
    saveLayout(next)
  }

  // Widget settings save immediately (not gated behind "Done") — a setting
  // change is a deliberate, complete action on its own, not a drag-in-progress.
  function updateWidgetSettings(id: string, settings: WidgetSettings) {
    if (!widgets) return
    const next = widgets.map((w) => (w.id === id ? { ...w, settings } : w))
    setWidgets(next)
    saveLayout(next)
  }

  // Keyboard fallback for react-grid-layout's mouse/touch-only drag: swaps this
  // widget's grid position with its reading-order (y, then x) neighbor. RGL's
  // vertical compaction resolves any resulting overlap on the next render.
  function moveWidget(id: string, dir: "up" | "down") {
    if (!widgets) return
    const order = [...widgets].sort((a, b) => a.y - b.y || a.x - b.x)
    const idx = order.findIndex((w) => w.id === id)
    const swapIdx = dir === "up" ? idx - 1 : idx + 1
    if (idx === -1 || swapIdx < 0 || swapIdx >= order.length) return
    const a = order[idx]
    const b = order[swapIdx]
    const next = widgets.map((w) => {
      if (w.id === a.id) return { ...w, x: b.x, y: b.y }
      if (w.id === b.id) return { ...w, x: a.x, y: a.y }
      return w
    })
    setWidgets(next)
    saveLayout(next)
  }

  function removeWidget(id: string) {
    if (!widgets) return
    const next = widgets.filter((w) => w.id !== id)
    setWidgets(next)
    saveLayout(next)
  }

  async function clearWidgets() {
    const ok = await confirm({
      title: "Clear all widgets?",
      description: "Every widget on this dashboard is removed — you keep the dashboard itself and can add widgets back at any time. This cannot be undone.",
      confirmLabel: "Clear",
    })
    if (!ok) return
    setWidgets([])
    saveLayout([])
  }

  function stopEditing() {
    setEditing(false)
    if (dirty.current && widgets) saveLayout(widgets)
    queryClient.invalidateQueries({ queryKey: ["inventory"] })
  }

  // A widget type may appear once per scope (fleet-wide or one connection):
  // a type with no instance adds directly as fleet-wide; otherwise the menu
  // lists the connections that don't have that type yet — so "Storage Usage"
  // can exist for the whole fleet AND per cluster, but never twice for the
  // same server.
  const { data: connections } = useConnections()
  const addable = useMemo(() => {
    return WIDGET_CATALOG.map((c) => {
      const taken = new Set((widgets ?? []).filter((w) => w.type === c.type).map((w) => w.settings?.connection || "all"))
      const freeConns = (connections ?? []).filter((conn) => !taken.has(conn.id))
      const fleetFree = !taken.has("all")
      return { type: c.type, label: c.label, fleetFree, freeConns, exhausted: !fleetFree && freeConns.length === 0 }
    })
  }, [widgets, connections])
  const addableTypes = addable.filter((a) => !a.exhausted)

  const activeName = dashboards?.find((d) => d.id === activeId)?.name ?? "Dashboard"

  return (
    <div className="space-y-4" ref={containerRef}>
      <PageHeader
        title={
          <DropdownMenu>
            <DropdownMenuTrigger
              className="inline-flex items-center gap-1.5 rounded-md px-1 -mx-1 transition-colors hover:bg-[var(--bg-muted)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
              aria-label="Switch dashboard"
            >
              {activeName}
              <ChevronDown className="h-4 w-4 shrink-0 text-[var(--text-muted)]" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-56">
              <DropdownMenuLabel>Your dashboards</DropdownMenuLabel>
              {dashboards?.map((d) => (
                <DropdownMenuItem key={d.id} onSelect={() => switchDashboard(d.id)}>
                  <Check className={cn("h-3.5 w-3.5 shrink-0", d.id !== activeId && "invisible")} aria-hidden />
                  <span className="truncate">{d.name}</span>
                </DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => setNameDialog("create")}>
                <Plus className="h-3.5 w-3.5" /> New dashboard
              </DropdownMenuItem>
              {activeId && (
                <DropdownMenuItem onSelect={() => setNameDialog("rename")}>
                  <Pencil className="h-3.5 w-3.5" /> Rename this dashboard
                </DropdownMenuItem>
              )}
              {activeId && (dashboards?.length ?? 0) > 1 && (
                <DropdownMenuItem onSelect={() => void deleteDashboard()} className="text-[var(--status-error)] data-[highlighted]:bg-[color-mix(in_oklab,var(--status-error)_10%,transparent)]">
                  <Trash2 className="h-3.5 w-3.5" /> Delete this dashboard
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        }
        description="Your own widget grid across every Proxmox connection — keep several named dashboards and switch between them, each widget scoped to one cluster or the whole fleet."
        icon={LayoutDashboard}
        actions={
          <>
            {editing && addableTypes.length > 0 && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="secondary" size="sm">
                    <Plus className="h-3.5 w-3.5" /> Add widget
                  </Button>
                </DropdownMenuTrigger>
                {/* Menu scrolls instead of overflowing the viewport when the
                    widget list is taller than the screen. */}
                <DropdownMenuContent align="end" className="max-h-[var(--radix-dropdown-menu-content-available-height,18rem)] w-64 overflow-y-auto">
                  <DropdownMenuLabel className="sticky top-0 bg-[var(--bg-elevated)]">Available widgets</DropdownMenuLabel>
                  {addableTypes.map((a) =>
                    a.fleetFree ? (
                      <DropdownMenuItem key={a.type} onSelect={() => addWidget(a.type)}>
                        {a.label}
                      </DropdownMenuItem>
                    ) : (
                      <DropdownMenuSub key={a.type}>
                        <DropdownMenuSubTrigger>{a.label}</DropdownMenuSubTrigger>
                        <DropdownMenuSubContent className="max-h-[var(--radix-dropdown-menu-content-available-height,18rem)] overflow-y-auto">
                          {a.freeConns.map((conn) => (
                            <DropdownMenuItem key={conn.id} onSelect={() => addWidget(a.type, conn.id)}>
                              {conn.name}
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuSubContent>
                      </DropdownMenuSub>
                    ),
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {editing && widgets && widgets.length > 0 && (
              <Button variant="ghost" size="sm" onClick={() => void clearWidgets()}>
                <Eraser className="h-3.5 w-3.5" /> Clear
              </Button>
            )}
            <Button variant={editing ? "default" : "secondary"} size="sm" onClick={() => (editing ? stopEditing() : setEditing(true))}>
              {editing ? <Save className="h-3.5 w-3.5" /> : <SlidersHorizontal className="h-3.5 w-3.5" />}
              {editing ? "Done" : "Customize"}
            </Button>
          </>
        }
      />

      <NameDashboardDialog
        open={nameDialog === "create"}
        onOpenChange={(o) => !o && setNameDialog(null)}
        initial=""
        title="New dashboard"
        onSave={createDashboard}
      />
      <NameDashboardDialog
        open={nameDialog === "rename"}
        onOpenChange={(o) => !o && setNameDialog(null)}
        initial={activeName}
        title="Rename dashboard"
        onSave={renameDashboard}
      />

      {loadError && (
        <ErrorState
          title="Couldn't load your dashboard"
          message="The layout could not be fetched. Your widgets will be back once the connection recovers."
          onRetry={() => {
            loadDashboardList()
            widgetsQuery.refetch()
          }}
        />
      )}

      {!loadError && !widgets && (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3" aria-busy>
          <Skeleton className="h-44 md:col-span-2 lg:col-span-2" />
          <Skeleton className="h-44" />
          <Skeleton className="h-44" />
          <Skeleton className="h-44" />
        </div>
      )}

      {mounted && widgets && widgets.length > 0 && (
        <ResponsiveGridLayout
          layouts={{
            // Desktop keeps the saved 12-column layout; smaller breakpoints get
            // a derived full-width stack so tablets/phones never overflow.
            // minW/minH keep a resize from being dragged down to an unusably
            // tiny sliver (a real complaint on its own — nothing stops the
            // handle from being dragged too far without a floor) and maxH
            // caps a drag/collision cascade from running away to an
            // absurd height. Bounds come off each widget's own catalog
            // default (roughly its smallest still-readable size), not a
            // single fixed number, since a full-width chart and a small KPI
            // tile need very different floors.
            lg: widgets.map((w) => {
              const spec = WIDGET_CATALOG.find((c) => c.type === w.type)
              return {
                i: w.id, x: w.x, y: w.y, w: w.w, h: w.h,
                minW: spec ? Math.min(spec.defaultSize.w, 3) : 2,
                minH: spec ? Math.min(spec.defaultSize.h, 4) : 4,
                maxH: 60,
              }
            }),
            sm: (() => {
              let y = 0
              return widgets.map((w) => {
                const item = { i: w.id, x: 0, y, w: Math.min(w.w, 6), h: w.h }
                y += w.h
                return item
              })
            })(),
            xs: (() => {
              let y = 0
              return widgets.map((w) => {
                const item = { i: w.id, x: 0, y, w: 1, h: w.h }
                y += w.h
                return item
              })
            })(),
          }}
          breakpoints={{ lg: 1024, sm: 640, xs: 0 }}
          cols={{ lg: 12, sm: 6, xs: 1 }}
          // 32px rows (v2) instead of 64: resizing snaps in half-height steps,
          // so a widget can stop at its content instead of being padded out to
          // the next 76px multiple. Horizontal margin stays 12; two v2 rows
          // plus the gap between them equal exactly one old row.
          rowHeight={32}
          margin={[12, 12]}
          width={width}
          dragConfig={{ enabled: editing, handle: ".drag-handle" }}
          // Edge handles as well as the corner: a widget that is only too tall
          // is resized straight down, without also having to hold its width.
          resizeConfig={{ enabled: editing, handles: ["se", "s", "e"] }}
          onLayoutChange={onLayoutChange}
        >
          {widgets.map((w) => {
            const Widget = widgetRegistry[w.type]
            const settings = w.settings ?? widgetDefaultSettings(w.type)
            const order = [...widgets].sort((a, b) => a.y - b.y || a.x - b.x)
            const orderIdx = order.findIndex((o) => o.id === w.id)
            return (
              <div key={w.id}>
                <WidgetChrome
                  type={w.type}
                  editing={editing}
                  onRemove={() => removeWidget(w.id)}
                  onMoveUp={orderIdx > 0 ? () => moveWidget(w.id, "up") : undefined}
                  onMoveDown={orderIdx < order.length - 1 ? () => moveWidget(w.id, "down") : undefined}
                  settings={settings}
                  onSettingsChange={(next) => updateWidgetSettings(w.id, next)}
                >
                  <Widget settings={settings} />
                </WidgetChrome>
              </div>
            )
          })}
        </ResponsiveGridLayout>
      )}

      {widgets && widgets.length === 0 && (
        <EmptyState
          icon={LayoutDashboard}
          title="No widgets on this dashboard yet"
          description="Click Customize, then Add widget to build your own fleet overview — every widget can be moved, resized and configured."
        />
      )}
    </div>
  )
}
