import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, Plus, Search } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  api,
  ApiError,
  type CIFSShare,
  type GlusterVolume,
  type ISCSITarget,
  type NFSExport,
} from "@/lib/api"

type StorageType = "dir" | "nfs" | "cifs" | "lvm" | "lvmthin" | "zfspool" | "iscsi" | "glusterfs"

const TYPE_LABEL: Record<StorageType, string> = {
  dir: "Directory",
  nfs: "NFS",
  cifs: "CIFS / SMB",
  lvm: "LVM",
  lvmthin: "LVM-Thin",
  zfspool: "ZFS Pool",
  iscsi: "iSCSI",
  glusterfs: "GlusterFS",
}

// Storage types whose export/share/target lives on a remote server the admin
// otherwise has to already know by heart — these get a "Scan" button next to
// the server/portal field to browse what's actually there.
const SCANNABLE: StorageType[] = ["nfs", "cifs", "iscsi", "glusterfs"]

interface ConnectionGroup {
  connId: string
  connName: string
  nodes: string[]
}

interface CreateStorageDialogProps {
  /** One entry per connection this dialog can target. A single "Add Storage"
   * trigger regardless of how many connections/nodes exist — with one button
   * per connection (the previous design) a 20-connection fleet turns the
   * card header into a wall of buttons that wraps for pages and pushes
   * everything else down; a single dialog with a connection picker scales
   * to any fleet size instead. */
  groups: ConnectionGroup[]
}

// Fills the confirmed gap from the Proxmox-API audit: registering a storage
// backend required already knowing the exact export path/IQN by heart, or
// looking it up in the real Proxmox UI first — this exposes PVE's own
// discovery (/scan/nfs, /scan/cifs, /scan/iscsi, /scan/glusterfs) instead.
export function CreateStorageDialog({ groups }: CreateStorageDialogProps) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [connId, setConnId] = useState(groups[0]?.connId ?? "")
  const nodes = groups.find((g) => g.connId === connId)?.nodes ?? []
  const [type, setType] = useState<StorageType>("nfs")
  const [storageName, setStorageName] = useState("")
  const [node, setNode] = useState(nodes[0] ?? "")
  const [server, setServer] = useState("")
  const [exportOrShare, setExportOrShare] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [domain, setDomain] = useState("")
  const [vgname, setVgname] = useState("")
  const [thinpool, setThinpool] = useState("")
  const [pool, setPool] = useState("")
  const [path, setPath] = useState("")
  const [portal, setPortal] = useState("")
  const [target, setTarget] = useState("")

  const [scanOptions, setScanOptions] = useState<string[]>([])

  function reset() {
    setConnId(groups[0]?.connId ?? "")
    setNode(groups[0]?.nodes[0] ?? "")
    setType("nfs")
    setStorageName("")
    setServer("")
    setExportOrShare("")
    setUsername("")
    setPassword("")
    setDomain("")
    setVgname("")
    setThinpool("")
    setPool("")
    setPath("")
    setPortal("")
    setTarget("")
    setScanOptions([])
  }

  const scanMutation = useMutation({
    mutationFn: async (): Promise<string[]> => {
      const base = `/connections/${connId}/nodes/${node}/scan`
      switch (type) {
        case "nfs": {
          const exports = await api.get<NFSExport[]>(`${base}/nfs?server=${encodeURIComponent(server)}`)
          return exports.map((e) => e.path)
        }
        case "cifs": {
          // CIFS is the one scan that posts: the backend deliberately takes
          // credentials in a JSON body (GET query strings end up in access
          // logs and browser history). Sending them as GET params made the
          // scan 404 into the SPA catch-all — see server.go's scanCIFS.
          const shares = await api.post<CIFSShare[]>(`${base}/cifs`, {
            server,
            ...(username ? { username } : {}),
            ...(password ? { password } : {}),
            ...(domain ? { domain } : {}),
          })
          return shares.map((s) => s.share)
        }
        case "iscsi": {
          const targets = await api.get<ISCSITarget[]>(`${base}/iscsi?portal=${encodeURIComponent(portal)}`)
          return targets.map((t) => t.target)
        }
        case "glusterfs": {
          const volumes = await api.get<GlusterVolume[]>(`${base}/glusterfs?server=${encodeURIComponent(server)}`)
          return volumes.map((v) => v.volname)
        }
        default:
          return []
      }
    },
    onSuccess: (options) => {
      setScanOptions(options)
      if (options.length === 0) toast.info("Scan found nothing at that address")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Scan failed"),
  })

  function extraFields(): Record<string, string> {
    switch (type) {
      case "dir":
        return { path }
      case "nfs":
        return { server, export: exportOrShare }
      case "cifs":
        return { server, share: exportOrShare, ...(username ? { username } : {}), ...(domain ? { domain } : {}) }
      case "lvm":
        return { vgname }
      case "lvmthin":
        return { vgname, thinpool }
      case "zfspool":
        return { pool }
      case "iscsi":
        return { portal, target }
      case "glusterfs":
        return { server, volume: exportOrShare }
    }
  }

  const createMutation = useMutation({
    mutationFn: () =>
      api.post(`/connections/${connId}/storage`, {
        storage: storageName,
        type,
        nodes: node,
        extra: extraFields(),
      }),
    onSuccess: () => {
      toast.success(`Storage "${storageName}" created`)
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setOpen(false)
      reset()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create storage"),
  })

  const needsScanTarget = type === "iscsi" ? !!portal : !!server
  const canCreate = !!storageName && !!node && (() => {
    switch (type) {
      case "dir": return !!path
      case "nfs": return !!server && !!exportOrShare
      case "cifs": return !!server && !!exportOrShare
      case "lvm": return !!vgname
      case "lvmthin": return !!vgname && !!thinpool
      case "zfspool": return !!pool
      case "iscsi": return !!portal && !!target
      case "glusterfs": return !!server && !!exportOrShare
    }
  })()

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) reset() }}>
      <DialogTrigger asChild>
        <Button type="button" size="sm">
          <Plus className="h-3.5 w-3.5" /> Add Storage
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add Storage</DialogTitle>
          <DialogDescription>Register a storage backend for a connection.</DialogDescription>
        </DialogHeader>

        {/* Only shown once there's an actual choice — a single connection
            skips straight to the Node picker below, same as before. */}
        {groups.length > 1 && (
          <div className="space-y-1.5">
            <Label>Connection</Label>
            <Select
              value={connId}
              onValueChange={(v) => {
                setConnId(v)
                setNode(groups.find((g) => g.connId === v)?.nodes[0] ?? "")
              }}
            >
              <SelectTrigger><SelectValue placeholder="Select a connection..." /></SelectTrigger>
              <SelectContent>
                {groups.map((g) => <SelectItem key={g.connId} value={g.connId}>{g.connName}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
        )}

        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <Label>Type</Label>
            <Select value={type} onValueChange={(v) => { setType(v as StorageType); setScanOptions([]) }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {(Object.keys(TYPE_LABEL) as StorageType[]).map((t) => (
                  <SelectItem key={t} value={t}>{TYPE_LABEL[t]}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>ID</Label>
            <Input value={storageName} onChange={(e) => setStorageName(e.target.value)} placeholder="my-storage" />
          </div>
        </div>

        <div className="space-y-1.5">
          <Label>Node</Label>
          <Select value={node} onValueChange={setNode}>
            <SelectTrigger><SelectValue placeholder="Select..." /></SelectTrigger>
            <SelectContent>
              {nodes.map((n) => <SelectItem key={n} value={n}>{n}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>

        {type === "dir" && (
          <div className="space-y-1.5">
            <Label>Path</Label>
            <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/mnt/data" />
          </div>
        )}

        {type === "iscsi" ? (
          <div className="space-y-1.5">
            <Label>Portal</Label>
            <div className="flex gap-2">
              <Input value={portal} onChange={(e) => { setPortal(e.target.value); setScanOptions([]) }} placeholder="192.168.1.10" className="flex-1" />
              <Button type="button" variant="outline" disabled={!node || !needsScanTarget || scanMutation.isPending} onClick={() => scanMutation.mutate()}>
                {scanMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />} Scan
              </Button>
            </div>
          </div>
        ) : (SCANNABLE.includes(type)) ? (
          <div className="space-y-1.5">
            <Label>Server</Label>
            <div className="flex gap-2">
              <Input value={server} onChange={(e) => { setServer(e.target.value); setScanOptions([]) }} placeholder="192.168.1.10" className="flex-1" />
              <Button type="button" variant="outline" disabled={!node || !needsScanTarget || scanMutation.isPending} onClick={() => scanMutation.mutate()}>
                {scanMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />} Scan
              </Button>
            </div>
          </div>
        ) : null}

        {type === "cifs" && (
          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-1.5">
              <Label>Username</Label>
              <Input value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label>Password</Label>
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label>Domain</Label>
              <Input value={domain} onChange={(e) => setDomain(e.target.value)} />
            </div>
          </div>
        )}

        {type === "iscsi" ? (
          <div className="space-y-1.5">
            <Label>Target</Label>
            {scanOptions.length > 0 ? (
              <Select value={target} onValueChange={setTarget}>
                <SelectTrigger><SelectValue placeholder="Select a discovered target..." /></SelectTrigger>
                <SelectContent>
                  {scanOptions.map((o) => <SelectItem key={o} value={o}>{o}</SelectItem>)}
                </SelectContent>
              </Select>
            ) : (
              <Input value={target} onChange={(e) => setTarget(e.target.value)} placeholder="iqn.2019-10.com.example:target1" />
            )}
          </div>
        ) : (type === "nfs" || type === "cifs" || type === "glusterfs") && (
          <div className="space-y-1.5">
            <Label>{type === "cifs" ? "Share" : type === "glusterfs" ? "Volume" : "Export"}</Label>
            {scanOptions.length > 0 ? (
              <Select value={exportOrShare} onValueChange={setExportOrShare}>
                <SelectTrigger><SelectValue placeholder="Select a discovered option..." /></SelectTrigger>
                <SelectContent>
                  {scanOptions.map((o) => <SelectItem key={o} value={o}>{o}</SelectItem>)}
                </SelectContent>
              </Select>
            ) : (
              <Input value={exportOrShare} onChange={(e) => setExportOrShare(e.target.value)} placeholder={type === "nfs" ? "/export/data" : type === "cifs" ? "share-name" : "volume-name"} />
            )}
          </div>
        )}

        {type === "lvm" && (
          <div className="space-y-1.5">
            <Label>Volume group</Label>
            <Input value={vgname} onChange={(e) => setVgname(e.target.value)} placeholder="my-vg" />
          </div>
        )}
        {type === "lvmthin" && (
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>Volume group</Label>
              <Input value={vgname} onChange={(e) => setVgname(e.target.value)} placeholder="my-vg" />
            </div>
            <div className="space-y-1.5">
              <Label>Thin pool</Label>
              <Input value={thinpool} onChange={(e) => setThinpool(e.target.value)} placeholder="data" />
            </div>
          </div>
        )}
        {type === "zfspool" && (
          <div className="space-y-1.5">
            <Label>ZFS pool</Label>
            <Input value={pool} onChange={(e) => setPool(e.target.value)} placeholder="rpool/data" />
          </div>
        )}

        <FormError message={createMutation.error instanceof ApiError ? createMutation.error.message : createMutation.error ? "Failed to create storage" : undefined} />

        <DialogFooter>
          <Button disabled={!canCreate || createMutation.isPending} onClick={() => createMutation.mutate()}>
            {createMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
