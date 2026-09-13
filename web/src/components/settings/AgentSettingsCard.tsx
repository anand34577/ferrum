import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bot, ShieldAlert } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError, type AgentSettings } from "@/lib/api"

/**
 * Instance-wide controls for the AI Assistant's agent loop and the MCP
 * endpoint — both admin-only and both safe-by-default: MCP starts disabled
 * (external agents can't reach live infrastructure until an admin opts in),
 * and the tool-call ceiling is a number here, not a hardcoded constant that
 * would need a code change to raise.
 */
export function AgentSettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "agent"],
    queryFn: () => api.get<AgentSettings>("/admin/settings/agent"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Bot className="h-4 w-4" /> API &amp; MCP
        </CardTitle>
        <CardDescription>Controls whether 3rd-party apps can use Ferrum's REST API, and what the AI Assistant's tool-calling loop and external MCP clients (Claude, etc.) can do.</CardDescription>
      </CardHeader>
      <CardContent>
        {query.isError ? (
          <ErrorState title="Couldn't load agent settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </div>
        ) : (
          <AgentSettingsForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function AgentSettingsForm({ initial }: { initial: AgentSettings }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState(initial)

  const save = useMutation({
    mutationFn: (next: AgentSettings) => api.put<AgentSettings>("/admin/settings/agent", next),
    onSuccess: (data) => {
      // This form saves instantly on every toggle/blur — without a toast the
      // admin gets zero confirmation the change reached the server.
      toast.success("Saved")
      queryClient.setQueryData(["admin", "settings", "agent"], data)
      queryClient.invalidateQueries({ queryKey: ["auth", "agent-status"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save"),
  })

  function update(next: AgentSettings) {
    setForm(next)
    save.mutate(next)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Enable REST API</p>
          <p className="text-xs text-[var(--text-muted)]">
            Lets 3rd-party applications call Ferrum's API using a user-generated API key (<code className="rounded-sm bg-[var(--bg-muted)] px-1">Authorization: Bearer …</code>).
            On by default. Turning this off immediately rejects every API-key request — existing keys are kept, not deleted, so re-enabling restores access without reissuing tokens.
          </p>
        </div>
        <Switch aria-label="Enable REST API" checked={form.apiEnabled} onCheckedChange={(v) => update({ ...form, apiEnabled: v })} />
      </div>

      {!form.apiEnabled && (
        <div className="flex items-start gap-2.5 rounded-md border border-[color-mix(in_oklab,var(--status-warn)_30%,transparent)] bg-[color-mix(in_oklab,var(--status-warn)_8%,transparent)] px-3 py-2.5 text-xs text-[var(--text-muted)]">
          <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--status-warn)]" />
          The REST API is currently off. Users cannot create general API keys, and every existing API key is rejected.
        </div>
      )}

      <div className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Enable MCP</p>
          <p className="text-xs text-[var(--text-muted)]">
            Lets external MCP clients (Claude Code, Claude Desktop, or any other MCP-capable agent) connect to <code className="rounded-sm bg-[var(--bg-muted)] px-1">/mcp</code> using
            a user-generated MCP token. Off by default — nothing outside Ferrum can reach this endpoint until you enable it.
          </p>
        </div>
        <Switch aria-label="Enable MCP" checked={form.mcpEnabled} onCheckedChange={(v) => update({ ...form, mcpEnabled: v })} />
      </div>

      {!form.mcpEnabled && (
        <div className="flex items-start gap-2.5 rounded-md border border-[color-mix(in_oklab,var(--status-warn)_30%,transparent)] bg-[color-mix(in_oklab,var(--status-warn)_8%,transparent)] px-3 py-2.5 text-xs text-[var(--text-muted)]">
          <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--status-warn)]" />
          MCP is currently off. Users cannot create MCP-scoped API keys, and the <code className="rounded-sm bg-[var(--bg-muted)] px-1">/mcp</code> endpoint rejects every request.
        </div>
      )}

      <div className="max-w-xs space-y-1.5">
        <Label htmlFor="max-tool-iterations">Max tool calls per message</Label>
        <Input
          id="max-tool-iterations"
          type="number"
          min={1}
          max={50}
          value={form.maxToolIterations}
          onChange={(e) => setForm({ ...form, maxToolIterations: Number(e.target.value) })}
          onBlur={() => {
            const clamped = Math.min(50, Math.max(1, form.maxToolIterations || 1))
            update({ ...form, maxToolIterations: clamped })
          }}
        />
        <p className="text-xs text-[var(--text-muted)]">
          How many tool round-trips the AI Assistant can make while answering one message before it must give a final answer.
          Raise it for complex multi-step questions, lower it to bound cost/latency against a slow provider.
        </p>
      </div>

      <p className="text-xs text-[var(--text-faint)]">Changes are saved automatically.</p>
    </div>
  )
}
