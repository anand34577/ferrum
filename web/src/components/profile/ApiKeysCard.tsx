import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, Copy, KeyRound, Plug, Plus, Terminal, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError, type ApiKey, type CreatedApiKey } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"

const EXPIRY_OPTIONS = [
  { value: "0", label: "Never" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
]

/**
 * Long-lived bearer tokens for 3rd-party apps, scripts, and the MCP
 * integration (see McpIntegrationCard). A key inherits whatever this
 * account's permissions currently are — same trust model as the session
 * cookie, just usable outside the browser. Scope is fixed at creation:
 * "api" keys work only against the general REST API, "mcp" keys only
 * against the MCP endpoint — never both, and MCP keys can only be minted
 * while an admin has MCP enabled instance-wide.
 */
export function ApiKeysCard() {
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState("")
  const [scope, setScope] = useState<"api" | "mcp">("api")
  const [expiresInDays, setExpiresInDays] = useState("0")
  const [created, setCreated] = useState<CreatedApiKey | null>(null)
  const [copied, setCopied] = useState(false)
  const confirm = useConfirm()

  const query = useQuery({
    queryKey: ["auth", "apikeys"],
    queryFn: () => api.get<ApiKey[]>("/auth/apikeys/"),
  })
  const mcpStatusQuery = useQuery({
    queryKey: ["auth", "agent-status"],
    queryFn: () => api.get<{ mcpEnabled: boolean; apiEnabled: boolean }>("/auth/agent-status"),
  })
  const mcpEnabled = mcpStatusQuery.data?.mcpEnabled ?? false
  const apiEnabled = mcpStatusQuery.data?.apiEnabled ?? true

  const create = useMutation({
    mutationFn: () =>
      api.post<CreatedApiKey>("/auth/apikeys/", {
        name,
        scope,
        expiresInDays: Number(expiresInDays) || undefined,
      }),
    onSuccess: (key) => {
      queryClient.invalidateQueries({ queryKey: ["auth", "apikeys"] })
      setCreateOpen(false)
      setName("")
      setScope("api")
      setExpiresInDays("0")
      setCreated(key)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create key"),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => api.delete(`/auth/apikeys/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["auth", "apikeys"] })
      toast.success("API key revoked")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to revoke key"),
  })

  async function handleRevoke(key: ApiKey) {
    const usedBy = key.scope === "mcp" ? "MCP clients using it" : "scripts/apps using it"
    if (await confirm({ title: `Revoke "${key.name}"?`, description: `${usedBy} will stop working immediately.` })) {
      revoke.mutate(key.id)
    }
  }

  function copyKey() {
    if (!created) return
    navigator.clipboard.writeText(created.key).then(() => {
      setCopied(true)
      toast.success("Copied to clipboard")
      setTimeout(() => setCopied(false), 2000)
    })
  }

  const keys = query.data ?? []

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="h-4 w-4" /> API keys
          </CardTitle>
          <CardDescription>For scripts, 3rd-party apps, and the MCP integration below.</CardDescription>
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="h-3.5 w-3.5" /> New key
        </Button>
      </CardHeader>
      <CardContent>
        {query.isError ? (
          <ErrorState title="Couldn't load API keys" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-2" aria-busy>
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-11 w-full" />
          </div>
        ) : keys.length === 0 ? (
          <p className="text-sm text-[var(--text-muted)]">No API keys yet. Create one to call the REST API or connect an MCP client.</p>
        ) : (
          <div className="space-y-2">
            {keys.map((key) => (
              <div key={key.id} className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] px-3 py-2.5">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate text-sm font-medium">{key.name}</span>
                    <code className="rounded-sm bg-[var(--bg-muted)] px-1.5 py-0.5 text-xs text-[var(--text-muted)]">{key.keyPrefix}…</code>
                    <Badge variant={key.scope === "mcp" ? "brand" : "outline"}>{key.scope === "mcp" ? "MCP" : "API"}</Badge>
                    {key.expiresAt && <Badge variant="default">Expires {formatRelativeTime(key.expiresAt)}</Badge>}
                  </div>
                  <p className="mt-0.5 text-xs text-[var(--text-muted)]">
                    Created {formatRelativeTime(key.createdAt)} · Last used {key.lastUsedAt ? formatRelativeTime(key.lastUsedAt) : "never"}
                  </p>
                </div>
                <Button size="icon-sm" variant="ghost" onClick={() => handleRevoke(key)} aria-label={`Revoke ${key.name}`}>
                  <Trash2 className="h-3.5 w-3.5 text-[var(--status-error)]" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>

      {/* Create dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New API key</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="key-name">Name</Label>
              <Input id="key-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Home Assistant, Claude MCP" autoFocus />
            </div>
            <div className="space-y-1.5">
              <Label>Purpose</Label>
              <Select value={scope} onValueChange={(v) => setScope(v as "api" | "mcp")}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="api" disabled={!apiEnabled}>
                    General API — scripts, 3rd-party apps{!apiEnabled && " (disabled by admin)"}
                  </SelectItem>
                  <SelectItem value="mcp" disabled={!mcpEnabled}>
                    <span className="flex items-center gap-1.5">
                      <Plug className="h-3.5 w-3.5" /> MCP — Claude &amp; other agents
                      {!mcpEnabled && " (disabled by admin)"}
                    </span>
                  </SelectItem>
                </SelectContent>
              </Select>
              {scope === "mcp" && (
                <p className="text-xs text-[var(--text-muted)]">This key will work ONLY against the MCP endpoint, not the general API.</p>
              )}
              {!mcpEnabled && (
                <p className="text-xs text-[var(--status-warn)]">
                  MCP is currently disabled for this Ferrum instance — ask an admin to enable it under Settings &gt; API &amp; MCP.
                </p>
              )}
              {!apiEnabled && (
                <p className="text-xs text-[var(--status-warn)]">
                  The REST API is currently disabled for this Ferrum instance — ask an admin to enable it under Settings &gt; API &amp; MCP.
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label>Expiration</Label>
              <Select value={expiresInDays} onValueChange={setExpiresInDays}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {EXPIRY_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button
              loading={create.isPending}
              disabled={!name.trim() || (scope === "mcp" && !mcpEnabled) || (scope === "api" && !apiEnabled)}
              onClick={() => create.mutate()}
            >
              Create key
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* One-time reveal dialog */}
      <Dialog open={!!created} onOpenChange={(open) => !open && setCreated(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Your new API key</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="flex items-center gap-2 rounded-md border border-[var(--status-warn)] bg-[color-mix(in_oklab,var(--status-warn)_10%,transparent)] px-3 py-2 text-sm">
              <Terminal className="h-4 w-4 shrink-0 text-[var(--status-warn)]" />
              Copy this now — it won't be shown again. Anyone with this key can act as you.
            </div>
            <div className="flex items-center gap-2">
              <code className="flex-1 overflow-x-auto rounded-md bg-[var(--bg-muted)] px-3 py-2 text-sm">{created?.key}</code>
              <Button size="icon-sm" variant="secondary" onClick={copyKey} aria-label="Copy key">
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </Button>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setCreated(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
