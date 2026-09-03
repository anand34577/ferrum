export class ApiError extends Error {
  status: number
  /** Machine-readable error code (e.g. "totp_required") — set only for the
   * handful of errors the UI needs to branch on, not every 4xx. */
  code?: string
  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

// Handlers registered via onUnauthorized fire when any API call comes back
// 401 — the auth layer uses this to reset its cache so expired sessions
// land on the login page instead of a wall of per-page error states.
type UnauthorizedHandler = () => void
const unauthorizedHandlers = new Set<UnauthorizedHandler>()

export function onUnauthorized(handler: UnauthorizedHandler): () => void {
  unauthorizedHandlers.add(handler)
  return () => unauthorizedHandlers.delete(handler)
}

// Handlers registered via onErrorCode fire for any API error that carries a
// `code` field — used for errors the UI must react to structurally (e.g.
// "totp_required" redirecting to the enrollment page), where matching the
// human-readable message would be fragile.
type ErrorCodeHandler = (code: string) => void
const errorCodeHandlers = new Set<ErrorCodeHandler>()

export function onErrorCode(handler: ErrorCodeHandler): () => void {
  errorCodeHandlers.add(handler)
  return () => errorCodeHandlers.delete(handler)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  })

  if (res.status === 204) return undefined as T

  const isJson = res.headers.get("content-type")?.includes("application/json")
  const data = isJson ? await res.json().catch(() => undefined) : undefined

  if (!res.ok) {
    if (res.status === 401) {
      for (const handler of unauthorizedHandlers) handler()
    }
    if (data?.code) {
      for (const handler of errorCodeHandlers) handler(data.code)
    }
    throw new ApiError(res.status, data?.error ?? res.statusText, data?.code)
  }
  return data as T
}

export const api = {
  get: <T>(path: string, init?: Omit<RequestInit, "method">) => request<T>(path, { ...init, method: "GET" }),
  post: <T>(path: string, body?: unknown, init?: Omit<RequestInit, "method" | "body">) =>
    request<T>(path, { ...init, method: "POST", body: body ? JSON.stringify(body) : undefined }),
  put: <T>(path: string, body?: unknown, init?: Omit<RequestInit, "method" | "body">) =>
    request<T>(path, { ...init, method: "PUT", body: body ? JSON.stringify(body) : undefined }),
  delete: <T>(path: string, init?: Omit<RequestInit, "method">) => request<T>(path, { ...init, method: "DELETE" }),
}
// --- Domain types (mirrors internal/auth, internal/pve, internal/api on the backend) ---

export interface User {
  id: string
  username: string
  email: string
  isAdmin: boolean
  totpEnabled: boolean
}

export interface Connection {
  id: string
  name: string
  host: string
  port: number
  authType: "token" | "password"
  username?: string
  tokenId?: string
  verifyTls: boolean
  behindReverseProxy: boolean
  createdAt: string
}

// Mirrors pve.ClusterResource (internal/pve/client.go).
export interface ClusterResource {
  id: string
  type: "node" | "qemu" | "lxc" | "storage" | "pool"
  // Present on every row PVE actually emits; typed required because the
  // fleet widgets pass it straight into node-scoped helpers.
  node: string
  vmid?: number
  name?: string
  status?: string
  cpu?: number
  maxcpu?: number
  mem?: number
  maxmem?: number
  disk?: number
  maxdisk?: number
  uptime?: number
  storage?: string
  tags?: string
  pool?: string
  plugintype?: string
  shared?: number
  hastate?: string
  template?: number
}

// Mirrors api.connectionInventory (internal/api/inventory.go).
export interface ConnectionInventory {
  connectionId: string
  name: string
  online: boolean
  error?: string
  resources?: ClusterResource[]
}

// Mirrors the api.fleetOverview aggregate (internal/api/overview.go).
export interface FleetOverviewConn {
  connectionId: string
  name: string
  host: string
  port: number
  online: boolean
  error?: string
  latencyMs?: number
  checkedAt: string
  cluster?: { name: string; quorate: boolean; nodes: number }
  nodes: { total: number; online: number; cores: number }
  vms: { total: number; running: number; stopped: number }
  lxcs: { total: number; running: number; stopped: number }
  templates: number
  haGuests: number
  cpu: { cores: number; usedCores: number; pct: number }
  memory: { total: number; used: number; pct: number }
  storage: { total: number; used: number; pct: number; byType: Record<string, number> }
  alerts: { critical: number; warning: number }
}

// Mirrors pve.RRDPoint (internal/pve/rrd.go).
export interface RRDPoint {
  time: number
  cpu?: number
  maxcpu?: number
  mem?: number
  maxmem?: number
  disk?: number
  maxdisk?: number
  netin?: number
  netout?: number
  diskread?: number
  diskwrite?: number
  swap?: number
  maxswap?: number
  iowait?: number
  loadavg?: number
  pressurecpusome?: number
  pressureiosome?: number
  pressureiofull?: number
  pressurememorysome?: number
  pressurememoryfull?: number
  extra?: Record<string, number>
}

// Mirrors pve.FirewallRule (internal/pve/firewall.go).
export interface FirewallRule {
  pos: number
  type: string
  action: string
  enable?: number
  source?: string
  dest?: string
  proto?: string
  dport?: string
  sport?: string
  comment?: string
  macro?: string
}

// Mirrors api.templateItem (internal/api/templates.go).
export interface TemplateItem {
  volid: string
  node: string
  storage: string
  content: "iso" | "vztmpl"
  size?: number
}

// Mirrors pve.Task (internal/pve/nodes.go).
export interface Task {
  upid: string
  node: string
  type: string
  status: string
  user: string
  starttime: number
  endtime?: number
  id?: string
}

// Mirrors pve.GuestLiveStatus (internal/pve/guests.go).
export interface GuestLiveStatus {
  status: string
  name?: string
  cpu?: number
  cpus?: number
  mem?: number
  maxmem?: number
  swap?: number
  maxswap?: number
  netin?: number
  netout?: number
  diskread?: number
  diskwrite?: number
  disk?: number
  maxdisk?: number
  uptime?: number
  balloon?: number
  lock?: string
  tags?: string
  hastate?: string
}

// Mirrors pve.BackupJob (internal/pve/backup.go).
export interface BackupJob {
  id: string
  schedule?: string
  storage?: string
  vmid?: string
  enabled?: number
  mode?: string
  compress?: string
  comment?: string
}

// Mirrors pve.Pool / pve.PoolDetail (internal/pve/pools.go).
export interface Pool {
  poolid: string
  comment?: string
}

export interface PoolDetail {
  poolid: string
  comment?: string
  members?: ClusterResource[]
}

// Mirrors pve.HAResource / pve.HAGroup / pve.HAStatus (internal/pve/ha.go).
export interface HAResource {
  sid: string
  type: string
  state?: string
  group?: string
  max_restart?: number
  max_relocate?: number
  comment?: string
}

export interface HAGroup {
  group: string
  nodes: string
  restricted?: number
  nofailback?: number
  comment?: string
}

export interface HAStatus {
  id: string
  type: string
  status?: string
  node?: string
}

// Mirrors pve.ReplicationJob / pve.ReplicationStatus (internal/pve/replication.go).
export interface ReplicationJob {
  id: string
  type: string
  source?: string
  target: string
  schedule?: string
  guest?: number
  disable?: number
  comment?: string
}

export interface ReplicationStatus {
  id: string
  last_sync?: number
  next_sync?: number
  duration?: number
  error?: string
  fail_count?: number
}

// Mirrors pve.CephStatus / pve.CephPool / pve.CephOSD (internal/pve/storage.go).
export interface CephStatus {
  health: { status: string }
  pgmap: { bytes_used: number; bytes_total: number; bytes_avail: number; num_pgs: number }
  osdmap: { num_osds: number; num_up_osds: number; num_in_osds: number }
}

export interface CephPool {
  pool_name: string
  size: number
  min_size: number
  pg_num: number
  bytes_used?: number
  percent_used?: number
}

export interface CephOSD {
  id: number
  host?: string
  status?: string
  in?: number
  up?: number
  type: string
}

// Mirrors pve.Subscription (internal/pve/datacenter.go).
export interface Subscription {
  status: string
  level?: string
  productname?: string
  nextduedate?: string
  message?: string
  serverid?: string
}

// Mirrors api.DatacenterOptions (internal/pve/datacenter.go).
export interface DatacenterOptions {
  keyboard?: string
  language?: string
  http_proxy?: string
  console?: string
  email_from?: string
  description?: string
  mac_prefix?: string
  max_workers?: number
}

// Mirrors pve.Node (internal/pve/nodes.go).
export interface Node {
  node: string
  status: string
  cpu: number
  maxcpu: number
  mem: number
  maxmem: number
  disk: number
  maxdisk: number
  uptime: number
}

// Mirrors pve.NodeStatus (internal/pve/nodes.go).
export interface NodeStatus {
  cpu: number
  wait: number
  idle: number
  cpuinfo: { model: string; cores: number; sockets: number; mhz?: string }
  memory: { total: number; used: number; free: number }
  swap: { total: number; used: number }
  rootfs: { total: number; used: number; avail: number }
  ksm: { shared: number }
  loadavg: string[]
  uptime: number
  kversion: string
  pveversion: string
}

// Mirrors pve.AptUpdate (internal/pve/nodes.go).
export interface AptUpdate {
  Package: string
  OldVersion: string
  Version: string
  Priority: string
  Description: string
}

// Mirrors pve.SyslogEntry (internal/pve/nodes.go).
export interface SyslogEntry {
  n: number
  t: string
}

// Mirrors pve.NetworkInterface (internal/pve/nodes.go).
export interface NetworkInterface {
  iface: string
  type: string
  active?: number
  address?: string
  netmask?: string
  gateway?: string
  autostart?: number
  bridge_ports?: string
}

// Mirrors pve.ClusterStatus (internal/pve/client.go).
export interface ClusterStatus {
  id: string
  type: "cluster" | "node"
  name?: string
  version?: number
  quorate?: number
  nodeid?: number
  ip?: string
  online?: number
  local?: number
}

// Mirrors pve.ClusterLogEntry (internal/pve/client.go).
export interface ClusterLogEntry {
  node: string
  msg: string
  pid: number
  tag: string
  uid: number
}

// Mirrors pve.Storage (internal/pve/storage.go).
export interface Storage {
  storage: string
  node?: string
  type: string
  content?: string
  shared?: number
  active?: number
  total?: number
  used?: number
  avail?: number
}

// Mirrors pve.StorageContentItem (internal/pve/storage.go).
export interface StorageContentItem {
  volid: string
  content: string
  format?: string
  size?: number
  vmid?: number
  ctime?: number
}

// Mirrors pve.DiskDevice / pve.NetDevice (internal/pve/guests.go).
export interface DiskDevice {
  key: string
  value: string
}

export interface NetDevice {
  key: string
  value: string
}

// Mirrors pve.GuestConfig (internal/pve/guests.go).
export interface GuestConfig {
  name?: string
  cores?: number
  sockets?: number
  memory?: number
  ostype?: string
  boot?: string
  onboot?: number
  tags?: string
  notes?: string
  raw?: Record<string, unknown>
  disks: DiskDevice[]
  networkDevices: NetDevice[]
}

// Mirrors pve.Snapshot (internal/pve/guests.go).
export interface Snapshot {
  name: string
  description?: string
  snaptime?: number
  parent?: string
  vmstate?: number
}

// Mirrors pve.AgentNetworkInterface (internal/pve/guests.go).
export interface AgentNetworkInterface {
  name: string
  "hardware-address"?: string
  "ip-addresses": string[]
}

// Mirrors pve.FirewallAlias / FirewallIPSet / FirewallIPSetEntry / FirewallOptions (internal/pve/firewall.go).
export interface FirewallAlias {
  name: string
  cidr: string
  comment?: string
}

export interface FirewallIPSet {
  name: string
  comment?: string
}

export interface FirewallIPSetEntry {
  cidr: string
  comment?: string
  nomatch?: number
}

export interface FirewallOptions {
  enable: number
}

// Mirrors api.alertRuleDTO (internal/api/alerts.go).
export interface AlertRule {
  id: string
  name: string
  metric: string
  connectionId?: string
  threshold: number
  severity: string
  enabled: boolean
  createdAt: string
}

// Mirrors api.alertInstanceDTO (internal/api/alerts.go).
export interface AlertInstance {
  id: string
  ruleId: string
  connectionId: string
  connectionName: string
  resourceName: string
  metric: string
  value: number
  threshold: number
  severity: string
  status: string
  triggeredAt: string
  updatedAt: string
  resolvedAt?: string
}
