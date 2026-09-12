// The in-memory fleet this demo build shows. Numbers here are kept in sync
// with the marketing site's screenshots (docs/screenshots/dashboard.png) so
// the demo, the docs, and the actual product renders all agree with each
// other. Everything is session-only — a reload resets it, on purpose: there's
// no backend to persist to, and starting fresh each visit is the simpler
// (and more honest) behavior for a sandbox.

export type GuestType = "qemu" | "lxc"

export interface DemoGuest {
  id: string // "qemu/100"
  type: GuestType
  vmid: number
  name: string
  node: string
  connId: string
  status: "running" | "stopped"
  cores: number
  cpu: number // 0..1 fraction of `cores`
  maxmem: number // bytes
  mem: number // bytes
  maxdisk: number // bytes
  disk: number // bytes
  uptime: number // seconds, 0 when stopped
  tags?: string
  hastate?: string
}

export interface DemoNode {
  node: string
  connId: string
  status: "online"
  cores: number
  cpu: number // 0..1
  maxmem: number
  mem: number
  maxdisk: number
  disk: number
  uptime: number
}

export interface DemoConnection {
  id: string
  name: string
  type: "pve" | "pbs"
  host: string
  port: number
  authType: "token" | "password"
  tokenId?: string
  username?: string
  verifyTls: boolean
  behindReverseProxy: boolean
  createdAt: string
  clusterName?: string
  quorate: boolean
  cores: number
  coresUsed: number
  memTotal: number
  memUsed: number
  storageTotal: number
  storageUsed: number
}

const GB = 1024 ** 3
const TB = 1024 ** 4
const now = () => Date.now() / 1000
const DAY = 86400

export const connections: DemoConnection[] = [
  {
    id: "c-prod",
    name: "prod-cluster",
    type: "pve",
    host: "10.20.0.11",
    port: 8006,
    authType: "token",
    tokenId: "root@pam!ferrum",
    verifyTls: true,
    behindReverseProxy: false,
    createdAt: new Date(Date.now() - 210 * DAY * 1000).toISOString(),
    clusterName: "prod-cluster",
    quorate: true,
    cores: 96,
    coresUsed: 42.9,
    memTotal: 768 * GB,
    memUsed: 344 * GB,
    storageTotal: 30.93 * TB,
    storageUsed: 10.51 * TB,
  },
  {
    id: "c-branch",
    name: "branch-office",
    type: "pve",
    host: "10.30.0.11",
    port: 8006,
    authType: "token",
    tokenId: "root@pam!ferrum",
    verifyTls: true,
    behindReverseProxy: false,
    createdAt: new Date(Date.now() - 150 * DAY * 1000).toISOString(),
    clusterName: "branch-office",
    quorate: true,
    cores: 32,
    coresUsed: 8.8,
    memTotal: 256 * GB,
    memUsed: 96 * GB,
    storageTotal: 4.98 * TB,
    storageUsed: 1.29 * TB,
  },
  {
    id: "c-dr",
    name: "dr-standby",
    type: "pve",
    host: "10.40.0.5",
    port: 8006,
    authType: "password",
    username: "root@pam",
    verifyTls: false,
    behindReverseProxy: false,
    createdAt: new Date(Date.now() - 90 * DAY * 1000).toISOString(),
    quorate: true,
    cores: 8,
    coresUsed: 0.6,
    memTotal: 64 * GB,
    memUsed: 14 * GB,
    storageTotal: 300 * GB,
    storageUsed: 102 * GB,
  },
]

export const nodes: DemoNode[] = [
  { node: "pve-core-01", connId: "c-prod", status: "online", cores: 32, cpu: 0.52, maxmem: 256 * GB, mem: 122 * GB, maxdisk: 120 * GB, disk: 41 * GB, uptime: 41 * DAY },
  { node: "pve-core-02", connId: "c-prod", status: "online", cores: 32, cpu: 0.37, maxmem: 256 * GB, mem: 108 * GB, maxdisk: 120 * GB, disk: 52 * GB, uptime: 41 * DAY },
  { node: "pve-core-03", connId: "c-prod", status: "online", cores: 32, cpu: 0.55, maxmem: 256 * GB, mem: 114 * GB, maxdisk: 120 * GB, disk: 38 * GB, uptime: 18 * DAY },
  { node: "pve-branch-01", connId: "c-branch", status: "online", cores: 16, cpu: 0.24, maxmem: 128 * GB, mem: 44 * GB, maxdisk: 100 * GB, disk: 33 * GB, uptime: 27 * DAY },
  { node: "pve-branch-02", connId: "c-branch", status: "online", cores: 16, cpu: 0.31, maxmem: 128 * GB, mem: 52 * GB, maxdisk: 100 * GB, disk: 29 * GB, uptime: 27 * DAY },
  { node: "pve-dr-01", connId: "c-dr", status: "online", cores: 8, cpu: 0.08, maxmem: 64 * GB, mem: 14 * GB, maxdisk: 200 * GB, disk: 61 * GB, uptime: 63 * DAY },
]

function guest(
  connId: string,
  node: string,
  type: GuestType,
  vmid: number,
  name: string,
  status: "running" | "stopped",
  cores: number,
  memGb: number,
  usedMemFrac: number,
  cpuFrac: number,
  diskGb: number,
  uptimeDays: number,
  extra?: Partial<DemoGuest>,
): DemoGuest {
  return {
    id: `${type}/${vmid}`,
    type,
    vmid,
    name,
    node,
    connId,
    status,
    cores,
    cpu: status === "running" ? cpuFrac : 0,
    maxmem: memGb * GB,
    mem: status === "running" ? memGb * GB * usedMemFrac : 0,
    maxdisk: diskGb * GB,
    disk: diskGb * GB * 0.6,
    uptime: status === "running" ? uptimeDays * DAY : 0,
    ...extra,
  }
}

export const guests: DemoGuest[] = [
  // prod-cluster — 10 VMs + 6 CTs
  guest("c-prod", "pve-core-01", "qemu", 100, "web-lb-01", "running", 2, 4, 0.55, 0.18, 40, 62),
  guest("c-prod", "pve-core-01", "qemu", 101, "web-app-01", "running", 4, 8, 0.68, 0.34, 60, 62),
  guest("c-prod", "pve-core-02", "qemu", 102, "db-primary", "running", 8, 32, 0.88, 0.61, 400, 140, { tags: "database" }),
  guest("c-prod", "pve-core-02", "qemu", 103, "db-replica-01", "running", 8, 32, 0.7, 0.35, 400, 140, { tags: "database" }),
  guest("c-prod", "pve-core-01", "qemu", 104, "k8s-worker-01", "running", 6, 16, 0.88, 0.61, 120, 48, { tags: "kubernetes" }),
  guest("c-prod", "pve-core-03", "qemu", 105, "k8s-worker-02", "running", 6, 16, 0.81, 0.44, 120, 48, { tags: "kubernetes" }),
  guest("c-prod", "pve-core-03", "qemu", 106, "k8s-worker-03", "running", 6, 16, 0.59, 0.29, 120, 48, { tags: "kubernetes" }),
  guest("c-prod", "pve-core-03", "qemu", 107, "vpn-gateway", "running", 1, 2, 0.3, 0.06, 20, 300),
  guest("c-prod", "pve-core-02", "qemu", 108, "staging-app-01", "stopped", 2, 8, 0, 0, 60, 0),
  guest("c-prod", "pve-core-01", "qemu", 109, "dev-sandbox-01", "stopped", 2, 4, 0, 0, 40, 0),
  guest("c-prod", "pve-core-01", "lxc", 110, "gitlab-ci-runner", "running", 4, 8, 0.63, 0.42, 80, 90, { tags: "ci" }),
  guest("c-prod", "pve-core-02", "lxc", 111, "redis-cache-01", "running", 2, 8, 0.71, 0.4, 20, 90),
  guest("c-prod", "pve-core-03", "lxc", 112, "monitoring-stack", "running", 2, 8, 0.52, 0.38, 100, 210),
  guest("c-prod", "pve-core-03", "lxc", 113, "log-aggregator", "running", 2, 6, 0.46, 0.27, 200, 210),
  guest("c-prod", "pve-core-01", "lxc", 114, "dns-primary", "running", 1, 1, 0.28, 0.03, 8, 300),
  guest("c-prod", "pve-core-02", "lxc", 115, "backup-proxy", "stopped", 1, 2, 0, 0, 20, 0),

  // branch-office — 4 VMs + 3 CTs
  guest("c-branch", "pve-branch-01", "qemu", 200, "web-app-02", "running", 2, 4, 0.5, 0.2, 40, 27),
  guest("c-branch", "pve-branch-01", "qemu", 201, "file-share-01", "running", 2, 4, 0.4, 0.11, 400, 180),
  guest("c-branch", "pve-branch-02", "qemu", 202, "ci-runner-02", "stopped", 2, 4, 0, 0, 40, 0, { hastate: undefined }),
  guest("c-branch", "pve-branch-02", "qemu", 203, "dev-sandbox-02", "stopped", 2, 4, 0, 0, 40, 0),
  guest("c-branch", "pve-branch-01", "lxc", 210, "dns-secondary", "running", 1, 1, 0.25, 0.03, 8, 300),
  guest("c-branch", "pve-branch-02", "lxc", 211, "mail-relay", "running", 1, 2, 0.35, 0.09, 20, 300),
  guest("c-branch", "pve-branch-02", "lxc", 212, "vpn-branch", "running", 1, 1, 0.22, 0.05, 8, 300),

  // dr-standby — 2 VMs + 1 CT
  guest("c-dr", "pve-dr-01", "qemu", 300, "staging-app-02", "running", 2, 8, 0.35, 0.15, 60, 21),
  guest("c-dr", "pve-dr-01", "qemu", 301, "dr-replica-db", "running", 4, 16, 0.5, 0.12, 300, 63),
  guest("c-dr", "pve-dr-01", "lxc", 310, "file-archive", "running", 1, 4, 0.3, 0.07, 180, 180),
]

export const storagePools = [
  { id: "ceph-pool-rbd", connId: "c-prod", type: "rbd", shared: true, total: 19 * TB, used: 10.8 * TB },
  { id: "local-lvm", connId: "c-prod", node: "pve-core-01", type: "lvmthin", shared: false, total: 2 * TB, used: 1.48 * TB },
  { id: "local-lvm", connId: "c-prod", node: "pve-core-02", type: "lvmthin", shared: false, total: 2 * TB, used: 0.92 * TB },
  { id: "local-lvm", connId: "c-prod", node: "pve-core-03", type: "lvmthin", shared: false, total: 2 * TB, used: 1.1 * TB },
  { id: "nfs-media", connId: "c-branch", type: "nfs", shared: true, total: 4.98 * TB, used: 1.29 * TB },
  { id: "local-lvm", connId: "c-branch", node: "pve-branch-01", type: "lvmthin", shared: false, total: 500 * GB, used: 180 * GB },
  { id: "local-lvm", connId: "c-branch", node: "pve-branch-02", type: "lvmthin", shared: false, total: 500 * GB, used: 140 * GB },
  { id: "local", connId: "c-dr", node: "pve-dr-01", type: "dir", shared: false, total: 300 * GB, used: 102 * GB },
  { id: "pbs-datastore-01", connId: "c-prod", type: "pbs", shared: true, total: 23 * TB, used: 9.4 * TB },
]

export const cephStatus = {
  health: "HEALTH_OK",
  mons: 3,
  osds: { total: 12, up: 12, in: 12 },
  pgs: 512,
  objects: 1_204_000,
}

export const backupJobs = [
  { id: "backup-nightly-vm-full", connId: "c-prod", schedule: "02:30 daily", storage: "pbs-datastore-01", mode: "snapshot", lastRun: now() - 6 * 3600, lastStatus: "ok", durationSec: 38 * 60 },
  { id: "backup-weekly-lxc-snap", connId: "c-prod", schedule: "Sun 01:00", storage: "pbs-datastore-01", mode: "snapshot", lastRun: now() - 3 * DAY, lastStatus: "ok", durationSec: 12 * 60 },
  { id: "backup-edge-daily", connId: "c-branch", schedule: "03:00 daily", storage: "nfs-media", mode: "stop", lastRun: now() - 5 * 3600, lastStatus: "warning", durationSec: 21 * 60 },
]

export const replicationJobs = [
  { id: "repl-dr-standby", connId: "c-prod", target: "c-dr", schedule: "*/4:00", guest: 102, lastSync: now() - 38 * 60, durationSec: 4 * 60, status: "ok" },
]

export const haResources = [
  { sid: "qemu:102", connId: "c-prod", group: "ha-core", state: "started", node: "pve-core-02" },
  { sid: "qemu:107", connId: "c-prod", group: "ha-core", state: "started", node: "pve-core-03" },
  { sid: "qemu:104", connId: "c-prod", group: "ha-edge", state: "started", node: "pve-core-01" },
]

export const haGroups = [
  { group: "ha-core", connId: "c-prod", nodes: "pve-core-01:2,pve-core-02:2,pve-core-03:1", restricted: 0, nofailback: 0 },
  { group: "ha-edge", connId: "c-prod", nodes: "pve-core-01,pve-core-03", restricted: 0, nofailback: 1 },
]

export const firewallRules = [
  { pos: 0, type: "in", action: "ACCEPT", proto: "tcp", dport: "8006", comment: "Web UI" },
  { pos: 1, type: "in", action: "ACCEPT", proto: "tcp", dport: "22", source: "10.0.0.0/8", comment: "SSH, internal only" },
  { pos: 2, type: "in", action: "DROP", comment: "Default deny" },
]

export const sdnZones = [{ zone: "edgezone", type: "vlan", nodes: "pve-core-01,pve-core-02,pve-core-03" }]
export const sdnVnets = [{ vnet: "vnet0", zone: "edgezone", tag: 100, alias: "guest-lan" }]

export interface DemoAlertRule {
  id: string
  name: string
  metric: string
  connectionId?: string
  threshold: number
  severity: "warning" | "critical"
  enabled: boolean
  createdAt: string
}

export const alertRules: DemoAlertRule[] = [
  { id: "rule-cpu", name: "Sustained high CPU", metric: "cpu", threshold: 90, severity: "warning", enabled: true, createdAt: new Date(Date.now() - 120 * DAY * 1000).toISOString() },
  { id: "rule-disk", name: "Storage headroom", metric: "disk", threshold: 85, severity: "warning", enabled: true, createdAt: new Date(Date.now() - 120 * DAY * 1000).toISOString() },
  { id: "rule-guest-down", name: "Guest heartbeat", metric: "guest_state", threshold: 1, severity: "critical", enabled: true, createdAt: new Date(Date.now() - 120 * DAY * 1000).toISOString() },
]

export interface DemoAlert {
  id: string
  ruleId: string
  connectionId: string
  connectionName: string
  resourceName: string
  metric: string
  value: number
  threshold: number
  severity: "warning" | "critical"
  status: "active" | "resolved"
  triggeredAt: string
  updatedAt: string
}

export const alerts: DemoAlert[] = [
  {
    id: "alert-1",
    ruleId: "rule-guest-down",
    connectionId: "c-branch",
    connectionName: "branch-office",
    resourceName: "ci-runner-02",
    metric: "guest_state",
    value: 0,
    threshold: 1,
    severity: "critical",
    status: "active",
    triggeredAt: new Date(Date.now() - 4 * 60 * 1000).toISOString(),
    updatedAt: new Date(Date.now() - 4 * 60 * 1000).toISOString(),
  },
  {
    id: "alert-2",
    ruleId: "rule-disk",
    connectionId: "c-branch",
    connectionName: "branch-office",
    resourceName: "nfs-media",
    metric: "disk",
    value: 88,
    threshold: 85,
    severity: "warning",
    status: "active",
    triggeredAt: new Date(Date.now() - 42 * 60 * 1000).toISOString(),
    updatedAt: new Date(Date.now() - 42 * 60 * 1000).toISOString(),
  },
  {
    id: "alert-3",
    ruleId: "rule-cpu",
    connectionId: "c-prod",
    connectionName: "prod-cluster",
    resourceName: "k8s-worker-01",
    metric: "cpu",
    value: 91,
    threshold: 90,
    severity: "warning",
    status: "active",
    triggeredAt: new Date(Date.now() - 70 * 60 * 1000).toISOString(),
    updatedAt: new Date(Date.now() - 11 * 60 * 1000).toISOString(),
  },
]

export const webhooks = [
  {
    id: "wh-1",
    name: "Ops Slack channel",
    url: "https://hooks.slack.com/services/T000/B000/xxxxxxxxxxxxxxxxxxxx",
    eventTypes: ["alert.triggered", "backup.failed", "guest.state_changed"],
    active: true,
    createdAt: new Date(Date.now() - 80 * DAY * 1000).toISOString(),
    updatedAt: new Date(Date.now() - 80 * DAY * 1000).toISOString(),
  },
]

export interface DemoUser {
  id: string
  username: string
  email: string
  isAdmin: boolean
  totpEnabled: boolean
  createdAt: string
}

export const users: DemoUser[] = [
  { id: "u-admin", username: "admin", email: "admin@example.com", isAdmin: true, totpEnabled: true, createdAt: new Date(Date.now() - 300 * DAY * 1000).toISOString() },
  { id: "u-jordan", username: "jordan", email: "jordan@example.com", isAdmin: false, totpEnabled: false, createdAt: new Date(Date.now() - 150 * DAY * 1000).toISOString() },
  { id: "u-priya", username: "priya", email: "priya@example.com", isAdmin: true, totpEnabled: true, createdAt: new Date(Date.now() - 95 * DAY * 1000).toISOString() },
]

export const currentUser: DemoUser = users[0]

export const auditLog = [
  { id: "a-1", username: "admin", action: "guest.power.stop", category: "vm", target: "qemu/108 staging-app-01", ip: "10.20.0.4", createdAt: new Date(Date.now() - 40 * 60 * 1000).toISOString() },
  { id: "a-2", username: "priya", action: "backup.job.run", category: "vm", target: "backup-nightly-vm-full", ip: "10.20.0.9", createdAt: new Date(Date.now() - 6 * 3600 * 1000).toISOString() },
  { id: "a-3", username: "admin", action: "alert.silence", category: "connections", target: "alert-2", ip: "10.20.0.4", createdAt: new Date(Date.now() - 1 * 3600 * 1000).toISOString() },
  { id: "a-4", username: "jordan", action: "connection.create", category: "connections", target: "dr-standby", ip: "10.40.0.2", createdAt: new Date(Date.now() - 90 * DAY * 1000).toISOString() },
  { id: "a-5", username: "admin", action: "user.role.update", category: "admin", target: "priya → admin", ip: "10.20.0.4", createdAt: new Date(Date.now() - 60 * DAY * 1000).toISOString() },
]

export function findConn(id: string) {
  return connections.find((c) => c.id === id)
}
export function findNode(connId: string, node: string) {
  return nodes.find((n) => n.connId === connId && n.node === node)
}
export function guestsFor(connId: string) {
  return guests.filter((g) => g.connId === connId)
}
export function nodesFor(connId: string) {
  return nodes.filter((n) => n.connId === connId)
}
