// Stands in for the Go backend so this build of the real frontend runs
// entirely as static files (no server, no database). It patches
// `window.fetch` for every `/api/v1/...` call the app makes and answers from
// the in-memory fleet in `demoWorld.ts`, mutating that state the same way a
// real backend would for actions taken in the UI (power a guest off, silence
// an alert, add a connection, ...) — all of it resets on reload, since there
// is nothing durable behind it.
//
// Anything below not worth hand-modeling (SMART data, journal tails, ACME
// certificates, ...) falls through to a generic 200 with an empty/plausible
// body instead of a 404, so a page can render its empty state instead of an
// error banner. That's a deliberate line, not an oversight: the pages a
// visitor actually lands on carry real data; deep, rarely-opened drill-downs
// degrade gracefully instead of costing effort nobody would see.
import * as W from "./demoWorld"

type Handler = (ctx: { params: Record<string, string>; query: URLSearchParams; body: unknown }) => unknown

interface Route {
  method: string
  re: RegExp
  keys: string[]
  handler: Handler
}

const routes: Route[] = []

function compile(path: string): { re: RegExp; keys: string[] } {
  const keys: string[] = []
  const pattern = path
    .split("/")
    .map((seg) => {
      if (seg.startsWith(":")) {
        keys.push(seg.slice(1))
        return "([^/]+)"
      }
      return seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    })
    .join("/")
  return { re: new RegExp(`^${pattern}$`), keys }
}

function on(method: string, path: string, handler: Handler) {
  const { re, keys } = compile(path)
  routes.push({ method, re, keys, handler })
}

// --- small helpers -----------------------------------------------------

const jitter = (base: number, amount: number) => Math.max(0, Math.min(1, base + (Math.random() - 0.5) * amount))

function guestDTO(g: W.DemoGuest) {
  return {
    id: g.id,
    type: g.type,
    node: g.node,
    vmid: g.vmid,
    name: g.name,
    status: g.status,
    cpu: g.status === "running" ? jitter(g.cpu, 0.06) : 0,
    maxcpu: g.cores,
    mem: g.mem,
    maxmem: g.maxmem,
    disk: g.disk,
    maxdisk: g.maxdisk,
    uptime: g.uptime,
    tags: g.tags,
    hastate: g.hastate,
  }
}

function nodeDTO(n: W.DemoNode) {
  return {
    id: `node/${n.node}`,
    type: "node",
    node: n.node,
    status: n.status,
    cpu: jitter(n.cpu, 0.05),
    maxcpu: n.cores,
    mem: n.mem,
    maxmem: n.maxmem,
    disk: n.disk,
    maxdisk: n.maxdisk,
    uptime: n.uptime,
  }
}

// StoragePage reads pools out of the plain inventory resources array (type
// "storage") rather than a dedicated endpoint — a shared pool (Ceph/NFS/PBS)
// is reported once per node that mounts it (real PVE behavior) and the
// frontend dedupes those by an exact (name, total, used) match; a local pool
// is genuinely separate per node, so each gets its own row and its own numbers.
function storageResourcesFor(connId: string) {
  const connNodes = W.nodesFor(connId)
  const out: Record<string, unknown>[] = []
  for (const pool of W.storagePools.filter((p) => p.connId === connId)) {
    const targets = pool.shared ? connNodes.map((n) => n.node) : [pool.node ?? connNodes[0]?.node]
    for (const node of targets) {
      if (!node) continue
      out.push({
        id: `storage/${node}/${pool.id}`,
        type: "storage",
        node,
        storage: pool.id,
        plugintype: pool.type,
        shared: pool.shared ? 1 : 0,
        maxdisk: pool.total,
        disk: pool.used,
      })
    }
  }
  return out
}

function inventoryFor(connId: string) {
  const conn = W.findConn(connId)
  if (!conn) return null
  return {
    connectionId: conn.id,
    name: conn.name,
    online: true,
    resources: [...W.nodesFor(connId).map(nodeDTO), ...W.guestsFor(connId).map(guestDTO), ...storageResourcesFor(connId)],
  }
}

function fleetOverview() {
  return W.connections.map((c) => {
    const gs = W.guestsFor(c.id)
    const vms = gs.filter((g) => g.type === "qemu")
    const lxcs = gs.filter((g) => g.type === "lxc")
    return {
      connectionId: c.id,
      name: c.name,
      host: c.host,
      port: c.port,
      online: true,
      latencyMs: 8 + Math.round(Math.random() * 12),
      checkedAt: new Date().toISOString(),
      cluster: c.clusterName ? { name: c.clusterName, quorate: c.quorate, nodes: W.nodesFor(c.id).length } : undefined,
      nodes: { total: W.nodesFor(c.id).length, online: W.nodesFor(c.id).length, cores: c.cores },
      vms: { total: vms.length, running: vms.filter((g) => g.status === "running").length, stopped: vms.filter((g) => g.status === "stopped").length },
      lxcs: { total: lxcs.length, running: lxcs.filter((g) => g.status === "running").length, stopped: lxcs.filter((g) => g.status === "stopped").length },
      templates: 0,
      haGuests: W.haResources.filter((h) => h.connId === c.id).length,
      cpu: { cores: c.cores, usedCores: jitter(c.coresUsed / c.cores, 0.03) * c.cores, pct: jitter(c.coresUsed / c.cores, 0.03) * 100 },
      memory: { total: c.memTotal, used: c.memUsed, pct: (c.memUsed / c.memTotal) * 100 },
      storage: { total: c.storageTotal, used: c.storageUsed, pct: (c.storageUsed / c.storageTotal) * 100, byType: { ceph: c.storageTotal * 0.6, lvmthin: c.storageTotal * 0.4 } },
      alerts: {
        critical: W.alerts.filter((a) => a.connectionId === c.id && a.status === "active" && a.severity === "critical").length,
        warning: W.alerts.filter((a) => a.connectionId === c.id && a.status === "active" && a.severity === "warning").length,
      },
    }
  })
}

function rrdSeries(base: number, points = 60, stepSec = 60) {
  const t0 = Math.floor(Date.now() / 1000) - points * stepSec
  return Array.from({ length: points }, (_, i) => ({
    time: t0 + i * stepSec,
    cpu: jitter(base, 0.15),
    mem: jitter(base * 0.9, 0.1) * 100,
    maxmem: 100,
    netin: Math.random() * 5_000_000,
    netout: Math.random() * 2_000_000,
    diskread: Math.random() * 1_000_000,
    diskwrite: Math.random() * 1_000_000,
  }))
}

// --- auth ---------------------------------------------------------------

on("GET", "/auth/setup-status", () => ({ needsSetup: false }))
on("GET", "/auth/me", () => ({ id: W.currentUser.id, username: W.currentUser.username, email: W.currentUser.email, isAdmin: true, totpEnabled: W.currentUser.totpEnabled }))
on("POST", "/auth/logout", () => ({}))
on("GET", "/auth/oidc/config", () => ({ enabled: false }))
on("GET", "/auth/agent-status", () => ({ mcpEnabled: true, apiEnabled: true }))
on("GET", "/auth/2fa/status", () => ({ enabled: true, remainingRecoveryCodes: 8 }))

let preferences: Record<string, unknown> = { theme: "system", accent: "oxide", look: "enterprise", density: "comfortable", activeDashboardId: "dash-default" }
on("GET", "/auth/me/preferences", () => preferences)
on("PUT", "/auth/me/preferences", ({ body }) => {
  preferences = { ...preferences, ...(body as object) }
  return preferences
})

const apiKeys = [{ id: "key-1", name: "grafana-export", keyPrefix: "frm_7f2a", scope: "api" as const, createdAt: new Date(Date.now() - 40 * 86400000).toISOString(), lastUsedAt: new Date(Date.now() - 3600000).toISOString() }]
on("GET", "/auth/apikeys/", () => apiKeys)
on("POST", "/auth/apikeys/", ({ body }) => {
  const b = body as { name: string; scope: "api" | "mcp" }
  const created = { id: `key-${apiKeys.length + 1}`, name: b.name, scope: b.scope, keyPrefix: "frm_" + Math.random().toString(16).slice(2, 6), createdAt: new Date().toISOString(), key: "frm_" + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2) }
  apiKeys.push(created)
  return created
})
on("DELETE", "/auth/apikeys/:id", ({ params }) => {
  const i = apiKeys.findIndex((k) => k.id === params.id)
  if (i >= 0) apiKeys.splice(i, 1)
  return {}
})

const sessions = [
  { id: "sess-1", createdAt: new Date(Date.now() - 3 * 3600000).toISOString(), lastSeenAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 20 * 3600000).toISOString(), ip: "10.20.0.4", userAgent: "This browser", current: true },
]
on("GET", "/profile/sessions/", () => sessions)

// --- fleet-wide ----------------------------------------------------------

on("GET", "/overview", () => fleetOverview())
on("GET", "/connections/", () =>
  W.connections.map((c) => ({
    id: c.id,
    name: c.name,
    type: c.type,
    host: c.host,
    port: c.port,
    authType: c.authType,
    username: c.username,
    tokenId: c.tokenId,
    verifyTls: c.verifyTls,
    behindReverseProxy: c.behindReverseProxy,
    createdAt: c.createdAt,
  })),
)
on("POST", "/connections/test", () => ({ status: "ok", version: "8.3.1" }))
on("POST", "/connections/", ({ body }) => {
  const b = body as Partial<W.DemoConnection>
  const id = `c-${Math.random().toString(36).slice(2, 7)}`
  W.connections.push({
    id,
    name: b.name || "new-connection",
    type: (b.type as "pve" | "pbs") || "pve",
    host: b.host || "0.0.0.0",
    port: b.port || 8006,
    authType: (b.authType as "token" | "password") || "token",
    tokenId: b.tokenId,
    username: b.username,
    verifyTls: b.verifyTls ?? true,
    behindReverseProxy: false,
    createdAt: new Date().toISOString(),
    quorate: true,
    cores: 8,
    coresUsed: 0.4,
    memTotal: 32 * 1024 ** 3,
    memUsed: 4 * 1024 ** 3,
    storageTotal: 500 * 1024 ** 3,
    storageUsed: 40 * 1024 ** 3,
  })
  return { id }
})
on("PUT", "/connections/:id", ({ params, body }) => {
  const c = W.findConn(params.id)
  if (c) Object.assign(c, body as object)
  return {}
})
on("DELETE", "/connections/:id", ({ params }) => {
  const i = W.connections.findIndex((c) => c.id === params.id)
  if (i >= 0) W.connections.splice(i, 1)
  return {}
})

on("GET", "/inventory/", () => W.connections.map((c) => inventoryFor(c.id)))

on("GET", "/health-score", () => ({
  score: 94,
  connections: W.connections.map((c) => ({ connectionId: c.id, connectionName: c.name, score: c.id === "c-branch" ? 86 : 96, components: healthComponents() })),
}))
on("GET", "/connections/:id/health-score", ({ params }) => {
  const c = W.findConn(params.id)
  return { connectionId: c?.id, connectionName: c?.name, score: c?.id === "c-branch" ? 86 : 96, components: healthComponents() }
})
function healthComponents() {
  return [
    { label: "Availability", points: 28, max: 30 },
    { label: "Capacity headroom", points: 22, max: 25 },
    { label: "Backup coverage", points: 24, max: 25 },
    { label: "Alerts", points: 18, max: 20 },
  ]
}

on("GET", "/forecast/capacity-warnings", () => [
  { connectionId: "c-branch", connectionName: "branch-office", node: "pve-branch-02", metric: "disk", currentPct: 88, trend: "rising", daysToWarning: 0, daysToCritical: 12, confidence: "high" },
])

// --- alerts ---------------------------------------------------------------

on("GET", "/alerts/summary", () => ({
  warning: W.alerts.filter((a) => a.status === "active" && a.severity === "warning").length,
  critical: W.alerts.filter((a) => a.status === "active" && a.severity === "critical").length,
}))
on("GET", "/alerts/", () => W.alerts)
on("POST", "/alerts/:id/silence", ({ params }) => {
  const a = W.alerts.find((x) => x.id === params.id)
  if (a) a.status = "resolved"
  return {}
})
on("GET", "/alert-rules/", () => W.alertRules)
on("POST", "/alert-rules/", ({ body }) => {
  const b = body as Partial<W.DemoAlertRule>
  W.alertRules.push({ id: `rule-${W.alertRules.length + 1}`, name: b.name || "New rule", metric: b.metric || "cpu", threshold: b.threshold ?? 90, severity: (b.severity as "warning" | "critical") || "warning", enabled: true, createdAt: new Date().toISOString(), connectionId: b.connectionId })
  return {}
})
on("DELETE", "/alert-rules/:id", ({ params }) => {
  const i = W.alertRules.findIndex((r) => r.id === params.id)
  if (i >= 0) W.alertRules.splice(i, 1)
  return {}
})
on("GET", "/connection-health", () => W.connections.map((c) => ({ connectionId: c.id, connectionName: c.name, status: "up" as const, since: c.createdAt, updatedAt: new Date().toISOString() })))

// --- guest actions ---------------------------------------------------------

on("POST", "/connections/:conn/guests/:type/:node/:vmid/power/:action", ({ params }) => {
  const g = W.guests.find((x) => x.connId === params.conn && x.vmid === Number(params.vmid))
  if (g) {
    if (params.action === "stop" || params.action === "shutdown") {
      g.status = "stopped"
      g.cpu = 0
      g.mem = 0
      g.uptime = 0
    } else if (params.action === "start") {
      g.status = "running"
      g.uptime = 1
    }
  }
  return {}
})
on("POST", "/connections/:conn/guests/:type/:node/:vmid/migrate", () => ({ upid: "UPID:demo:migrate" }))
on("POST", "/connections/:conn/guests/:type/:node/:vmid/console", () => ({ wsPath: "/ws/unavailable-in-demo" }))
on("POST", "/connections/:conn/guests/:type/:node/:vmid/shell", () => ({ wsPath: "/ws/unavailable-in-demo" }))
on("GET", "/connections/:conn/guests/:type/:node/:vmid/status", ({ params }) => {
  const g = W.guests.find((x) => x.connId === params.conn && x.vmid === Number(params.vmid))
  return g ? { status: g.status, cpu: g.cpu, cpus: g.cores, mem: g.mem, maxmem: g.maxmem, uptime: g.uptime } : {}
})
on("POST", "/bulk/guests/action", ({ body }) => {
  const b = body as { targets: { connId: string; vmid: number }[]; action: string }
  return (b.targets || []).map((t) => {
    const g = W.guests.find((x) => x.connId === t.connId && x.vmid === t.vmid)
    if (g && (b.action === "stop" || b.action === "start")) g.status = b.action === "start" ? "running" : "stopped"
    return { vmid: t.vmid, ok: true }
  })
})

// --- node detail -----------------------------------------------------------

on("GET", "/connections/:conn/nodes/:node/status", ({ params }) => {
  const n = W.findNode(params.conn, params.node)
  if (!n) return {}
  return {
    cpu: jitter(n.cpu, 0.05),
    wait: 0.01,
    idle: 1 - n.cpu,
    cpuinfo: { model: "AMD EPYC 7302P 16-Core Processor", cores: n.cores, sockets: 1 },
    memory: { total: n.maxmem, used: n.mem, free: n.maxmem - n.mem },
    swap: { total: 8 * 1024 ** 3, used: 0 },
    rootfs: { total: n.maxdisk, used: n.disk, avail: n.maxdisk - n.disk },
    ksm: { shared: 0 },
    loadavg: ["1.24", "1.10", "0.98"],
    uptime: n.uptime,
    kversion: "Linux 6.8.12-4-pve",
    pveversion: "pve-manager/8.3.1/2b8d5e6f",
  }
})
on("GET", "/connections/:conn/nodes/:node/rrddata", ({ params }) => rrdSeries(W.findNode(params.conn, params.node)?.cpu ?? 0.3))
on("GET", "/connections/:conn/nodes/:node/storage", ({ params }) =>
  W.storagePools.filter((p) => p.connId === params.conn).map((p) => ({ storage: p.id, node: params.node, type: p.type, shared: p.shared ? 1 : 0, active: 1, total: p.total, used: p.used, avail: p.total - p.used })),
)
on("GET", "/connections/:conn/nodes/:node/disks", () => [
  { devpath: "/dev/sda", model: "Samsung SSD 870 EVO", size: 1_024_000_000_000, type: "ssd", wearout: 97, health: "PASSED", used: "LVM" },
  { devpath: "/dev/sdb", model: "Seagate Exos X18", size: 18_000_000_000_000, type: "hdd", health: "PASSED", used: "ZFS" },
])
on("GET", "/connections/:conn/nodes/:node/network", () => [
  { iface: "vmbr0", type: "bridge", active: 1, address: "10.20.0.11", netmask: "255.255.255.0", gateway: "10.20.0.1", autostart: 1, bridge_ports: "eno1" },
])
on("GET", "/connections/:conn/nodes/:node/apt/updates", () => [])
on("GET", "/connections/:conn/nodes/:node/journal", () => {
  const lines = [
    "pvedaemon[1042]: <admin@pve> starting task",
    "pve-firewall[988]: firewall update time (5.012 seconds)",
    "pvestatd[1011]: status update time (0.412 seconds)",
    "systemd[1]: Started PVE guest agent bridge.",
  ]
  return lines.map((t, i) => ({ n: i, t: `${new Date(Date.now() - (lines.length - i) * 60000).toISOString()} pve-core-01 ${t}` }))
})
on("GET", "/connections/:conn/nodes/:node/subscription", () => ({ status: "notfound", message: "There is no subscription key" }))
on("POST", "/connections/:conn/nodes/:node/reboot", () => ({ upid: "UPID:demo:reboot" }))
on("POST", "/connections/:conn/nodes/:node/shutdown", () => ({ upid: "UPID:demo:shutdown" }))

// --- storage / ceph ---------------------------------------------------------

on("GET", "/connections/:conn/nodes/:node/ceph/status", () => ({
  health: { status: W.cephStatus.health },
  pgmap: { bytes_used: W.cephStatus.objects * 4_000_000, bytes_total: 19 * 1024 ** 4, bytes_avail: 8.2 * 1024 ** 4, num_pgs: W.cephStatus.pgs },
  osdmap: { num_osds: W.cephStatus.osds.total, num_up_osds: W.cephStatus.osds.up, num_in_osds: W.cephStatus.osds.in },
}))
on("GET", "/connections/:conn/nodes/:node/ceph/pools", () => [{ pool_name: "rbd-vm-pool", size: 3, min_size: 2, pg_num: 512, bytes_used: 10.8 * 1024 ** 4, percent_used: 57 }])
on("GET", "/connections/:conn/nodes/:node/ceph/osds", () =>
  Array.from({ length: W.cephStatus.osds.total }, (_, i) => ({ id: i, host: `pve-core-0${(i % 3) + 1}`, status: "up", in: 1, up: 1, type: "bluestore" })),
)

// --- pools -------------------------------------------------------------------

on("GET", "/connections/:conn/pools/", ({ params }) => {
  if (params.conn !== "c-prod") return []
  return [{ poolid: "kubernetes", comment: "k8s worker fleet" }, { poolid: "databases", comment: "" }]
})
on("GET", "/connections/:conn/pools/:id", ({ params }) => {
  const members = W.guestsFor(params.conn)
    .filter((g) => (params.id === "kubernetes" ? g.tags?.includes("kubernetes") : g.tags?.includes("database")))
    .map(guestDTO)
  return { poolid: params.id, comment: "", members }
})

// --- backups / replication ----------------------------------------------------

on("GET", "/connections/:conn/cluster/backup-jobs", ({ params }) =>
  W.backupJobs.filter((j) => j.connId === params.conn).map((j) => ({ id: j.id, schedule: j.schedule, storage: j.storage, mode: j.mode, enabled: 1 })),
)
on("POST", "/connections/:conn/cluster/backup-jobs/run", () => ({ upid: "UPID:demo:backup" }))
on("POST", "/connections/:conn/cluster/backup-jobs", () => ({}))
on("DELETE", "/connections/:conn/cluster/backup-jobs/:id", () => ({}))
on("GET", "/connections/:conn/cluster/replication-jobs", ({ params }) =>
  W.replicationJobs.filter((j) => j.connId === params.conn).map((j) => ({ id: j.id, type: "local", target: j.target, schedule: j.schedule, guest: j.guest })),
)
on("GET", "/connections/:conn/nodes/:node/replication", ({ params }) =>
  W.replicationJobs
    .filter((j) => j.connId === params.conn)
    .map((j) => ({ id: j.id, last_sync: j.lastSync, next_sync: j.lastSync + 4 * 3600, duration: j.durationSec })),
)
on("POST", "/connections/:conn/nodes/:node/replication/:id/run", () => ({}))

// --- HA -------------------------------------------------------------------

on("GET", "/connections/:conn/cluster/ha/resources", ({ params }) => W.haResources.filter((h) => h.connId === params.conn).map((h) => ({ sid: h.sid, state: h.state, group: h.group })))
on("GET", "/connections/:conn/cluster/ha/groups", ({ params }) => W.haGroups.filter((g) => g.connId === params.conn).map((g) => ({ group: g.group, nodes: g.nodes, restricted: g.restricted, nofailback: g.nofailback })))
on("GET", "/connections/:conn/cluster/ha/status", ({ params }) => W.haResources.filter((h) => h.connId === params.conn).map((h) => ({ id: h.sid, type: "service", status: h.state, node: h.node })))
on("GET", "/connections/:conn/cluster/ha/rules", () => [])
on("POST", "/connections/:conn/cluster/ha/resources", ({ params, body }) => {
  const b = body as { sid: string; group?: string }
  W.haResources.push({ sid: b.sid, connId: params.conn, group: b.group || "", state: "started", node: W.nodesFor(params.conn)[0]?.node || "" })
  return {}
})
on("DELETE", "/connections/:conn/cluster/ha/resources/:sid", ({ params }) => {
  const i = W.haResources.findIndex((h) => h.connId === params.conn && h.sid === params.sid)
  if (i >= 0) W.haResources.splice(i, 1)
  return {}
})
on("POST", "/connections/:conn/cluster/ha/groups", ({ params, body }) => {
  const b = body as { group: string; nodes: string }
  W.haGroups.push({ group: b.group, connId: params.conn, nodes: b.nodes, restricted: 0, nofailback: 0 })
  return {}
})
on("DELETE", "/connections/:conn/cluster/ha/groups/:group", ({ params }) => {
  const i = W.haGroups.findIndex((g) => g.connId === params.conn && g.group === params.group)
  if (i >= 0) W.haGroups.splice(i, 1)
  return {}
})

// --- firewall / SDN --------------------------------------------------------

on("GET", "/connections/:conn/cluster/firewall/options", () => ({ enable: 1 }))
on("PUT", "/connections/:conn/cluster/firewall/options", () => ({}))
on("GET", "/connections/:conn/cluster/firewall/rules", () => W.firewallRules)
on("GET", "/connections/:conn/cluster/firewall/aliases", () => [])
on("GET", "/connections/:conn/cluster/firewall/ipsets", () => [])
on("GET", "/connections/:conn/cluster/sdn/zones", ({ params }) => (params.conn === "c-prod" ? W.sdnZones : []))
on("GET", "/connections/:conn/cluster/sdn/vnets", ({ params }) => (params.conn === "c-prod" ? W.sdnVnets : []))
on("GET", "/connections/:conn/cluster/sdn/controllers", () => [])
on("GET", "/connections/:conn/cluster/sdn/ipams", () => [])
on("GET", "/connections/:conn/cluster/config/nodes", ({ params }) => W.nodesFor(params.conn).map((n, i) => ({ name: n.node, nodeid: i + 1, quorum_votes: 1 })))

// --- tasks ------------------------------------------------------------------

const taskVerbs = ["qmstart", "qmstop", "vzdump", "qmigrate", "vzcreate"]
function fakeTasks(connId: string) {
  const gs = W.guestsFor(connId)
  return Array.from({ length: 6 }, (_, i) => {
    const g = gs[i % gs.length]
    const start = Math.floor(Date.now() / 1000) - i * 900 - 60
    return { upid: `UPID:${connId}:${1000 + i}:demo:${taskVerbs[i % taskVerbs.length]}:${g?.vmid ?? ""}:admin@pam:`, node: g?.node ?? W.nodesFor(connId)[0]?.node, type: taskVerbs[i % taskVerbs.length], status: i === 2 ? "running" : "OK", user: "admin@pam", starttime: start, endtime: i === 2 ? undefined : start + 45, id: String(g?.vmid ?? "") }
  })
}
on("GET", "/connections/:conn/cluster/tasks", ({ params }) => fakeTasks(params.conn))
on("GET", "/connections/:conn/nodes/:node/tasks/:upid/log", () => ["INFO: starting task", "INFO: 100% complete", "TASK OK"])
on("DELETE", "/connections/:conn/nodes/:node/tasks/:upid", () => ({}))

// --- dashboards ---------------------------------------------------------------

const defaultDashboard = {
  id: "dash-default",
  name: "Fleet Overview",
  version: 2,
  widgets: [
    { id: "w1", type: "health-score", x: 0, y: 0, w: 4, h: 8 },
    { id: "w2", type: "cpu-by-node", x: 4, y: 0, w: 4, h: 8, settings: { connection: "all" } },
    { id: "w3", type: "memory-by-node", x: 8, y: 0, w: 4, h: 8, settings: { connection: "all" } },
    { id: "w4", type: "alert-activity", x: 0, y: 8, w: 4, h: 8 },
    { id: "w5", type: "fleet-trend", x: 4, y: 8, w: 8, h: 8, settings: { timeframe: "hour" } },
    { id: "w6", type: "capacity-forecast", x: 0, y: 16, w: 6, h: 8, settings: { horizonDays: "30" } },
    { id: "w7", type: "cluster-comparison", x: 6, y: 16, w: 6, h: 10, settings: { sort: "cpu" } },
  ],
}
const dashboards = new Map<string, typeof defaultDashboard>([["dash-default", defaultDashboard]])
on("GET", "/dashboards", () => [...dashboards.values()].map((d) => ({ id: d.id, name: d.name, updatedAt: new Date().toISOString() })))
on("GET", "/dashboards/:id", ({ params }) => dashboards.get(params.id) ?? null)
on("POST", "/dashboards/", ({ body }) => {
  const b = body as { name: string }
  const id = `dash-${dashboards.size + 1}`
  const d = { ...defaultDashboard, id, name: b.name }
  dashboards.set(id, d)
  return d
})
on("PUT", "/dashboards/:id", ({ params, body }) => {
  const d = dashboards.get(params.id)
  if (d) Object.assign(d, body as object)
  return d
})
on("DELETE", "/dashboards/:id", ({ params }) => {
  dashboards.delete(params.id)
  return {}
})

// --- users / audit -------------------------------------------------------------

on("GET", "/users/", () => W.users)
on("GET", "/roles", () => [{ roleid: "admin" }, { roleid: "member" }])
on("POST", "/users/", ({ body }) => {
  const b = body as Partial<W.DemoUser>
  W.users.push({ id: `u-${W.users.length + 1}`, username: b.username || "new-user", email: b.email || "", isAdmin: !!b.isAdmin, totpEnabled: false, createdAt: new Date().toISOString() })
  return {}
})
on("PUT", "/users/:id", ({ params, body }) => {
  const u = W.users.find((x) => x.id === params.id)
  if (u) Object.assign(u, body as object)
  return {}
})
on("DELETE", "/users/:id", ({ params }) => {
  const i = W.users.findIndex((u) => u.id === params.id)
  if (i >= 0) W.users.splice(i, 1)
  return {}
})

on("GET", "/audit", () => W.auditLog)

// --- webhooks --------------------------------------------------------------

on("GET", "/settings/webhooks/", () => W.webhooks)
on("POST", "/settings/webhooks/", ({ body }) => {
  const b = body as Partial<(typeof W.webhooks)[number]>
  const created = { id: `wh-${W.webhooks.length + 1}`, name: b.name || "New webhook", url: b.url || "", eventTypes: b.eventTypes || [], active: true, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }
  W.webhooks.push(created)
  return created
})
on("PUT", "/settings/webhooks/:id/", ({ params, body }) => {
  const w = W.webhooks.find((x) => x.id === params.id)
  if (w) Object.assign(w, body as object)
  return w
})
on("DELETE", "/settings/webhooks/:id/", ({ params }) => {
  const i = W.webhooks.findIndex((w) => w.id === params.id)
  if (i >= 0) W.webhooks.splice(i, 1)
  return {}
})
on("POST", "/settings/webhooks/:id/test", () => ({ delivered: true }))
on("GET", "/settings/webhooks/:id/deliveries", () => [
  { id: "d-1", eventType: "alert.triggered", eventId: "evt-1", attempt: 1, statusCode: 200, success: true, createdAt: new Date(Date.now() - 3600000).toISOString() },
  { id: "d-2", eventType: "backup.failed", eventId: "evt-2", attempt: 1, statusCode: 200, success: true, createdAt: new Date(Date.now() - 86400000).toISOString() },
])

// --- settings ------------------------------------------------------------------

on("GET", "/admin/settings/agent", () => ({ mcpEnabled: true, apiEnabled: true, maxToolIterations: 12 }))
on("PUT", "/admin/settings/agent", ({ body }) => body)
on("GET", "/admin/settings/ai/providers/", () => [
  { id: "ai-needle", name: "Needle 2 (built-in, local)", baseUrl: "local://needle2", hasApiKey: false, isEnabled: true, models: [{ id: "m-1", label: "needle2-45m", modelId: "needle2", isDefault: true, createdAt: new Date().toISOString() }], createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
])
on("GET", "/ai/providers", () => [{ providerId: "ai-needle", providerName: "Needle 2 (built-in, local)", models: [{ id: "m-1", label: "needle2-45m", isDefault: true }] }])
on("GET", "/ai/activity", () => [])
on("GET", "/admin/ai/activity", () => [])
on("GET", "/admin/settings/system", () => ({ instanceName: "Ferrum", timezone: "UTC" }))
on("PUT", "/admin/settings/system", ({ body }) => body)
on("GET", "/admin/settings/security", () => ({ requireTotpForAdmins: true, sessionTimeoutMinutes: 720 }))
on("PUT", "/admin/settings/security", ({ body }) => body)
on("GET", "/admin/settings/oidc", () => ({ enabled: false }))
on("PUT", "/admin/settings/oidc", ({ body }) => body)
on("GET", "/admin/settings/notifications", () => ({ emailEnabled: false }))
on("PUT", "/admin/settings/notifications", ({ body }) => body)
on("POST", "/admin/settings/notifications/test", () => ({ delivered: true }))
on("GET", "/admin/settings/defaults", () => ({ defaultLook: "enterprise", defaultAccent: "oxide" }))
on("PUT", "/admin/settings/defaults", ({ body }) => body)
on("GET", "/settings/lifecycle", () => ({ enabled: false }))
on("PUT", "/settings/lifecycle", ({ body }) => body)
on("GET", "/admin/settings/digest", () => ({ enabled: true, schedule: "08:00 daily", recipients: ["admin@example.com"] }))
on("PUT", "/admin/settings/digest", ({ body }) => body)
on("POST", "/admin/settings/digest/send-now", () => ({ sent: true }))
on("GET", "/connections/:conn/cluster/options", () => ({ keyboard: "en-us", console: "html5" }))
on("PUT", "/connections/:conn/cluster/options", () => ({}))

// --- search ------------------------------------------------------------------

on("GET", "/search", ({ query }) => {
  const q = (query.get("q") || "").toLowerCase()
  if (!q) return []
  return W.guests
    .filter((g) => g.name.toLowerCase().includes(q))
    .slice(0, 8)
    .map((g) => ({ type: g.type, name: g.name, node: g.node, connectionId: g.connId, vmid: g.vmid }))
})

// --- guest detail dialog -----------------------------------------------------

on("GET", "/connections/:conn/guests/:type/:node/:vmid/config", ({ params }) => {
  const g = W.guests.find((x) => x.connId === params.conn && x.vmid === Number(params.vmid))
  return { name: g?.name, cores: g?.cores, sockets: 1, memory: (g?.maxmem ?? 0) / 1024 ** 2, ostype: params.type === "qemu" ? "l26" : undefined, onboot: 1, disks: [{ key: "scsi0", value: `local-lvm:vm-${params.vmid}-disk-0,size=${((g?.maxdisk ?? 0) / 1024 ** 3).toFixed(0)}G` }], networkDevices: [{ key: "net0", value: "virtio=BC:24:11:00:00:01,bridge=vmbr0" }] }
})
on("GET", "/connections/:conn/guests/:type/:node/:vmid/snapshots", () => [{ name: "pre-upgrade", snaptime: Math.floor(Date.now() / 1000) - 8 * 86400 }, { name: "current", snaptime: Math.floor(Date.now() / 1000) - 60 }])
on("GET", "/connections/:conn/guests/:type/:node/:vmid/backups", () => [])
on("GET", "/connections/:conn/guests/:type/:node/:vmid/rrddata", ({ params }) => {
  const g = W.guests.find((x) => x.connId === params.conn && x.vmid === Number(params.vmid))
  return rrdSeries(g?.cpu ?? 0.2)
})
on("POST", "/connections/:conn/guests/:type/:node/:vmid/agent/ping", () => ({}))
on("GET", "/connections/:conn/guests/:type/:node/:vmid/agent/network", () => [{ name: "eth0", "hardware-address": "bc:24:11:00:00:01", "ip-addresses": ["10.20.0.42"] }])
on("GET", "/connections/:conn/guests/:type/:node/:vmid/agent/hostname", ({ params }) => {
  const g = W.guests.find((x) => x.connId === params.conn && x.vmid === Number(params.vmid))
  return { hostname: g?.name ?? "guest" }
})
on("GET", "/connections/:conn/guests/:type/:node/:vmid/agent/timezone", () => ({ zone: "UTC" }))

// --- generic fallback ------------------------------------------------------

function matchRoute(method: string, path: string) {
  for (const r of routes) {
    if (r.method !== method) continue
    const m = r.re.exec(path)
    if (!m) continue
    const params: Record<string, string> = {}
    r.keys.forEach((k, i) => (params[k] = decodeURIComponent(m[i + 1])))
    return { route: r, params }
  }
  return null
}

function jsonResponse(status: number, body: unknown) {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const LATENCY_MS = 90

// The real app also opens an SSE stream (GET /api/v1/events) for live push
// on top of polling — see useEventStream.ts. There's nothing to stream here,
// and EventSource doesn't go through window.fetch, so it needs its own
// no-op stand-in: an idle connection that never opens and never errors,
// which is exactly what "no live events" should look like (the app already
// falls back to its own polling for everything that matters).
class NullEventSource extends EventTarget {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: (() => void) | null = null
  onerror: (() => void) | null = null
  close() {
    this.readyState = 2
  }
}

// The AI Assistant streams its reply as OpenAI-style SSE chunks over a plain
// fetch (see AIAssistantPage.tsx's parseSSELine) rather than going through
// `api.ts`, so it needs its own handler here instead of a route-table entry —
// a canned answer, picked by keyword from the fleet state above, streamed a
// few words at a time the same shape a real provider would send.
function chatAnswerFor(question: string): string {
  const q = question.toLowerCase()
  const activeAlerts = W.alerts.filter((a) => a.status === "active")
  const stopped = W.guests.filter((g) => g.status === "stopped")
  if (q.includes("health") || q.includes("overall")) {
    return `Fleet health looks good overall. prod-cluster and dr-standby are both scoring 96/100; branch-office is the one to watch at 86/100, mainly because of ${W.alerts.find((a) => a.connectionId === "c-branch")?.resourceName ?? "its storage headroom"}. Across all three connections you're running ${W.guests.filter((g) => g.status === "running").length} of ${W.guests.length} guests, with ${activeAlerts.length} active alert${activeAlerts.length === 1 ? "" : "s"}.`
  }
  if (q.includes("stopped") || q.includes("down")) {
    if (stopped.length === 0) return "Every guest across all three connections is currently running — nothing stopped right now."
    return `${stopped.length} guest${stopped.length === 1 ? " is" : "s are"} stopped right now: ${stopped.map((g) => `${g.name} (${g.node})`).join(", ")}.`
  }
  if (q.includes("alert")) {
    if (activeAlerts.length === 0) return "No active alerts right now — the fleet is quiet."
    return `${activeAlerts.length} active alert${activeAlerts.length === 1 ? "" : "s"}: ${activeAlerts.map((a) => `${a.severity} on ${a.resourceName} (${a.connectionName})`).join("; ")}.`
  }
  if (q.includes("backup")) {
    return `${W.backupJobs.length} scheduled backup jobs are configured. The most recent run was ${W.backupJobs[0].id} on ${W.backupJobs[0].storage}, which finished ${W.backupJobs[0].lastStatus === "ok" ? "successfully" : "with a warning"}.`
  }
  if (q.includes("storage") || q.includes("disk") || q.includes("space")) {
    const pool = W.storagePools.reduce((a, b) => (b.used / b.total > a.used / a.total ? b : a))
    const totalUsed = W.connections.reduce((s, c) => s + c.storageUsed, 0)
    const totalCap = W.connections.reduce((s, c) => s + c.storageTotal, 0)
    return `Fullest pool right now is ${pool.id} at ${Math.round((pool.used / pool.total) * 100)}% used. Fleet-wide storage is at ${Math.round((totalUsed / totalCap) * 100)}%.`
  }
  return `I can see ${W.connections.length} connections, ${W.nodes.length} nodes, and ${W.guests.length} guests across the fleet. Ask me about fleet health, stopped guests, active alerts, backups, or storage headroom and I'll pull the current numbers.`
}

function sseChunks(text: string): string[] {
  const words = text.split(" ")
  const out: string[] = []
  for (let i = 0; i < words.length; i += 2) {
    const piece = words.slice(i, i + 2).join(" ") + (i + 2 < words.length ? " " : "")
    out.push(`data: ${JSON.stringify({ choices: [{ delta: { content: piece } }] })}\n\n`)
  }
  out.push("data: [DONE]\n\n")
  return out
}

function fakeChatResponse(body: unknown): Response {
  const messages = (body as { messages?: { role: string; content: string }[] })?.messages ?? []
  const lastUser = [...messages].reverse().find((m) => m.role === "user")?.content ?? ""
  const chunks = sseChunks(chatAnswerFor(lastUser))
  const stream = new ReadableStream<Uint8Array>({
    async start(controller) {
      const enc = new TextEncoder()
      for (const chunk of chunks) {
        await new Promise((r) => setTimeout(r, 45))
        controller.enqueue(enc.encode(chunk))
      }
      controller.close()
    },
  })
  return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } })
}

export function installDemoApi() {
  const RealEventSource = window.EventSource
  window.EventSource = function (url: string | URL, init?: EventSourceInit) {
    const u = typeof url === "string" ? url : url.toString()
    if (u.includes("/api/v1/events")) return new NullEventSource() as unknown as EventSource
    return new RealEventSource(url, init)
  } as unknown as typeof EventSource

  const realFetch = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    const marker = "/api/v1"
    const idx = url.indexOf(marker)
    if (idx === -1) return realFetch(input, init)

    const rest = url.slice(idx + marker.length)
    const [rawPath, rawQuery] = rest.split("?")
    const path = rawPath.replace(/\/+$/, "") || "/"
    const query = new URLSearchParams(rawQuery || "")
    const method = (init?.method || "GET").toUpperCase()

    let body: unknown
    if (init?.body && typeof init.body === "string") {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = undefined
      }
    }

    if (method === "POST" && path === "/ai/chat") return fakeChatResponse(body)

    await new Promise((r) => setTimeout(r, LATENCY_MS))

    // Try exact path, then the same path with a trailing slash (routes are
    // declared inconsistently — mirroring the real API's own conventions).
    const found = matchRoute(method, path) ?? matchRoute(method, path + "/") ?? matchRoute(method, path.replace(/\/$/, ""))

    if (found) {
      try {
        const result = found.route.handler({ params: found.params, query, body })
        if (result === null) return jsonResponse(404, { error: "not found" })
        return jsonResponse(200, result)
      } catch (err) {
        console.error("[demo api] handler failed for", method, path, err)
        return jsonResponse(200, method === "GET" ? [] : {})
      }
    }

    // Unmodeled endpoint: default to a harmless empty success instead of a
    // 404 wall, so pages render an empty state rather than an error banner.
    return jsonResponse(200, method === "GET" ? [] : {})
  }
}
