import { useQuery } from "@tanstack/react-query"
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  useNodesState,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { Boxes, Check, KeyRound, Lock, Pencil, Server, ServerCog, Waypoints } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { api, type ClusterResource, type Connection, type ConnectionInventory } from "@/lib/api"
import { useTheme } from "@/lib/theme"
import { cn, formatBytes, formatPercentFine } from "@/lib/utils"
import { ErrorState } from "@/components/ui/error-state"
import { PageHeader } from "@/components/ui/page-header"
import { StatusDot } from "@/components/ui/status-dot"

const STATUS_COLOR: Record<string, string> = {
  online: "var(--status-ok)",
  running: "var(--status-ok)",
  offline: "var(--status-error)",
  stopped: "var(--status-error)",
}

const DEFAULT_ROOT_NAME = "Proxmox Servers"
const ROOT_NAME_KEY = "ferrum-topology-root-name"
const POSITIONS_KEY = "ferrum-topology-positions"

type PositionMap = Record<string, { x: number; y: number }>

function loadPositions(): PositionMap {
  try {
    return JSON.parse(localStorage.getItem(POSITIONS_KEY) ?? "{}") as PositionMap
  } catch {
    return {}
  }
}

/** Tiny horizontal load bar used across all topology cards. */
function LoadBar({ pct }: { pct: number }) {
  const color = pct >= 90 ? "bg-[var(--status-error)]" : pct >= 75 ? "bg-[var(--status-warn)]" : "bg-brand-500"
  return (
    <div className="h-1 min-w-10 flex-1 overflow-hidden rounded-sm bg-[var(--track)]">
      <div className={cn("h-full rounded-sm", color)} style={{ width: `${Math.min(100, pct)}%` }} />
    </div>
  )
}

function Chip({ kind }: { kind: string }) {
  const styles =
    kind === "VM"
      ? "bg-[color-mix(in_oklab,var(--chart-1)_15%,transparent)] text-[var(--chart-1)]"
      : kind === "LXC"
        ? "bg-[color-mix(in_oklab,var(--chart-2)_15%,transparent)] text-[var(--chart-2)]"
        : "bg-[var(--bg-muted)] text-[var(--text-muted)]"
  return <span className={cn("shrink-0 rounded px-1 py-px font-mono text-[9px] font-semibold uppercase tracking-wide", styles)}>{kind}</span>
}

/** The central hub everything hangs from — double-click the title to rename
 * it (persisted locally). */
function RootNameEditor({
  initial,
  onSave,
  onCancel,
}: {
  initial: string
  onSave: (val: string) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(initial)
  return (
    <span className="flex items-center gap-1">
      <input
        autoFocus
        value={draft}
        className="nodrag w-40 rounded border border-brand-400/60 bg-[var(--bg-surface)] px-1.5 py-0.5 font-display text-sm font-semibold text-[var(--text)] outline-none"
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.key === "Enter") onSave(draft.trim() || DEFAULT_ROOT_NAME)
          if (e.key === "Escape") onCancel()
        }}
        aria-label="Root node name"
      />
      <button
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => onSave(draft.trim() || DEFAULT_ROOT_NAME)}
        className="text-[var(--status-ok)]"
        aria-label="Save name"
      >
        <Check className="h-3.5 w-3.5" />
      </button>
    </span>
  )
}

/** The central hub everything hangs from — double-click the title to rename
 * it (persisted locally). */
function RootNode({ data }: NodeProps<Node<{ label: string; connections: number; nodes: number; guests: number; editing: boolean; onStartEdit: () => void; onRename: (name: string) => void }>>) {
  return (
    <div className="flex min-w-52 flex-col items-center gap-0.5 rounded-xl bg-[var(--bg-elevated)] px-5 py-3 text-center shadow-lg ring-2 ring-brand-500/60">
      <div onDoubleClick={data.onStartEdit} className="flex cursor-text items-center gap-1.5" title="Double-click to rename">
        <Waypoints className="h-4 w-4 shrink-0 text-brand-400" />
        {data.editing ? (
          <RootNameEditor
            key={data.label}
            initial={data.label}
            onSave={data.onRename}
            onCancel={() => data.onRename(data.label)}
          />
        ) : (
          <>
            <span className="font-display text-sm font-semibold text-[var(--text)]">{data.label}</span>
            <Pencil className="h-3 w-3 text-[var(--text-muted)]" />
          </>
        )}
      </div>
      <p className="font-mono text-[10px] text-[var(--text-muted)] tabular">
        {data.connections} connection{data.connections === 1 ? "" : "s"} · {data.nodes} node{data.nodes === 1 ? "" : "s"} · {data.guests} guest{data.guests === 1 ? "" : "s"}
      </p>
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-brand-400" />
    </div>
  )
}

interface ConnInfo {
  host: string
  port: number
  auth: string
  verifyTls: boolean
}

/** Proxmox cluster/server entry point — carries its connection string. */
function ConnectionNode({ data }: NodeProps<Node<{ label: string; online: boolean; address?: string; auth?: string; verifyTls?: boolean; nodes?: number; guests?: number }>>) {
  return (
    <div
      className={cn(
        "flex min-w-56 flex-col gap-1 rounded-lg px-4 py-3 shadow-sm bg-[var(--bg-surface)]",
        data.online ? "ring-1 ring-brand-500/40" : "ring-1 ring-[var(--status-error)]/50",
      )}
    >
      <div className="flex items-center gap-2">
        <ServerCog className="h-4 w-4 shrink-0 text-brand-500" />
        <p className="truncate font-display text-sm font-semibold leading-tight">{data.label}</p>
        <span className="ml-auto shrink-0" title={data.online ? "Online" : "Offline"}>
          <StatusDot status={data.online ? "ok" : "error"} />
        </span>
      </div>
      {data.address && (
        <p className="truncate font-mono text-[11px]" title={`https://${data.address}`}>
          https://{data.address}
        </p>
      )}
      <div className="flex items-center gap-3 text-[10px] text-[var(--text-muted)]">
        {data.auth && (
          <span className="flex min-w-0 items-center gap-1" title={data.auth}>
            <KeyRound className="h-3 w-3 shrink-0" />
            <span className="truncate font-mono">{data.auth}</span>
          </span>
        )}
        {data.verifyTls === false && (
          <span className="flex shrink-0 items-center gap-1 text-[var(--status-warn)]" title="TLS certificate verification is disabled for this connection">
            <Lock className="h-3 w-3" /> no TLS verify
          </span>
        )}
        {(data.nodes !== undefined || data.guests !== undefined) && (
          <span className="ml-auto shrink-0 tabular">
            {data.nodes ?? 0} nodes · {data.guests ?? 0} guests
          </span>
        )}
      </div>
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--border-strong)]" />
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--border-strong)]" />
    </div>
  )
}

/** PVE host: live CPU, RAM and root-disk load. */
function PveNode({ data }: NodeProps<Node<{ label: string; status: string; cpu: number; mem: number; maxmem: number; disk: number; maxdisk: number; connId?: string; nodeName?: string }>>) {
  const memPct = data.maxmem ? (data.mem / data.maxmem) * 100 : 0
  const diskPct = data.maxdisk ? (data.disk / data.maxdisk) * 100 : 0
  return (
    <div className="w-52 rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 shadow-sm">
      <div className="flex items-center gap-1.5">
        <Server className="h-3.5 w-3.5 shrink-0" style={{ color: STATUS_COLOR[data.status] ?? "var(--text-muted)" }} />
        <span className="truncate text-xs font-semibold" title={data.label}>{data.label}</span>
        <span className="ml-auto shrink-0" title={data.status}>
          <StatusDot status={data.status === "online" ? "ok" : data.status === "offline" ? "error" : "warn"} />
        </span>
      </div>
      <div className="mt-1.5 space-y-1">
        <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
          <span className="w-7 shrink-0">CPU</span>
          <LoadBar pct={data.cpu * 100} />
          <span className="w-9 shrink-0 text-right tabular">{formatPercentFine(data.cpu * 100)}</span>
        </div>
        <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
          <span className="w-7 shrink-0">RAM</span>
          <LoadBar pct={memPct} />
          <span className="w-14 shrink-0 text-right tabular">{formatBytes(data.mem)}</span>
        </div>
        {data.maxdisk > 0 && (
          <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
            <span className="w-7 shrink-0">Disk</span>
            <LoadBar pct={diskPct} />
            <span className="w-14 shrink-0 text-right tabular">{formatBytes(data.disk)}</span>
          </div>
        )}
      </div>
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--border-strong)]" />
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--border-strong)]" />
    </div>
  )
}

/** VM / container: type chip, status dot, and live CPU / RAM / disk load. */
function GuestNode({ data }: NodeProps<Node<{ label: string; vmid: number; status: string; type: string; cpu: number; mem: number; maxmem: number; disk: number; maxdisk: number }>>) {
  const kind = data.type === "lxc" ? "LXC" : "VM"
  const memPct = data.maxmem ? (data.mem / data.maxmem) * 100 : 0
  const diskPct = data.maxdisk ? (data.disk / data.maxdisk) * 100 : 0
  return (
    <div className="w-44 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-2.5 py-1.5 shadow-sm">
      <div className="flex items-center gap-1.5">
        <Chip kind={kind} />
        <span className="truncate text-xs font-medium" title={data.label}>{data.label}</span>
        <span className="ml-auto flex shrink-0 items-center gap-1">
          <span className="font-mono text-[9px] text-[var(--text-faint)] tabular">#{data.vmid}</span>
          <StatusDot status={data.status === "running" ? "ok" : "error"} />
        </span>
      </div>
      <div className="mt-1 space-y-0.5">
        <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
          <span className="w-6 shrink-0">CPU</span>
          <LoadBar pct={data.cpu * 100} />
          <span className="w-9 shrink-0 text-right tabular">{formatPercentFine(data.cpu * 100)}</span>
        </div>
        <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
          <span className="w-6 shrink-0">RAM</span>
          <LoadBar pct={memPct} />
          <span className="w-9 shrink-0 text-right tabular">{memPct.toFixed(0)}%</span>
        </div>
        {data.maxdisk > 0 && (
          <div className="flex items-center gap-1.5 text-[9px] text-[var(--text-muted)]">
            <span className="w-6 shrink-0">Disk</span>
            <LoadBar pct={diskPct} />
            <span className="w-9 shrink-0 text-right tabular">{formatBytes(data.disk)}</span>
          </div>
        )}
      </div>
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--border-strong)]" />
    </div>
  )
}

const nodeTypes = { root: RootNode, connection: ConnectionNode, pveNode: PveNode, guest: GuestNode }

type FlowNode = Node<Record<string, unknown>>

/** MiniMap colors can't use CSS variables — @xyflow applies them as SVG
 * presentation attributes, where var() is invalid — so the live token values
 * are resolved once per theme instead of duplicating them as hardcoded
 * literals (which previously had to be hand-kept in sync with index.css). */
function minimapColors(dark: boolean): { node: Record<string, string>; fallback: string; mask: string } {
  const style = getComputedStyle(document.documentElement)
  const token = (name: string) => style.getPropertyValue(name).trim()
  return {
    node: { root: token("--chart-1"), connection: token("--chart-3"), pveNode: token("--chart-2"), guest: token("--border-strong") },
    fallback: token("--border-strong"),
    mask: dark ? "rgba(18, 20, 23, 0.65)" : "rgba(238, 240, 243, 0.65)",
  }
}

export function TopologyPage() {
  const navigate = useNavigate()
  const { effectiveTheme } = useTheme()
  const [rootName, setRootName] = useState(() => localStorage.getItem(ROOT_NAME_KEY) ?? DEFAULT_ROOT_NAME)
  const [editingRoot, setEditingRoot] = useState(false)
  // Dragged positions, persisted across reloads. State (not a ref) so the
  // graph rebuild sees them and React's compiler can reason about renders.
  const [savedPositions, setSavedPositions] = useState<PositionMap>(() => loadPositions())

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 20_000,
  })
  const { data: connections } = useQuery({
    queryKey: ["connections"],
    queryFn: () => api.get<Connection[]>("/connections/"),
  })
  const infoByConn = useMemo(
    () =>
      new Map(
        (connections ?? []).map((c) => [
          c.id,
          { host: c.host, port: c.port, auth: c.authType === "token" ? c.tokenId ?? "" : c.username ?? "", verifyTls: c.verifyTls } satisfies ConnInfo,
        ]),
      ),
    [connections],
  )

  const { graphNodes, edges } = useMemo(
    () => buildGraph(data ?? [], infoByConn, rootName, editingRoot, setEditingRoot, renameRoot(setRootName), savedPositions),
    [data, infoByConn, rootName, editingRoot, savedPositions],
  )

  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([])
  // Re-sync the graph on every inventory poll while keeping manually dragged
  // positions (in-memory and persisted) intact.
  useEffect(() => {
    setNodes((current) => {
      const posById = new Map(current.map((n) => [n.id, { ...n.position }]))
      return graphNodes.map((n) => ({ ...n, position: posById.get(n.id) ?? n.position }))
    })
  }, [graphNodes, setNodes])

  function handleNodesChange(changes: NodeChange<FlowNode>[]) {
    onNodesChange(changes)
    // Persist manual drags so layout survives reloads and polls.
    const drops = changes.filter(
      (c): c is NodeChange<FlowNode> & { type: "position"; id: string; position: { x: number; y: number } } =>
        c.type === "position" && c.dragging === false && !!c.position,
    )
    if (drops.length === 0) return
    const next: PositionMap = { ...savedPositions }
    for (const d of drops) next[d.id] = d.position
    setSavedPositions(next)
    try {
      localStorage.setItem(POSITIONS_KEY, JSON.stringify(next))
    } catch {
      // storage blocked — dragging still works, it just won't persist
    }
  }

  const hasGraph = nodes.length > 0
  const mmColors = useMemo(() => minimapColors(effectiveTheme === "dark"), [effectiveTheme])

  return (
    <div className="flex h-[calc(100vh-8rem)] flex-col gap-4">
      <PageHeader
        title="Topology"
        description="Drag cards to arrange your estate — the diagram flows from your Proxmox Servers hub through every cluster, host, VM, and container. Double-click the hub name to rename it."
        icon={Waypoints}
      />

      <div className="min-h-0 flex-1 overflow-hidden rounded-lg border border-[var(--border)]">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodesChange={handleNodesChange}
          nodesDraggable
          nodesConnectable={false}
          elementsSelectable
          minZoom={0.1}
          fitView
          fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
          proOptions={{ hideAttribution: true }}
          onNodeClick={(_, node) => {
            if (node.type === "pveNode" && node.data.connId && node.data.nodeName) {
              navigate(`/nodes/${node.data.connId}/${node.data.nodeName}`)
            }
          }}
        >
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="var(--border)" />
          <Controls showInteractive={false} />
          <MiniMap
            pannable
            zoomable
            className="!bg-[var(--bg-muted)]"
            nodeColor={(n) => mmColors.node[(n.type as string) ?? ""] ?? mmColors.fallback}
            maskColor={mmColors.mask}
          />
          {isError && (
            <Panel position="top-center">
              <ErrorState title="Couldn't load your fleet topology" onRetry={refetch} />
            </Panel>
          )}
          {!isError && isLoading && !hasGraph && (
            <Panel position="top-center" className="text-sm text-[var(--text-muted)]" aria-busy>
              Mapping your fleet…
            </Panel>
          )}
          {!isError && !isLoading && !hasGraph && (
            <Panel position="top-center" className="text-sm text-[var(--text-muted)]">
              No connections configured yet — add one under Connections.
            </Panel>
          )}
          {hasGraph && (
            <Panel position="top-left" className="!m-2 flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] px-2.5 py-1.5 text-[11px] text-[var(--text-muted)] shadow-xs">
              <Boxes className="h-3.5 w-3.5" />
              Drag any card to rearrange · positions are remembered
            </Panel>
          )}
        </ReactFlow>
      </div>
    </div>
  )
}

function renameRoot(setter: (name: string) => void) {
  return (name: string) => {
    setter(name)
    try {
      localStorage.setItem(ROOT_NAME_KEY, name)
    } catch {
      // non-fatal — the name just resets next load
    }
  }
}

function buildGraph(
  connections: ConnectionInventory[],
  infoByConn: Map<string, ConnInfo>,
  rootName: string,
  editingRoot: boolean,
  setEditingRoot: (v: boolean) => void,
  onRename: (name: string) => void,
  savedPositions: PositionMap,
): { graphNodes: FlowNode[]; edges: Edge[] } {
  const nodes: FlowNode[] = []
  const edges: Edge[] = []

  const totals = connections.reduce(
    (acc, conn) => {
      const res = conn.resources ?? []
      return {
        nodes: acc.nodes + res.filter((r) => r.type === "node").length,
        guests: acc.guests + res.filter((r) => r.type === "qemu" || r.type === "lxc").length,
      }
    },
    { nodes: 0, guests: 0 },
  )

  nodes.push({
    id: "root",
    type: "root",
    position: savedPositions["root"] ?? { x: -80, y: 140 },
    data: { label: rootName, connections: connections.length, nodes: totals.nodes, guests: totals.guests, editing: editingRoot, onStartEdit: () => setEditingRoot(true), onRename },
    draggable: true,
  })

  const connGapY = 420
  connections.forEach((conn, connIdx) => {
    const connY = connIdx * connGapY
    const connNodeId = `conn-${conn.connectionId}`
    const info = infoByConn.get(conn.connectionId)
    const connString = info ? `${info.host}:${info.port}` : undefined
    const pveNodes = (conn.resources ?? []).filter((r) => r.type === "node")
    const guests = (conn.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc")

    nodes.push({
      id: connNodeId,
      type: "connection",
      position: savedPositions[connNodeId] ?? { x: 300, y: connY + 40 },
      data: {
        label: conn.name,
        online: conn.online,
        address: connString,
        auth: info?.auth,
        verifyTls: info?.verifyTls,
        nodes: pveNodes.length,
        guests: guests.length,
      },
    })
    edges.push({
      id: `e-root-${connNodeId}`,
      source: "root",
      target: connNodeId,
      animated: conn.online,
      style: { stroke: conn.online ? "var(--status-ok)" : "var(--status-error)", strokeWidth: 2 },
    })

    pveNodes.forEach((pn: ClusterResource, nodeIdx) => {
      const nodeId = `node-${conn.connectionId}-${pn.node}`
      const nodeY = connY + nodeIdx * 118
      nodes.push({
        id: nodeId,
        type: "pveNode",
        position: savedPositions[nodeId] ?? { x: 700, y: nodeY },
        data: {
          label: pn.node,
          status: pn.status ?? "unknown",
          cpu: pn.cpu ?? 0,
          mem: pn.mem ?? 0,
          maxmem: pn.maxmem ?? 0,
          disk: pn.disk ?? 0,
          maxdisk: pn.maxdisk ?? 0,
          connId: conn.connectionId,
          nodeName: pn.node,
        },
      })
      edges.push({
        id: `e-${connNodeId}-${nodeId}`,
        source: connNodeId,
        target: nodeId,
        animated: conn.online && pn.status === "online",
        label: connString,
        labelShowBg: true,
        labelBgPadding: [4, 2],
        labelBgBorderRadius: 4,
        labelBgStyle: { fill: "var(--bg-surface)", stroke: "var(--border)", strokeWidth: 0.5 },
        labelStyle: { fill: "var(--text-muted)", fontSize: 9, fontFamily: "var(--font-mono)" },
        style: {
          stroke: pn.status === "online" ? "var(--status-ok)" : "var(--status-error)",
          strokeWidth: 1.5,
        },
      })

      const nodeGuests = guests.filter((g) => g.node === pn.node)
      nodeGuests.forEach((g, guestIdx) => {
        const guestId = `guest-${conn.connectionId}-${g.id}`
        nodes.push({
          id: guestId,
          type: "guest",
          position: savedPositions[guestId] ?? { x: 1010, y: nodeY + guestIdx * 84 - ((nodeGuests.length - 1) * 84) / 2 },
          data: {
            label: g.name ?? `#${g.vmid}`,
            vmid: g.vmid ?? 0,
            status: g.status ?? "unknown",
            type: g.type,
            cpu: g.cpu ?? 0,
            mem: g.mem ?? 0,
            maxmem: g.maxmem ?? 0,
            disk: g.disk ?? 0,
            maxdisk: g.maxdisk ?? 0,
          },
        })
        edges.push({
          id: `e-${nodeId}-${guestId}`,
          source: nodeId,
          target: guestId,
          style: { stroke: "var(--text-muted)", strokeWidth: 1.5 },
        })
      })
    })
  })

  return { graphNodes: nodes, edges }
}
