import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Combobox } from "@/components/ui/combobox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { UploadTemplateDialog } from "@/components/inventory/UploadTemplateDialog"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { api, ApiError, type ClusterResource, type TemplateItem } from "@/lib/api"
import { dangerousExtraKeys, parseExtraLines } from "@/lib/utils"

interface CreateGuestDialogProps {
  connId: string
  nodes: ClusterResource[]
  open: boolean
  onOpenChange: (open: boolean) => void
}

const vmDefaults = {
  node: "", name: "", cores: 2, memoryMb: 2048, storage: "local-lvm", diskGb: 20,
  bridge: "vmbr0", iso: "", ciUser: "", ciPassword: "", sshPublicKey: "", ipConfig: "ip=dhcp",
  extraText: "",
}

const lxcDefaults = {
  node: "", hostname: "", cores: 2, memoryMb: 1024, storage: "local-lvm", diskGb: 8,
  template: "", bridge: "vmbr0", password: "", sshPublicKey: "", unprivileged: true,
  extraText: "",
}


export function CreateGuestDialog({ connId, nodes, open, onOpenChange }: CreateGuestDialogProps) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [vmForm, setVmForm] = useState(vmDefaults)
  const [lxcForm, setLxcForm] = useState(lxcDefaults)

  const templatesQuery = useQuery({
    queryKey: ["templates", connId],
    queryFn: () => api.get<TemplateItem[]>(`/connections/${connId}/templates`),
    enabled: open,
    staleTime: 60_000,
  })
  const isos = templatesQuery.data?.filter((t) => t.content === "iso") ?? []
  const vztmpls = templatesQuery.data?.filter((t) => t.content === "vztmpl") ?? []

  // Single-node connections are the common case — don't make the admin pick
  // the only option that exists. Only fills the field when it's still blank,
  // so it never clobbers a deliberate choice.
  useEffect(() => {
    if (!open || nodes.length !== 1) return
    const only = nodes[0].node!
    setVmForm((f) => (f.node ? f : { ...f, node: only }))
    setLxcForm((f) => (f.node ? f : { ...f, node: only }))
  }, [open, nodes])

  const vmInvalid = vmForm.cores < 1 || vmForm.memoryMb < 16 || vmForm.diskGb < 1
  const lxcInvalid = lxcForm.cores < 1 || lxcForm.memoryMb < 16 || lxcForm.diskGb < 1

  const createVM = useMutation({
    mutationFn: () => api.post(`/connections/${connId}/vms`, { ...vmForm, extraText: undefined, extra: parseExtraLines(vmForm.extraText) }),
    onSuccess: () => {
      toast.success("VM creation started")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      onOpenChange(false)
      setVmForm(vmDefaults)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create VM"),
  })

  const createLXC = useMutation({
    mutationFn: () => api.post(`/connections/${connId}/lxc`, { ...lxcForm, extraText: undefined, extra: parseExtraLines(lxcForm.extraText) }),
    onSuccess: () => {
      toast.success("Container creation started")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      onOpenChange(false)
      setLxcForm(lxcDefaults)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create container"),
  })

  async function confirmDangerousExtra(keys: string[]) {
    if (keys.length === 0) return true
    return confirm({
      title: "Advanced option runs on the host",
      description: `"${keys.join(", ")}" ${keys.length > 1 ? "are" : "is"} not sandboxed like normal guest config — it can run arbitrary code on the Proxmox host or weaken this guest's isolation. Only continue if you trust this configuration.`,
      confirmLabel: "Create anyway",
    })
  }

  async function submitVM() {
    if (await confirmDangerousExtra(dangerousExtraKeys(parseExtraLines(vmForm.extraText)))) createVM.mutate()
  }

  async function submitLXC() {
    if (await confirmDangerousExtra(dangerousExtraKeys(parseExtraLines(lxcForm.extraText)))) createLXC.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Create guest</DialogTitle>
          <DialogDescription>Provision a new VM or container on this connection.</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="vm">
          <TabsList>
            <TabsTrigger value="vm">Virtual Machine</TabsTrigger>
            <TabsTrigger value="lxc">Container</TabsTrigger>
          </TabsList>

          <TabsContent value="vm" className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Node</Label>
                <Combobox
                  value={vmForm.node}
                  onChange={(v) => setVmForm({ ...vmForm, node: v })}
                  options={nodes.map((n) => ({ value: n.node!, label: n.node! }))}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Name</Label>
                <Input value={vmForm.name} onChange={(e) => setVmForm({ ...vmForm, name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Cores</Label>
                <Input type="number" min={1} value={vmForm.cores} onChange={(e) => setVmForm({ ...vmForm, cores: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Memory (MB)</Label>
                <Input type="number" min={16} value={vmForm.memoryMb} onChange={(e) => setVmForm({ ...vmForm, memoryMb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Storage</Label>
                <Input value={vmForm.storage} onChange={(e) => setVmForm({ ...vmForm, storage: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Disk (GB)</Label>
                <Input type="number" min={1} value={vmForm.diskGb} onChange={(e) => setVmForm({ ...vmForm, diskGb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Network bridge</Label>
                <Input value={vmForm.bridge} onChange={(e) => setVmForm({ ...vmForm, bridge: e.target.value })} />
              </div>
              <div className="col-span-2 space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label>Boot ISO (optional — omit when cloning a cloud-init template)</Label>
                  <UploadTemplateDialog connId={connId} nodes={nodes} content="iso" />
                </div>
                <Combobox
                  value={vmForm.iso}
                  onChange={(v) => setVmForm({ ...vmForm, iso: v })}
                  placeholder={templatesQuery.isLoading ? "Loading..." : "None"}
                  searchPlaceholder="Search ISOs..."
                  emptyText="No ISOs found on this connection"
                  options={isos.map((t) => ({ value: t.volid, label: `${t.volid.split("/").pop()} (${t.node})` }))}
                />
              </div>
              <div className="space-y-1.5">
                <Label>IP config (cloud-init)</Label>
                <Input value={vmForm.ipConfig} onChange={(e) => setVmForm({ ...vmForm, ipConfig: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Cloud-init user</Label>
                <Input value={vmForm.ciUser} onChange={(e) => setVmForm({ ...vmForm, ciUser: e.target.value })} placeholder="optional" />
              </div>
              <div className="space-y-1.5">
                <Label>Cloud-init password</Label>
                <Input type="password" value={vmForm.ciPassword} onChange={(e) => setVmForm({ ...vmForm, ciPassword: e.target.value })} placeholder="optional" />
              </div>
              <div className="col-span-2 space-y-1.5">
                <Label>SSH public key (optional)</Label>
                <Input value={vmForm.sshPublicKey} onChange={(e) => setVmForm({ ...vmForm, sshPublicKey: e.target.value })} />
              </div>
            </div>
            <p className="text-xs text-[var(--text-muted)]">
              Cloud-init fields only take effect when cloning from a cloud-init-capable template.
            </p>
            <div className="space-y-1.5">
              <Label>Advanced: extra disks/NICs/hardware (one key=value per line)</Label>
              <Textarea
                className="text-xs"
                rows={2}
                placeholder={"scsi1=local-lvm:32\nnet1=virtio,bridge=vmbr1"}
                value={vmForm.extraText}
                onChange={(e) => setVmForm({ ...vmForm, extraText: e.target.value })}
              />
            </div>
            <FormError message={createVM.error instanceof ApiError ? createVM.error.message : createVM.error ? "Failed to create VM" : undefined} />
            <DialogFooter className="flex-col items-end gap-1">
              {!vmForm.node ? (
                <p className="text-xs text-[var(--text-muted)]">Select a node to continue.</p>
              ) : vmInvalid ? (
                <p className="text-xs text-[var(--text-muted)]">Cores, memory (min 16 MB) and disk (min 1 GB) must be at least 1.</p>
              ) : null}
              <Button loading={createVM.isPending} disabled={!vmForm.node || vmInvalid} onClick={() => void submitVM()}>
                Create VM
              </Button>
            </DialogFooter>
          </TabsContent>

          <TabsContent value="lxc" className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Node</Label>
                <Combobox
                  value={lxcForm.node}
                  onChange={(v) => setLxcForm({ ...lxcForm, node: v })}
                  options={nodes.map((n) => ({ value: n.node!, label: n.node! }))}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Hostname</Label>
                <Input value={lxcForm.hostname} onChange={(e) => setLxcForm({ ...lxcForm, hostname: e.target.value })} />
              </div>
              <div className="col-span-2 space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label>Container template</Label>
                  <UploadTemplateDialog connId={connId} nodes={nodes} content="vztmpl" />
                </div>
                <Combobox
                  value={lxcForm.template}
                  onChange={(v) => setLxcForm({ ...lxcForm, template: v })}
                  placeholder={templatesQuery.isLoading ? "Loading..." : "Select a template..."}
                  searchPlaceholder="Search templates..."
                  emptyText="No container templates found — add one above"
                  options={vztmpls.map((t) => ({ value: t.volid, label: `${t.volid.split("/").pop()} (${t.node})` }))}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Cores</Label>
                <Input type="number" min={1} value={lxcForm.cores} onChange={(e) => setLxcForm({ ...lxcForm, cores: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Memory (MB)</Label>
                <Input type="number" min={16} value={lxcForm.memoryMb} onChange={(e) => setLxcForm({ ...lxcForm, memoryMb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Storage</Label>
                <Input value={lxcForm.storage} onChange={(e) => setLxcForm({ ...lxcForm, storage: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Disk (GB)</Label>
                <Input type="number" min={1} value={lxcForm.diskGb} onChange={(e) => setLxcForm({ ...lxcForm, diskGb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Network bridge</Label>
                <Input value={lxcForm.bridge} onChange={(e) => setLxcForm({ ...lxcForm, bridge: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Root password</Label>
                <Input type="password" value={lxcForm.password} onChange={(e) => setLxcForm({ ...lxcForm, password: e.target.value })} />
              </div>
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={lxcForm.unprivileged} onCheckedChange={(v) => setLxcForm({ ...lxcForm, unprivileged: v })} />
              Unprivileged container (recommended)
            </label>
            <div className="space-y-1.5">
              <Label>Advanced: extra mount points/NICs/hardware (one key=value per line)</Label>
              <Textarea
                className="text-xs"
                rows={2}
                placeholder={"mp0=local-lvm:8,mp=/data\nnet1=name=eth1,bridge=vmbr1"}
                value={lxcForm.extraText}
                onChange={(e) => setLxcForm({ ...lxcForm, extraText: e.target.value })}
              />
            </div>
            <FormError message={createLXC.error instanceof ApiError ? createLXC.error.message : createLXC.error ? "Failed to create container" : undefined} />
            <DialogFooter className="flex-col items-end gap-1">
              {!lxcForm.node ? (
                <p className="text-xs text-[var(--text-muted)]">Select a node to continue.</p>
              ) : !lxcForm.template ? (
                <p className="text-xs text-[var(--text-muted)]">Select a container template to continue.</p>
              ) : lxcInvalid ? (
                <p className="text-xs text-[var(--text-muted)]">Cores, memory (min 16 MB) and disk (min 1 GB) must be at least 1.</p>
              ) : null}
              <Button loading={createLXC.isPending} disabled={!lxcForm.node || !lxcForm.template || lxcInvalid} onClick={() => void submitLXC()}>
                Create Container
              </Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
