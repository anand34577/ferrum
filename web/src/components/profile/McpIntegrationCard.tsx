import { useQuery } from "@tanstack/react-query"
import { Check, Copy, Plug, ShieldOff } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import { useCopiedFlag } from "@/lib/useCopiedFlag"

function CopyBlock({ text }: { text: string }) {
  const [copied, flashCopied] = useCopiedFlag(2000)
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      flashCopied()
      toast.success("Copied to clipboard")
    }).catch(() => toast.error("Could not copy to clipboard"))
  }
  return (
    <div className="flex items-start gap-2">
      <pre className="flex-1 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-[var(--bg-muted)] px-3 py-2 font-mono text-xs leading-relaxed">{text}</pre>
      <Button size="icon-sm" variant="secondary" onClick={copy} aria-label="Copy">
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </Button>
    </div>
  )
}

/**
 * Ready-to-paste config for adding Ferrum as a remote MCP server in Claude
 * Code / Claude Desktop (or any other MCP-capable agent). Requires an
 * MCP-scoped API key (created above, with "Purpose: MCP") — a general API
 * key will NOT work here — and an admin must have enabled MCP instance-wide
 * (Settings > API & MCP); both are enforced server-side regardless of
 * what the UI shows.
 */
export function McpIntegrationCard() {
  const mcpStatusQuery = useQuery({
    queryKey: ["auth", "agent-status"],
    queryFn: () => api.get<{ mcpEnabled: boolean }>("/auth/agent-status"),
  })
  const mcpEnabled = mcpStatusQuery.data?.mcpEnabled ?? false

  const mcpUrl = `${window.location.origin}/mcp`
  const claudeCodeCmd = `claude mcp add --transport http ferrum ${mcpUrl} --header "Authorization: Bearer <your-mcp-key>"`
  const desktopConfig = JSON.stringify(
    { mcpServers: { ferrum: { url: mcpUrl, headers: { Authorization: "Bearer <your-mcp-key>" } } } },
    null,
    2,
  )

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle className="flex items-center gap-2">
            <Plug className="h-4 w-4" /> MCP (Claude &amp; agent integration)
          </CardTitle>
          <CardDescription>
            Connect Claude Code, Claude Desktop, or any other MCP-capable agent so it can query and operate your fleet directly.
          </CardDescription>
        </div>
        {!mcpStatusQuery.isLoading && (
          <Badge variant={mcpEnabled ? "ok" : "default"}>{mcpEnabled ? "Enabled" : "Disabled"}</Badge>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {!mcpEnabled ? (
          <div className="flex items-start gap-2.5 rounded-md border border-[var(--border)] bg-[var(--bg-muted)] px-3 py-2.5 text-sm text-[var(--text-muted)]">
            <ShieldOff className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              MCP is disabled for this Ferrum instance. An admin must turn it on under <strong>Settings &gt; API &amp; MCP</strong> before
              any MCP client — including this one — can connect.
            </span>
          </div>
        ) : (
          <p className="text-xs text-[var(--text-muted)]">
            Create an API key above with <strong>Purpose: MCP</strong> — it works only against this endpoint, never the general REST API —
            then paste it in place of <code className="rounded-sm bg-[var(--bg-muted)] px-1">&lt;your-mcp-key&gt;</code> below.
          </p>
        )}
        <div className="space-y-1.5">
          <p className="text-sm font-medium">Claude Code</p>
          <CopyBlock text={claudeCodeCmd} />
        </div>
        <div className="space-y-1.5">
          <p className="text-sm font-medium">Claude Desktop (claude_desktop_config.json)</p>
          <CopyBlock text={desktopConfig} />
        </div>
        <p className="text-xs text-[var(--text-muted)]">
          Read-only tools (listing connections, nodes, guests, storage, pools, alerts) work for any account. Power actions
          (start/stop/reboot) require the key to belong to an admin account. Every tool call — successful or not — is recorded and
          visible under Profile &gt; Activity and, for admins, in the Audit Log.
        </p>
      </CardContent>
    </Card>
  )
}
