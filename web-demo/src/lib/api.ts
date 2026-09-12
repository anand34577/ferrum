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
  type: "pve" | "pbs"
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

// Mirrors api.ForecastResult (internal/api/forecast.go).
export interface ForecastResult {
  metric: "disk" | "mem" | "cpu"
  currentPct: number
  trend: "rising" | "falling" | "flat"
  daysToWarning?: number
  daysToCritical?: number
  projectedDate90?: string
  projectedDate100?: string
  confidence: "low" | "medium" | "high"
  sampleSize: number
  rSquared: number
}

// Mirrors api.capacityWarning (internal/api/forecast.go).
export interface CapacityWarning {
  connectionId: string
  connectionName: string
  node: string
  metric: "disk" | "mem" | "cpu"
  currentPct: number
  trend: "rising" | "falling" | "flat"
  daysToWarning?: number
  daysToCritical?: number
  confidence: "low" | "medium" | "high"
}

// Mirrors api.healthComponent (internal/api/health.go).
export interface HealthComponent {
  label: string
  points: number
  max: number
}

// Mirrors api.healthScoreResult (internal/api/health.go).
export interface HealthScoreResult {
  connectionId?: string
  connectionName?: string
  score: number
  components: HealthComponent[]
}

// Mirrors the fleetHealthScore handler's response (internal/api/health.go).
export interface FleetHealthScore {
  score: number
  worst?: HealthScoreResult
  connections: HealthScoreResult[]
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
  /** Only present on the single-task status endpoint; the server normalizes
   *  it into `status`, so prefer `status` for the OK/failed verdict. */
  exitstatus?: string
}

// Mirrors pve.GuestLiveStatus (internal/pve/guests.go).
export interface GuestLiveStatus {
  status: string
  /** QEMU only: "running" | "paused" | "prelaunch" | … A suspended VM keeps
   *  status "running", so this is the only way to tell it is paused. */
  qmpstatus?: string
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
  "notification-mode"?: string
  mailto?: string
  mailnotification?: string
  bwlimit?: number
  pigz?: number
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

// Mirrors pve.HARule (internal/pve/ha.go) — Proxmox 9's HA rules
// (node-affinity / resource-affinity). Loose on purpose: rule-type-specific
// fields (resources, nodes, affinity, strict, ...) fall through here rather
// than being individually modeled, mirroring the Go side's Raw map.
export interface HARule {
  rule: string
  type: string
  comment?: string
  disable?: number
  [key: string]: unknown
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
  // Every option PVE returned, unfiltered — includes fields above by name
  // plus anything this type doesn't model (bwlimit, migration, u2f, ...).
  raw?: Record<string, unknown>
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

// Mirrors pve.JournalEntry (internal/pve/nodes.go) — the structured,
// filterable systemd journal that replaced the legacy plain-text syslog feed.
export interface JournalEntry {
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
  time: number
  pri: number
  user?: string
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

// Mirrors pve.Disk (internal/pve/disks.go).
export interface Disk {
  devpath: string
  model?: string
  serial?: string
  vendor?: string
  size: number
  type: string // "hdd" | "ssd" | "usb" | "unknown"
  rpm?: number
  wearout?: number // SSD/NVMe life remaining, percent — absent for HDDs
  health?: string // "PASSED" | "FAILED" | "UNKNOWN"
  used?: string // what's using it: "LVM", "ZFS", "partitions", "" (unused)
  wwn?: string
}

// Mirrors pve.SmartAttribute / pve.SmartData (internal/pve/disks.go).
export interface SmartAttribute {
  id?: number
  name: string
  value?: string
  worst?: string
  threshold?: string
  raw?: string
  flags?: string
  fail?: string
}
export interface SmartData {
  type: string
  health?: string
  attributes?: SmartAttribute[]
  text?: string
}

// Mirrors pve.ZFSPoolInfo / pve.LVMVolumeGroup / pve.LVMThinPool (internal/pve/disks.go).
export interface ZFSPoolInfo {
  name: string
  size?: number
  free?: number
  health?: string
}
export interface LVMVolumeGroup {
  name: string
  size?: number
  free?: number
}
export interface LVMThinPool {
  lv: string
  vg: string
  size?: number
  used?: number
}

// Mirrors pve.NFSExport / pve.CIFSShare / pve.ISCSITarget / pve.GlusterVolume (internal/pve/storage.go).
export interface NFSExport {
  path: string
  options?: string
}
export interface CIFSShare {
  share: string
  description?: string
}
export interface ISCSITarget {
  target: string
  portal?: string
}
export interface GlusterVolume {
  volname: string
}

// Mirrors pve.StorageContentItem (internal/pve/storage.go).
export interface StorageContentItem {
  volid: string
  content: string
  format?: string
  size?: number
  vmid?: number
  ctime?: number
  protected?: number
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

// Mirrors pve.GuestAgentExecResult / GuestAgentExecStatus (internal/pve/agent.go).
export interface GuestAgentExecResult {
  pid: number
}

export interface GuestAgentExecStatus {
  exited: boolean
  exitcode?: number
  signal?: number
  "out-data"?: string
  "err-data"?: string
  truncated?: boolean
}

// Mirrors the raw maps returned by pve.GuestAgentOSInfo / GuestAgentFSInfo /
// GuestAgentVCPUs (internal/pve/agent.go) — fields vary by guest OS, so these
// stay loose rather than pinning a rigid shape the backend doesn't guarantee.
export type GuestAgentOSInfo = Record<string, unknown>
export type GuestAgentFSInfo = Record<string, unknown>
export type GuestAgentVCPU = Record<string, unknown>

// Mirrors pve.CephMon / CephMgr / CephFS (internal/pve/storage.go).
export interface CephMon {
  name: string
  host?: string
  addr?: string
  quorum?: number
}

export interface CephMgr {
  host?: string
  addr?: string
  active?: number
}

export interface CephFilesystem {
  name: string
}

// Mirrors pve.FileRestoreEntry (internal/pve/storage.go).
export interface FileRestoreEntry {
  filepath: string
  type: "f" | "d"
  size?: number
  mtime?: number
}

// Mirrors pve.SDNZone / SDNVnet / SDNSubnet (internal/pve/sdn.go).
export interface SDNZone {
  zone: string
  type: string
  nodes?: string
  mtu?: number
  pending?: number
  raw?: Record<string, unknown>
}

export interface SDNVnet {
  vnet: string
  zone: string
  alias?: string
  tag?: number
  vlanaware?: number
  pending?: number
}

export interface SDNSubnet {
  subnet: string
  type: string
  gateway?: string
  snat?: number
}

export interface SDNController {
  controller: string
  type: string
  raw?: Record<string, unknown>
}

export interface SDNIPAM {
  ipam: string
  type: string
  raw?: Record<string, unknown>
}

// Mirrors pve.AccessUser / AccessUserToken / AccessRole / AccessACLEntry /
// AccessDomain (internal/pve/access.go) — Proxmox's own user/permission
// system, read-only here (distinct from this app's local accounts in User).
export interface AccessUser {
  userid: string
  enable?: number
  expire?: number
  email?: string
  firstname?: string
  lastname?: string
  comment?: string
  groups?: string
  tokens?: AccessUserToken[]
}

export interface AccessUserToken {
  tokenid: string
  comment?: string
  expire?: number
  privsep?: number
}

export interface AccessRole {
  roleid: string
  raw?: Record<string, unknown>
}

export interface AccessACLEntry {
  path: string
  roleid: string
  ugid: string
  type: string
  propagate?: number
}

export interface AccessDomain {
  realm: string
  type: string
  comment?: string
  default?: number
}

// Mirrors pve.NodeCertificate (internal/pve/certificates.go).
export interface NodeCertificate {
  filename: string
  subject?: string
  issuer?: string
  notbefore?: number
  notafter?: number
  fingerprint?: string
  san?: string[]
}

// Mirrors pve.ClusterConfigNode / ClusterJoinInfo (internal/pve/cluster_membership.go).
export interface ClusterConfigNode {
  name: string
  nodeid?: number
  quorum_votes?: number
}

export interface ClusterJoinInfo {
  fingerprint: string
  nodelist?: { name: string; pve_addr?: string }[]
  preferred_node?: string
}

// Mirrors pve.NodeDNSConfig / NodeTimeInfo / NodeHosts (internal/pve/nodes.go).
export interface NodeDNSConfig {
  search?: string
  dns1?: string
  dns2?: string
  dns3?: string
}

export interface NodeTimeInfo {
  timezone: string
  time: number
  localtime: number
}

export interface NodeHosts {
  data: string
  digest?: string
}

// Mirrors pve.NodeService (internal/pve/nodes.go).
export interface NodeService {
  service: string
  name?: string
  desc?: string
  state: string
}

// Mirrors poller.ConnectionHealth (internal/poller/alerts.go) — whether a
// connection answered the alert evaluator's last poll, tracked independently
// of alert_rules so "is the server even reachable" doesn't depend on the
// admin having configured a threshold first.
export interface ConnectionHealth {
  connectionId: string
  connectionName: string
  status: "up" | "down"
  lastError?: string
  since: string
  updatedAt: string
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

// Mirrors auth.APIKey (internal/auth/apikeys.go). scope "api" works against
// the general REST API; "mcp" works only against the MCP endpoint — the two
// are mutually exclusive by design.
export interface ApiKey {
  id: string
  name: string
  keyPrefix: string
  scope: "api" | "mcp"
  lastUsedAt?: string
  expiresAt?: string
  createdAt: string
}

// Returned once, at creation — never retrievable again.
export interface CreatedApiKey extends ApiKey {
  key: string
}

// Mirrors auth.Session (internal/auth/sessions.go) — one signed-in device,
// for the Sessions panel on ProfilePage and its admin equivalent on the
// Users page. The session token itself is never exposed.
export interface Session {
  id: string
  createdAt: string
  lastSeenAt?: string
  expiresAt: string
  ip?: string
  userAgent?: string
  current: boolean
}

// Mirrors api.aiModelDTO — one selectable model under a provider. label is
// the friendly display name; modelId is the exact identifier sent to the
// provider's API (frequently different, e.g. "GPT-4o mini" vs "gpt-4o-mini").
export interface AIModel {
  id: string
  label: string
  modelId: string
  isDefault: boolean
  createdAt: string
}

// Mirrors api.aiProviderDTO (internal/api/ai_providers.go) — admin view. A
// provider can carry any number of models.
export interface AIProvider {
  id: string
  name: string
  baseUrl: string
  hasApiKey: boolean
  isEnabled: boolean
  models: AIModel[]
  createdAt: string
  updatedAt: string
}

// Mirrors api.usableProviderDTO/usableModelDTO — what every user sees in the
// assistant's picker: enabled providers and their models only.
export interface UsableAIModel {
  id: string
  label: string
  isDefault: boolean
}
export interface UsableAIProvider {
  providerId: string
  providerName: string
  models: UsableAIModel[]
}

export interface AIChatMessage {
  role: "system" | "user" | "assistant"
  content: string
}

// Mirrors api.agentSettingsResponse (internal/api/agent_settings.go).
export interface AgentSettings {
  mcpEnabled: boolean
  apiEnabled: boolean
  maxToolIterations: number
}

// Mirrors api.toolCallDTO (internal/api/ai_activity.go).
export interface ToolCallRecord {
  id: string
  username?: string // present only in the admin, cross-user view
  source: "chat" | "mcp"
  tool: string
  args?: string
  ok: boolean
  error?: string
  createdAt: string
}
