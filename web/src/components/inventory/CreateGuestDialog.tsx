import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, ApiError, type ClusterResource, type TemplateItem } from "@/lib/api"

interface CreateGuestDialogProps {
  connId: string
  nodes: ClusterResource[]
  open: boolean
  onOpenChange: (open: boolean) => void
}

const vmDefaults = {
  node: "", name: "", cores: 2, memoryMb: 2048, storage: "local-lvm", diskGb: 20,
  bridge: "vmbr0", iso: "", ciUser: "", ciPassword: "", sshPublicKey: "", ipConfig: "ip=dhcp",
}

const lxcDefaults = {
  node: "", hostname: "", cores: 2, memoryMb: 1024, storage: "local-lvm", diskGb: 8,
  template: "", bridge: "vmbr0", password: "", sshPublicKey: "", unprivileged: true,
}

export function CreateGuestDialog({ connId, nodes, open, onOpenChange }: CreateGuestDialogProps) {
  const queryClient = useQueryClient()
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

  const createVM = useMutation({
    mutationFn: () => api.post(`/connections/${connId}/vms`, vmForm),
    onSuccess: () => {
      toast.success("VM creation started")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      onOpenChange(false)
      setVmForm(vmDefaults)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create VM"),
  })

  const createLXC = useMutation({
    mutationFn: () => api.post(`/connections/${connId}/lxc`, lxcForm),
    onSuccess: () => {
      toast.success("Container creation started")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      onOpenChange(false)
      setLxcForm(lxcDefaults)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create container"),
  })

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
                <Select value={vmForm.node} onValueChange={(v) => setVmForm({ ...vmForm, node: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select..." />
                  </SelectTrigger>
                  <SelectContent>
                    {nodes.map((n) => <SelectItem key={n.node} value={n.node!}>{n.node}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Name</Label>
                <Input value={vmForm.name} onChange={(e) => setVmForm({ ...vmForm, name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Cores</Label>
                <Input type="number" value={vmForm.cores} onChange={(e) => setVmForm({ ...vmForm, cores: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Memory (MB)</Label>
                <Input type="number" value={vmForm.memoryMb} onChange={(e) => setVmForm({ ...vmForm, memoryMb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Storage</Label>
                <Input value={vmForm.storage} onChange={(e) => setVmForm({ ...vmForm, storage: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Disk (GB)</Label>
                <Input type="number" value={vmForm.diskGb} onChange={(e) => setVmForm({ ...vmForm, diskGb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Network bridge</Label>
                <Input value={vmForm.bridge} onChange={(e) => setVmForm({ ...vmForm, bridge: e.target.value })} />
              </div>
              <div className="col-span-2 space-y-1.5">
                <Label>Boot ISO (optional — omit when cloning a cloud-init template)</Label>
                <Select value={vmForm.iso} onValueChange={(v) => setVmForm({ ...vmForm, iso: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder={templatesQuery.isLoading ? "Loading..." : "None"} />
                  </SelectTrigger>
                  <SelectContent>
                    {isos.map((t) => (
                      <SelectItem key={t.volid} value={t.volid}>
                        {t.volid.split("/").pop()} <span className="text-[var(--text-muted)]">({t.node})</span>
                      </SelectItem>
                    ))}
                    {isos.length === 0 && !templatesQuery.isLoading && (
                      <SelectItem value="__none__" disabled>No ISOs found on this connection</SelectItem>
                    )}
                  </SelectContent>
                </Select>
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
            <FormError message={createVM.error instanceof ApiError ? createVM.error.message : createVM.error ? "Failed to create VM" : undefined} />
            <DialogFooter>
              <Button disabled={!vmForm.node || createVM.isPending} onClick={() => createVM.mutate()}>
                {createVM.isPending ? "Creating..." : "Create VM"}
              </Button>
            </DialogFooter>
          </TabsContent>

          <TabsContent value="lxc" className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Node</Label>
                <Select value={lxcForm.node} onValueChange={(v) => setLxcForm({ ...lxcForm, node: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select..." />
                  </SelectTrigger>
                  <SelectContent>
                    {nodes.map((n) => <SelectItem key={n.node} value={n.node!}>{n.node}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Hostname</Label>
                <Input value={lxcForm.hostname} onChange={(e) => setLxcForm({ ...lxcForm, hostname: e.target.value })} />
              </div>
              <div className="col-span-2 space-y-1.5">
                <Label>Container template</Label>
                <Select value={lxcForm.template} onValueChange={(v) => setLxcForm({ ...lxcForm, template: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder={templatesQuery.isLoading ? "Loading..." : "Select a template..."} />
                  </SelectTrigger>
                  <SelectContent>
                    {vztmpls.map((t) => (
                      <SelectItem key={t.volid} value={t.volid}>
                        {t.volid.split("/").pop()} <span className="text-[var(--text-muted)]">({t.node})</span>
                      </SelectItem>
                    ))}
                    {vztmpls.length === 0 && !templatesQuery.isLoading && (
                      <SelectItem value="__none__" disabled>No container templates found — download one in Proxmox first</SelectItem>
                    )}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Cores</Label>
                <Input type="number" value={lxcForm.cores} onChange={(e) => setLxcForm({ ...lxcForm, cores: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Memory (MB)</Label>
                <Input type="number" value={lxcForm.memoryMb} onChange={(e) => setLxcForm({ ...lxcForm, memoryMb: Number(e.target.value) })} />
              </div>
              <div className="space-y-1.5">
                <Label>Storage</Label>
                <Input value={lxcForm.storage} onChange={(e) => setLxcForm({ ...lxcForm, storage: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Disk (GB)</Label>
                <Input type="number" value={lxcForm.diskGb} onChange={(e) => setLxcForm({ ...lxcForm, diskGb: Number(e.target.value) })} />
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
            <FormError message={createLXC.error instanceof ApiError ? createLXC.error.message : createLXC.error ? "Failed to create container" : undefined} />
            <DialogFooter>
              <Button disabled={!lxcForm.node || !lxcForm.template || createLXC.isPending} onClick={() => createLXC.mutate()}>
                {createLXC.isPending ? "Creating..." : "Create Container"}
              </Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
