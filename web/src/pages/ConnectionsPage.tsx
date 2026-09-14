import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Download, Network, Pencil, Plus, Trash2, Wifi } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { useFormDirty } from "@/components/settings/use-form-dirty"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Textarea } from "@/components/ui/textarea"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type Connection, type ConnectionInventory } from "@/lib/api"

interface FormState {
  name: string
  type: "pve" | "pbs"
  host: string
  port: number
  authType: "token" | "password"
  username: string
  password: string
  tokenId: string
  tokenSecret: string
  verifyTls: boolean
  behindReverseProxy: boolean
  // "" means SSH access isn't configured for this connection at all.
  sshAuthType: "" | "password" | "key"
  sshUsername: string
  sshPort: number
  sshPassword: string
  sshPrivateKey: string
}

const emptyForm: FormState = {
  name: "",
  type: "pve",
  host: "",
  port: 8006,
  authType: "token",
  username: "",
  password: "",
  tokenId: "",
  tokenSecret: "",
  verifyTls: true,
  behindReverseProxy: false,
  sshAuthType: "",
  sshUsername: "",
  sshPort: 22,
  sshPassword: "",
  sshPrivateKey: "",
}

const DEFAULT_PORT: Record<FormState["type"], number> = { pve: 8006, pbs: 8007 }

export function ConnectionsPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  // Dirty baseline: a snapshot of the form as it was when create/edit began.
  // startEdit puts blanks in the secret fields (the API never returns stored
  // credentials), so this is the form's own initial state — never raw server
  // data. startCreate/startEdit always re-snapshot both, so dirty can't go
  // stale across opens; the save-success handlers reset them together.
  const [formInitial, setFormInitial] = useState<FormState>(emptyForm)
  const formDirty = useFormDirty(form, formInitial)

  function startCreate() {
    setEditingId(null)
    setForm(emptyForm)
    setFormInitial(emptyForm)
    setShowForm(true)
  }

  function startEdit(conn: Connection) {
    setEditingId(conn.id)
    const next: FormState = {
      name: conn.name,
      type: conn.type,
      host: conn.host,
      port: conn.port,
      authType: conn.authType,
      username: conn.username ?? "",
      password: "",
      tokenId: conn.tokenId ?? "",
      tokenSecret: "",
      verifyTls: conn.verifyTls,
      behindReverseProxy: conn.behindReverseProxy,
      sshAuthType: (conn.sshAuthType as FormState["sshAuthType"]) || "",
      sshUsername: conn.sshUsername ?? "",
      sshPort: conn.sshPort || 22,
      sshPassword: "",
      sshPrivateKey: "",
    }
    setForm(next)
    setFormInitial(next)
    setShowForm(true)
  }

  const { data, isLoading, isError, refetch, isRefetching } = useQuery({
    queryKey: ["connections"],
    queryFn: () => api.get<Connection[]>("/connections/"),
  })
  const { data: inventory } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })
  const liveStatus = new Map((inventory ?? []).map((c) => [c.connectionId, c]))

  const testMutation = useMutation({
    mutationFn: () => api.post<{ status: string; version: string }>("/connections/test", form),
    onSuccess: (res) => toast.success(`Connected — ${form.type === "pbs" ? "PBS" : "PVE"} ${res.version}`),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Test failed"),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post("/connections/", form),
    onSuccess: () => {
      toast.success("Connection added")
      queryClient.invalidateQueries({ queryKey: ["connections"] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setForm(emptyForm)
      setFormInitial(emptyForm)
      setShowForm(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add connection"),
  })

  const updateMutation = useMutation({
    mutationFn: () => {
      // Only send secret fields if the user actually typed a new one — an
      // empty field means "keep the existing credential".
      const body: Record<string, unknown> = {
        name: form.name,
        type: form.type,
        host: form.host,
        port: form.port,
        authType: form.authType,
        verifyTls: form.verifyTls,
        behindReverseProxy: form.behindReverseProxy,
      }
      if (form.authType === "token") {
        body.tokenId = form.tokenId
        if (form.tokenSecret) body.tokenSecret = form.tokenSecret
      } else {
        body.username = form.username
        if (form.password) body.password = form.password
      }
      body.sshAuthType = form.sshAuthType
      if (form.sshAuthType) {
        body.sshUsername = form.sshUsername
        body.sshPort = form.sshPort
        if (form.sshPassword) body.sshPassword = form.sshPassword
        if (form.sshPrivateKey) body.sshPrivateKey = form.sshPrivateKey
      }
      return api.put(`/connections/${editingId}`, body)
    },
    onSuccess: () => {
      toast.success("Connection updated")
      queryClient.invalidateQueries({ queryKey: ["connections"] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setForm(emptyForm)
      setFormInitial(emptyForm)
      setEditingId(null)
      setShowForm(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update connection"),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/connections/${id}`),
    onSuccess: () => {
      toast.success("Connection removed")
      queryClient.invalidateQueries({ queryKey: ["connections"] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove connection"),
  })

  // The export endpoints return a raw file (Content-Disposition: attachment),
  // not JSON — bypasses the api.ts helper's JSON parsing and triggers a real
  // browser save via a throwaway object-URL link, same technique as
  // TopologyPage's SVG export.
  async function downloadExport(connId: string, format: "terraform" | "ansible", filename: string) {
    try {
      const res = await fetch(`/api/v1/connections/${connId}/export/${format}`, { credentials: "include" })
      if (!res.ok) throw new Error(`Export failed (${res.status})`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = filename
      a.click()
      URL.revokeObjectURL(url)
    } catch {
      toast.error(`Couldn't export as ${format === "terraform" ? "Terraform" : "Ansible"}`)
    }
  }

  async function removeConnection(conn: Connection) {
    const ok = await confirm({
      title: `Remove ${conn.name}?`,
      description: "Ferrum stops managing this host or cluster. Its stored credentials are deleted; your Proxmox server itself is untouched.",
      confirmLabel: "Remove",
    })
    if (ok) deleteMutation.mutate(conn.id)
  }

  const saving = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-4">
      <PageHeader
        title="Connections"
        description="Proxmox clusters and standalone hosts."
        icon={Network}
        onRefresh={() => {
          void queryClient.invalidateQueries({ queryKey: ["connections"] })
          void queryClient.invalidateQueries({ queryKey: ["inventory"] })
        }}
        refreshing={isRefetching}
        actions={
          <Button onClick={() => (showForm ? setShowForm(false) : startCreate())}>
            <Plus className="h-4 w-4" /> Add connection
          </Button>
        }
      />

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle>{editingId ? "Edit connection" : "New connection"}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Name</Label>
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Type</Label>
                <Select
                  value={form.type}
                  onValueChange={(v) => {
                    const type = v as FormState["type"]
                    // Only nudge the port when it's still at the other type's
                    // default — an admin who already typed a custom port
                    // shouldn't have it silently overwritten by switching type.
                    const port = form.port === DEFAULT_PORT[form.type] ? DEFAULT_PORT[type] : form.port
                    setForm({ ...form, type, port })
                  }}
                  disabled={!!editingId}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="pve">Proxmox VE (cluster or host)</SelectItem>
                    <SelectItem value="pbs">Proxmox Backup Server</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Host</Label>
                <Input
                  placeholder="10.0.0.5 or pve.example.com"
                  value={form.host}
                  onChange={(e) => setForm({ ...form, host: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Port</Label>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={form.port}
                  // Clearing the field or typing 0 would otherwise PUT a
                  // portless/invalid connection — snap back to the type's
                  // default on blur instead.
                  onBlur={() => setForm((f) => ({ ...f, port: f.port >= 1 && f.port <= 65535 ? f.port : DEFAULT_PORT[f.type] }))}
                  onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Auth type</Label>
                <Select value={form.authType} onValueChange={(v) => setForm({ ...form, authType: v as FormState["authType"] })}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="token">API Token</SelectItem>
                    <SelectItem value="password">Username / Password</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {form.authType === "token" ? (
                <>
                  <div className="space-y-1.5">
                    <Label>Token ID</Label>
                    <Input
                      placeholder="root@pam!ferrum"
                      autoComplete="off"
                      value={form.tokenId}
                      onChange={(e) => setForm({ ...form, tokenId: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>{editingId ? "Token secret (leave blank to keep current)" : "Token secret"}</Label>
                    <Input
                      type="password"
                      autoComplete="new-password"
                      value={form.tokenSecret}
                      onChange={(e) => setForm({ ...form, tokenSecret: e.target.value })}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="space-y-1.5">
                    <Label>Username</Label>
                    <Input
                      placeholder="root@pam"
                      autoComplete="off"
                      value={form.username}
                      onChange={(e) => setForm({ ...form, username: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>{editingId ? "Password (leave blank to keep current)" : "Password"}</Label>
                    <Input
                      type="password"
                      autoComplete="new-password"
                      value={form.password}
                      onChange={(e) => setForm({ ...form, password: e.target.value })}
                    />
                  </div>
                </>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-4 text-sm">
              <label className="flex items-center gap-2">
                <Checkbox checked={form.verifyTls} onCheckedChange={(v) => setForm({ ...form, verifyTls: v === true })} />
                Verify TLS certificate
              </label>
              <label className="flex items-center gap-2">
                <Checkbox
                  checked={form.behindReverseProxy}
                  onCheckedChange={(v) => setForm({ ...form, behindReverseProxy: v === true })}
                />
                Behind reverse proxy
              </label>
            </div>

            <div className="space-y-3 rounded-md border border-[var(--border)] p-3">
              <div className="space-y-1.5">
                <Label>SSH access (optional)</Label>
                <p className="text-xs text-[var(--text-muted)]">
                  Lets "SSH Shell" in Inventory connect straight to this host without retyping a password every time.
                </p>
                <Select value={form.sshAuthType || "none"} onValueChange={(v) => setForm({ ...form, sshAuthType: v === "none" ? "" : (v as FormState["sshAuthType"]) })}>
                  <SelectTrigger className="max-w-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">Not configured</SelectItem>
                    <SelectItem value="password">Username / password</SelectItem>
                    <SelectItem value="key">Username / private key</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {form.sshAuthType && (
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label>SSH username</Label>
                    <Input placeholder="root" autoComplete="off" value={form.sshUsername} onChange={(e) => setForm({ ...form, sshUsername: e.target.value })} />
                  </div>
                  <div className="space-y-1.5">
                    <Label>SSH port</Label>
                    <Input type="number" min={1} max={65535} value={form.sshPort} onChange={(e) => setForm({ ...form, sshPort: Number(e.target.value) })} />
                  </div>
                  {form.sshAuthType === "password" ? (
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label>{editingId ? "SSH password (leave blank to keep current)" : "SSH password"}</Label>
                      <Input type="password" autoComplete="new-password" value={form.sshPassword} onChange={(e) => setForm({ ...form, sshPassword: e.target.value })} />
                    </div>
                  ) : (
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label>{editingId ? "SSH private key (leave blank to keep current)" : "SSH private key"}</Label>
                      <Textarea
                        rows={4}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        className="font-mono text-xs"
                        value={form.sshPrivateKey}
                        onChange={(e) => setForm({ ...form, sshPrivateKey: e.target.value })}
                      />
                      <p className="text-xs text-[var(--text-muted)]">Unencrypted (no passphrase) keys only, for now.</p>
                    </div>
                  )}
                </div>
              )}
            </div>

            <FormError
              message={(() => {
                const err = testMutation.error ?? (editingId ? updateMutation.error : createMutation.error)
                return err instanceof ApiError ? err.message : err ? "Something went wrong — try again." : undefined
              })()}
            />

            {(!form.name || !form.host) && (
              <p className="text-xs text-[var(--text-muted)]">Name and host are required.</p>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                variant="secondary"
                onClick={() => testMutation.mutate()}
                loading={testMutation.isPending}
                disabled={!form.host}
              >
                {!testMutation.isPending && <Wifi className="h-4 w-4" />}
                Test connection
              </Button>
              <Button
                onClick={() => (editingId ? updateMutation.mutate() : createMutation.mutate())}
                loading={saving}
                disabled={!formDirty || !form.name || !form.host}
              >
                {editingId ? "Save changes" : "Save connection"}
              </Button>
              <Button
                variant="ghost"
                onClick={() => {
                  setShowForm(false)
                  setEditingId(null)
                }}
              >
                Cancel
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="space-y-2">
        {isError && <ErrorState title="Couldn't load connections" onRetry={refetch} />}
        {!isError && isLoading && (
          <div className="space-y-2" aria-busy>
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-[4.5rem]" style={{ opacity: 1 - i * 0.2 }} />
            ))}
          </div>
        )}
        {!isError && !isLoading && (data ?? []).length === 0 && !showForm && (
          <EmptyState
            icon={Network}
            title="No connections yet"
            description="Add your first Proxmox host or cluster — Ferrum reaches it over HTTPS with an API token or a username/password."
          />
        )}
        {!isError && data?.map((conn) => {
          const status = liveStatus.get(conn.id)
          return (
            <Card key={conn.id}>
              <CardContent className="flex flex-wrap items-center justify-between gap-2 py-3">
                <div className="min-w-0 flex-1">
                  <p className="flex min-w-0 items-center gap-2 font-medium">
                    {status && (
                      <span className="flex shrink-0 items-center" title={status.online ? "Reachable" : "Unreachable"}>
                        <StatusDot status={status.online ? "ok" : "error"} />
                        <span className="sr-only">{status.online ? "Reachable" : "Unreachable"}</span>
                      </span>
                    )}
                    <span className="truncate">{conn.name}</span>
                    {/* The reason a connection is down used to live only in a
                        hover tooltip — print it like Overview does, since
                        "red dot" alone doesn't fix a dead host. */}
                    {status && !status.online && (
                      <span className="hidden min-w-0 truncate text-xs font-normal text-[var(--status-error)] sm:inline" title={status.error}>
                        {status.error ?? "unreachable"}
                      </span>
                    )}
                  </p>
                  <p className="truncate text-xs text-[var(--text-muted)]">
                    {conn.host}:{conn.port} · {conn.authType === "token" ? "API token" : "username / password"}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Badge variant="outline">{conn.type === "pbs" ? "PBS" : "PVE"}</Badge>
                  <Badge variant={conn.verifyTls ? "ok" : "default"}>{conn.verifyTls ? "TLS verified" : "TLS insecure"}</Badge>
                  {conn.type === "pve" && (
                    <DropdownMenu>
                      <Hint label="Export inventory as IaC">
                        <DropdownMenuTrigger asChild>
                          <Button size="icon" variant="ghost" aria-label={`Export ${conn.name} inventory`}>
                            <Download className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                      </Hint>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onSelect={() => downloadExport(conn.id, "terraform", `${conn.name}.tf`)}>Terraform (.tf)</DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => downloadExport(conn.id, "ansible", `${conn.name}-inventory.yml`)}>
                          Ansible inventory (.yml)
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
                  <Hint label="Edit connection">
                    <Button size="icon" variant="ghost" onClick={() => startEdit(conn)} aria-label={`Edit ${conn.name}`}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </Hint>
                  <Hint label="Remove connection">
                    <Button
                      size="icon"
                      variant="ghost-danger"
                      onClick={() => removeConnection(conn)}
                      aria-label={`Remove ${conn.name}`}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </Hint>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
