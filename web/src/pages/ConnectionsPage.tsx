import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Network, Pencil, Plus, Trash2, Wifi } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
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
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type Connection, type ConnectionInventory } from "@/lib/api"

interface FormState {
  name: string
  host: string
  port: number
  authType: "token" | "password"
  username: string
  password: string
  tokenId: string
  tokenSecret: string
  verifyTls: boolean
  behindReverseProxy: boolean
}

const emptyForm: FormState = {
  name: "",
  host: "",
  port: 8006,
  authType: "token",
  username: "",
  password: "",
  tokenId: "",
  tokenSecret: "",
  verifyTls: false,
  behindReverseProxy: false,
}

export function ConnectionsPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)

  function startCreate() {
    setEditingId(null)
    setForm(emptyForm)
    setShowForm(true)
  }

  function startEdit(conn: Connection) {
    setEditingId(conn.id)
    setForm({
      name: conn.name,
      host: conn.host,
      port: conn.port,
      authType: conn.authType,
      username: conn.username ?? "",
      password: "",
      tokenId: conn.tokenId ?? "",
      tokenSecret: "",
      verifyTls: conn.verifyTls,
      behindReverseProxy: conn.behindReverseProxy,
    })
    setShowForm(true)
  }

  const { data, isLoading, isError, refetch } = useQuery({
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
    onSuccess: (res) => toast.success(`Connected — PVE ${res.version}`),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Test failed"),
  })

  const createMutation = useMutation({
    mutationFn: () => api.post("/connections/", form),
    onSuccess: () => {
      toast.success("Connection added")
      queryClient.invalidateQueries({ queryKey: ["connections"] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setForm(emptyForm)
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
      return api.put(`/connections/${editingId}`, body)
    },
    onSuccess: () => {
      toast.success("Connection updated")
      queryClient.invalidateQueries({ queryKey: ["connections"] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setForm(emptyForm)
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
                  value={form.port}
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
                      value={form.tokenId}
                      onChange={(e) => setForm({ ...form, tokenId: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>{editingId ? "Token secret (leave blank to keep current)" : "Token secret"}</Label>
                    <Input
                      type="password"
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
                      value={form.username}
                      onChange={(e) => setForm({ ...form, username: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>{editingId ? "Password (leave blank to keep current)" : "Password"}</Label>
                    <Input
                      type="password"
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

            <FormError
              message={(() => {
                const err = testMutation.error ?? (editingId ? updateMutation.error : createMutation.error)
                return err instanceof ApiError ? err.message : err ? "Something went wrong — try again." : undefined
              })()}
            />

            <div className="flex flex-wrap gap-2">
              <Button
                variant="secondary"
                onClick={() => testMutation.mutate()}
                loading={testMutation.isPending}
                disabled={!form.host}
              >
                {!testMutation.isPending && <Wifi className="h-4 w-4" />}
                Test
              </Button>
              <Button
                onClick={() => (editingId ? updateMutation.mutate() : createMutation.mutate())}
                loading={saving}
                disabled={!form.name || !form.host}
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
                  <p className="flex items-center gap-2 truncate font-medium">
                    {status && <StatusDot status={status.online ? "ok" : "error"} />}
                    <span className="truncate">{conn.name}</span>
                  </p>
                  <p className="truncate text-xs text-[var(--text-muted)]">
                    {conn.host}:{conn.port} · {conn.authType === "token" ? "API token" : "username / password"}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {status && (
                    <span className="flex shrink-0 items-center" title={status.online ? "Online" : "Unreachable"}>
                      <StatusDot status={status.online ? "ok" : "error"} />
                    </span>
                  )}
                  <Badge variant={conn.verifyTls ? "ok" : "default"}>{conn.verifyTls ? "TLS verified" : "TLS insecure"}</Badge>
                  <Hint label="Edit connection">
                    <Button size="icon" variant="ghost" onClick={() => startEdit(conn)} aria-label={`Edit ${conn.name}`}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </Hint>
                  <Hint label="Remove connection">
                    <Button
                      size="icon"
                      variant="ghost"
                      className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
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
