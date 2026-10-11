import { useMutation, useQuery } from "@tanstack/react-query"
import { KeyRound, Terminal } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { api, ApiError, type Connection } from "@/lib/api"
import { buildSSHUrl, openConsolePopup } from "@/lib/console"

interface SSHShellDialogProps {
  /** Pre-fills the Host field (e.g. a guest's known agent-reported IP) — still editable, never assumed correct. */
  defaultHost?: string
  /** When this connection has SSH credentials configured (Settings → Connections), offers a one-click connect using them instead of the manual form. */
  connId?: string
}

// A direct SSH shell — bypasses Proxmox entirely (no PVE connection, no
// termproxy ticket), for when a node/guest's own Proxmox console isn't an
// option (termproxy unreachable, no qemu-guest-agent) or a real SSH session
// is just what's wanted. The password travels once, in this dialog's own
// authenticated POST, never in the popup URL — see openSSHShell (backend)
// and openConsolePopup (lib/console.ts).
export function SSHShellDialog({ defaultHost = "", connId }: SSHShellDialogProps) {
  const [open, setOpen] = useState(false)
  const [host, setHost] = useState(defaultHost)
  const [port, setPort] = useState("22")
  const [username, setUsername] = useState("")
  const [authType, setAuthType] = useState<"password" | "key">("password")
  const [password, setPassword] = useState("")
  const [privateKey, setPrivateKey] = useState("")

  // Reuses the Connections page's own query cache (same key) — no extra
  // request when that page has already been visited this session.
  const { data: connections } = useQuery({
    queryKey: ["connections"],
    queryFn: () => api.get<Connection[]>("/connections/"),
    enabled: open && !!connId,
  })
  const savedConn = connections?.find((c) => c.id === connId && c.sshAuthType)

  const secret = authType === "key" ? privateKey : password
  const connectMutation = useMutation({
    mutationFn: () =>
      api.post<{ wsPath: string }>("/ssh/sessions", {
        host,
        port: Number(port) || 22,
        username,
        authType,
        ...(authType === "key" ? { key: privateKey } : { password }),
        cols: 80,
        rows: 24,
      }),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "SSH connection failed"),
  })
  const connectWithSavedMutation = useMutation({
    mutationFn: () => api.post<{ wsPath: string }>(`/connections/${connId}/ssh-session`, { cols: 80, rows: 24 }),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "SSH connection failed"),
  })

  function connect() {
    openConsolePopup(buildSSHUrl(`${username || "ssh"}@${host}`), "width=900,height=600", () => connectMutation.mutateAsync())
    setOpen(false)
    setPassword("") // never held longer than the click that sends it
    setPrivateKey("")
  }

  function connectWithSaved() {
    if (!savedConn) return
    openConsolePopup(buildSSHUrl(`${savedConn.sshUsername || "ssh"}@${savedConn.name}`), "width=900,height=600", () => connectWithSavedMutation.mutateAsync())
    setOpen(false)
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) { setPassword(""); setPrivateKey("") } }}>
      <DialogTrigger asChild>
        <Button size="sm" variant="secondary">
          <Terminal className="h-3.5 w-3.5" /> SSH Shell
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>SSH Shell</DialogTitle>
          <DialogDescription>Connect directly over SSH — bypasses Proxmox's own console entirely.</DialogDescription>
        </DialogHeader>

        {savedConn && (
          <div className="space-y-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] p-3">
            <p className="text-sm">
              Use <span className="font-medium">{savedConn.sshUsername}</span>@{savedConn.host} — saved on this connection
            </p>
            <Button size="sm" loading={connectWithSavedMutation.isPending} onClick={connectWithSaved}>
              <KeyRound className="h-3.5 w-3.5" /> Connect
            </Button>
          </div>
        )}

        <div className="space-y-4">
          {savedConn && <p className="text-xs text-[var(--text-muted)]">Or connect to a different host manually:</p>}
          <div className="space-y-1.5">
            <Label>Host</Label>
            <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="10.0.0.5" />
          </div>
          <div className="space-y-1.5">
            <Label>Port</Label>
            <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="22" />
          </div>
          <div className="space-y-1.5">
            <Label>Username</Label>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="root" />
          </div>
          <div className="space-y-1.5">
            <Label>Authenticate with</Label>
            <Select value={authType} onValueChange={(v) => setAuthType(v as "password" | "key")}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="password">Password</SelectItem>
                <SelectItem value="key">Private key</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {authType === "password" ? (
            <div className="space-y-1.5">
              <Label>Password</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && host && username && secret) connect() }}
              />
            </div>
          ) : (
            <div className="space-y-1.5">
              <Label>Private key</Label>
              <Textarea
                rows={4}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                className="font-mono text-xs"
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
              />
              <p className="text-xs text-[var(--text-muted)]">Unencrypted (no passphrase) keys only, for now.</p>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button size="sm" variant="secondary" disabled={!host || !username || !secret} onClick={connect}>
            <Terminal className="h-3.5 w-3.5" /> Connect
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
