export type WidgetType =
  | "fleet-overview"
  | "cluster-comparison"
  | "fleet-summary"
  | "connection-status"
  | "top-consumers"
  | "running-tasks"
  | "storage-usage"
  | "storage-treemap"
  | "guest-status"
  | "guest-histogram"
  | "utilization-heatmap"
  | "cpu-by-node"
  | "memory-by-node"
  | "capacity-planning"
  | "node-scatter"
  | "fleet-trend"
  | "node-comparison"
  | "node-composition"
  | "alert-activity"
  | "backup-activity"
  | "uptime-leaderboard"
  | "capacity-forecast"
  | "health-score"
  | "ha-status"
  | "tag-breakdown"
  | "cluster-activity"
  | "node-versions"
  | "server-roster"
  | "pool-usage"
  | "pbs-datastores"
  | "replication-status"

/** A widget's own configuration — e.g. which metric a bar chart ranks by,
 * or how many rows a list shows. Lives inside the same WidgetSpec that's
 * already persisted server-side (the dashboards table, via PUT
 * /dashboards/{id}), so a widget's settings survive across browsers and
 * devices exactly like its position does — nothing widget-specific is ever
 * kept in localStorage. */
export type WidgetSettings = Record<string, string>

export interface WidgetSpec {
  id: string
  type: WidgetType
  x: number
  y: number
  w: number
  h: number
  settings?: WidgetSettings
}

/** A user can keep several named dashboards — this is the lightweight row
 * used for the switcher list (GET /dashboards/); DashboardFull below is the
 * one dashboard currently open, fetched separately by id. */
export interface DashboardSummary {
  id: string
  name: string
  updatedAt: string
}

export interface DashboardFull {
  id: string
  name: string
  /** Grid schema version; absent/0 means a pre-versioning layout whose row
   * heights are in the old 64px unit and must be doubled. See LAYOUT_VERSION. */
  version?: number
  widgets: WidgetSpec[]
}

/** Bump alongside internal/api/dashboard.go's layoutVersion. v2 halved the
 * row unit (64px -> 32px) so resizing snaps in finer steps and widgets stop
 * being padded out to the next 76px multiple. */
export const LAYOUT_VERSION = 2

/** One-time v1 -> v2 conversion: every vertical measure doubles. */
export function migrateLayout(version: number | undefined, widgets: WidgetSpec[]): WidgetSpec[] {
  if ((version ?? 1) >= LAYOUT_VERSION) return widgets
  return widgets.map((w) => ({ ...w, y: w.y * 2, h: w.h * 2 }))
}

export interface SettingField {
  key: string
  label: string
  options: { value: string; label: string }[]
  /** "connections" = options resolved at render time from the configured
   * Proxmox connections (plus "All connections") — the multi-cluster scope
   * selector every data widget shares. */
  dynamic?: "connections"
}

/** Shared per-widget connection scope: "all" (fleet-wide) or one connection. */
export const CONNECTION_FIELD: SettingField = {
  key: "connection",
  label: "Scope",
  options: [],
  dynamic: "connections",
}

export const WIDGET_CATALOG: {
  type: WidgetType
  label: string
  defaultSize: { w: number; h: number }
  defaultSettings?: WidgetSettings
  settingsFields?: SettingField[]
}[] = [
  { type: "fleet-overview", label: "Fleet KPI Matrix", defaultSize: { w: 12, h: 8 } },
  {
    type: "cluster-comparison",
    label: "Cluster Comparison",
    defaultSize: { w: 12, h: 10 },
    defaultSettings: { sort: "cpu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "sort",
        label: "Sort by",
        options: [
          { value: "cpu", label: "CPU %" },
          { value: "memory", label: "Memory %" },
          { value: "storage", label: "Storage %" },
          { value: "vms", label: "Guest count" },
          { value: "name", label: "Name" },
        ],
      },
    ],
  },
  { type: "fleet-summary", label: "Fleet Summary", defaultSize: { w: 12, h: 4 }, settingsFields: [CONNECTION_FIELD] },
  { type: "connection-status", label: "Connection Status", defaultSize: { w: 6, h: 8 } },
  {
    type: "cpu-by-node",
    label: "CPU by Node",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { connection: "all", sort: "usage" },
    settingsFields: [CONNECTION_FIELD],
  },
  {
    type: "memory-by-node",
    label: "Memory by Node",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { connection: "all" },
    settingsFields: [CONNECTION_FIELD],
  },
  {
    type: "capacity-planning",
    label: "Capacity & Overcommit",
    defaultSize: { w: 6, h: 10 },
    defaultSettings: { connection: "all" },
    settingsFields: [CONNECTION_FIELD],
  },
  {
    type: "node-scatter",
    label: "Node Density (CPU × Memory)",
    defaultSize: { w: 6, h: 10 },
    defaultSettings: { connection: "all" },
    settingsFields: [CONNECTION_FIELD],
  },
  {
    type: "top-consumers",
    label: "Top Resource Consumers",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { metric: "cpu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "metric",
        label: "Rank by",
        options: [
          { value: "cpu", label: "CPU %" },
          { value: "mem", label: "Memory %" },
        ],
      },
    ],
  },
  {
    type: "running-tasks",
    label: "Running Tasks",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { limit: "8" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "limit",
        label: "Rows",
        options: [
          { value: "5", label: "5" },
          { value: "8", label: "8" },
          { value: "15", label: "15" },
        ],
      },
    ],
  },
  { type: "storage-usage", label: "Storage Usage", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "storage-treemap", label: "Storage Treemap", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "guest-status", label: "Guest Status Distribution", defaultSize: { w: 4, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  {
    type: "guest-histogram",
    label: "Utilization Distribution",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { metric: "cpu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "metric",
        label: "Distribute by",
        options: [
          { value: "cpu", label: "CPU %" },
          { value: "mem", label: "Memory %" },
        ],
      },
    ],
  },
  {
    type: "utilization-heatmap",
    label: "Utilization Heatmap",
    defaultSize: { w: 12, h: 10 },
    defaultSettings: { timeframe: "day", metric: "cpu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "timeframe",
        label: "Range",
        options: [
          { value: "hour", label: "Last hour" },
          { value: "day", label: "Last day" },
          { value: "week", label: "Last week" },
        ],
      },
      {
        key: "metric",
        label: "Metric",
        options: [
          { value: "cpu", label: "CPU %" },
          { value: "mem", label: "Memory %" },
        ],
      },
    ],
  },
  {
    type: "fleet-trend",
    label: "Fleet Trend (CPU / Memory / Network)",
    defaultSize: { w: 12, h: 10 },
    defaultSettings: { timeframe: "hour" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "timeframe",
        label: "Range",
        options: [
          { value: "hour", label: "Last hour" },
          { value: "day", label: "Last day" },
          { value: "week", label: "Last week" },
        ],
      },
    ],
  },
  {
    type: "node-comparison",
    label: "Node Comparison",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { metric: "cpu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "metric",
        label: "Compare by",
        options: [
          { value: "cpu", label: "CPU %" },
          { value: "mem", label: "Memory %" },
          { value: "disk", label: "Disk %" },
        ],
      },
    ],
  },
  {
    type: "node-composition",
    label: "Node Guest Composition",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { sort: "qemu" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "sort",
        label: "Sort by",
        options: [
          { value: "qemu", label: "VM count" },
          { value: "total", label: "Total guests" },
        ],
      },
    ],
  },
  { type: "alert-activity", label: "Alert Activity", defaultSize: { w: 4, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "backup-activity", label: "Backup Activity", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  {
    type: "uptime-leaderboard",
    label: "Uptime Leaderboard",
    defaultSize: { w: 4, h: 8 },
    defaultSettings: { scope: "guest" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "scope",
        label: "Show",
        options: [
          { value: "guest", label: "Guests" },
          { value: "node", label: "Nodes" },
        ],
      },
    ],
  },
  {
    type: "capacity-forecast",
    label: "Capacity Forecast",
    defaultSize: { w: 6, h: 8 },
    defaultSettings: { horizonDays: "30" },
    settingsFields: [
      CONNECTION_FIELD,
      {
        key: "horizonDays",
        label: "Horizon",
        options: [
          { value: "14", label: "14 days" },
          { value: "30", label: "30 days" },
          { value: "60", label: "60 days" },
          { value: "90", label: "90 days" },
        ],
      },
    ],
  },
  { type: "health-score", label: "Fleet Health Score", defaultSize: { w: 4, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "ha-status", label: "HA & Cluster Quorum", defaultSize: { w: 6, h: 8 } },
  { type: "tag-breakdown", label: "Guest Tags Breakdown", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "cluster-activity", label: "Cluster Activity Log", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "node-versions", label: "Node Versions & Drift", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "server-roster", label: "Server Roster", defaultSize: { w: 6, h: 10 } },
  { type: "pool-usage", label: "Resource Pools", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "pbs-datastores", label: "PBS Datastores", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
  { type: "replication-status", label: "Replication Health", defaultSize: { w: 6, h: 8 }, settingsFields: [CONNECTION_FIELD] },
]

export function widgetLabel(type: WidgetType): string {
  return WIDGET_CATALOG.find((w) => w.type === type)?.label ?? type
}

export function widgetDefaultSettings(type: WidgetType): WidgetSettings {
  return WIDGET_CATALOG.find((w) => w.type === type)?.defaultSettings ?? {}
}
