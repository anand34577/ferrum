import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, Wrench } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api, ApiError, type Disk } from "@/lib/api"

type ProvisionType = "directory" | "lvm" | "lvmthin" | "zfs"

const TYPE_LABEL: Record<ProvisionType, string> = {
  directory: "Directory (ext4)",
  lvm: "LVM",
  lvmthin: "LVM-Thin",
  zfs: "ZFS",
}

interface ProvisionDiskDialogProps {
  connId: string
  node: string
  disk: Disk | null
  onOpenChange: (open: boolean) => void
}

// Fills the other confirmed gap from the Proxmox-API audit: disks.go could
// only ever look at a physical drive, never turn an idle one into storage —
// admins had to drop into the real Proxmox UI to format/pool a disk. Single
// disk in, one storage type out; multi-disk ZFS arrays stay in Proxmox's UI.
export function ProvisionDiskDialog({ connId, node, disk, onOpenChange }: ProvisionDiskDialogProps) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [type, setType] = useState<ProvisionType>("directory")
  const [name, setName] = useState("")
  const [addStorage, setAddStorage] = useState(true)

  const base = `/connections/${connId}/nodes/${node}`

  const mutation = useMutation({
    mutationFn: () => {
      if (!disk) throw new Error("No disk selected")
      const body = { device: disk.devpath, name, addStorage }
      switch (type) {
        case "directory":
          return api.post<{ upid: string }>(`${base}/disks/directory`, body)
        case "lvm":
          return api.post<{ upid: string }>(`${base}/disks/lvm`, body)
        case "lvmthin":
          return api.post<{ upid: string }>(`${base}/disks/lvmthin`, body)
        case "zfs":
          return api.post<{ upid: string }>(`${base}/disks/zfs`, { name, devices: [disk.devpath], raidLevel: "single", addStorage })
      }
    },
    onSuccess: () => {
      toast.success(`Provisioning ${disk?.devpath} as ${TYPE_LABEL[type]} started`)
      queryClient.invalidateQueries({ queryKey: ["node-disks", connId, node] })
      queryClient.invalidateQueries({ queryKey: ["node-storage", connId, node] })
      close()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Provisioning failed"),
  })

  function close() {
    onOpenChange(false)
    setType("directory")
    setName("")
    setAddStorage(true)
  }

  async function provision() {
    if (!disk) return
    const ok = await confirm({
      title: `Erase ${disk.devpath}?`,
      description: `This will erase all data on ${disk.devpath} and provision it as ${TYPE_LABEL[type]} storage${addStorage ? ` named "${name}"` : ""}. This cannot be undone.`,
      confirmLabel: "Erase & provision",
    })
    if (ok) mutation.mutate()
  }

  return (
    <Dialog open={disk !== null} onOpenChange={(o) => !o && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Provision {disk?.devpath}</DialogTitle>
          <DialogDescription>
            {disk?.model || disk?.devpath} · {disk ? `${(disk.size / 1e9).toFixed(0)} GB` : ""} — currently unused.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-1.5">
          <Label>Storage type</Label>
          <Select value={type} onValueChange={(v) => setType(v as ProvisionType)}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {(Object.keys(TYPE_LABEL) as ProvisionType[]).map((t) => (
                <SelectItem key={t} value={t}>{TYPE_LABEL[t]}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Name</Label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. fast-nvme" />
        </div>
        <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2">
          <div>
            <p className="text-sm font-medium">Register as storage</p>
            <p className="text-xs text-[var(--text-muted)]">Make it immediately usable for VM disks/backups on this node.</p>
          </div>
          <Switch checked={addStorage} onCheckedChange={setAddStorage} />
        </div>

        <FormError message={mutation.error instanceof ApiError ? mutation.error.message : mutation.error ? "Provisioning failed" : undefined} />

        <DialogFooter>
          <Button variant="destructive" disabled={!name || mutation.isPending} onClick={() => provision()}>
            {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Wrench className="h-3.5 w-3.5" />}
            Erase & provision
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
