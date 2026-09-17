import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Bell, Check, Copy, KeyRound, ShieldCheck, ShieldOff, UserRound } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { ApiKeysCard } from "@/components/profile/ApiKeysCard"
import { McpIntegrationCard } from "@/components/profile/McpIntegrationCard"
import { SessionsCard } from "@/components/profile/SessionsCard"
import { AppearanceCard } from "@/components/settings/AppearanceCard"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

interface PersonalPreferences {
  notifyEmail: boolean
  landingPage: string
}

const LANDING_PAGES = [
  { value: "/", label: "Fleet Overview" },
  { value: "/dashboard", label: "Custom Dashboard" },
  { value: "/inventory", label: "Inventory" },
  { value: "/topology", label: "Topology" },
  { value: "/storage", label: "Storage" },
  { value: "/pools", label: "Resource Pools" },
  { value: "/ha", label: "High Availability" },
  { value: "/backups", label: "Backups" },
  { value: "/firewall", label: "Firewall" },
  { value: "/alerts", label: "Alerts" },
  { value: "/tasks", label: "Task Center" },
  // Kept in sync with Settings' new-account defaults (DefaultPreferencesCard).
  { value: "/ai-assistant", label: "AI Assistant" },
]

// Admin-only destinations — an admin who lives in the audit log or webhooks
// couldn't previously land there. Non-admins never see these (the routes
// themselves redirect non-admins).
const ADMIN_LANDING_PAGES = [
  { value: "/cluster", label: "Cluster & SDN" },
  { value: "/connections", label: "Connections" },
  { value: "/bulk-operations", label: "Bulk Operations" },
  { value: "/users", label: "Users" },
  { value: "/webhooks", label: "Webhooks" },
  { value: "/audit", label: "Audit Log" },
  { value: "/settings", label: "Settings" },
]

/** Personal notification opt-in and landing page — shares the
 * ["auth","preferences"] query key with ThemeProvider/AppearanceCard, so
 * this costs no extra request beyond what the page already makes. */
function PersonalPreferencesCard() {
  const query = useQuery({
    queryKey: ["auth", "preferences"],
    queryFn: () => api.get<PersonalPreferences>("/auth/me/preferences"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Bell className="h-4 w-4" /> Notifications &amp; navigation
        </CardTitle>
        <CardDescription>Personal to your account — doesn't change anyone else's.</CardDescription>
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <Skeleton className="h-20 w-full" />
        ) : (
          <PersonalPreferencesForm key={JSON.stringify(query.data)} initial={query.data ?? { notifyEmail: false, landingPage: "/" }} />
        )}
      </CardContent>
    </Card>
  )
}

function PersonalPreferencesForm({ initial }: { initial: PersonalPreferences }) {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const [form, setForm] = useState(initial)

  const save = useMutation({
    mutationFn: (next: PersonalPreferences) => api.put<PersonalPreferences>("/auth/me/preferences", next),
    onSuccess: (data) => {
      // Saves instantly on every toggle — say so, or the change reaching the
      // server is invisible.
      toast.success("Saved")
      queryClient.setQueryData(["auth", "preferences"], (old: object | undefined) => ({ ...old, ...data }))
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save"),
  })

  function update(next: PersonalPreferences) {
    setForm(next)
    save.mutate(next)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Email me alert notifications</p>
          <p className="text-xs text-[var(--text-muted)]">
            Sent to {user?.email || "your account email"} whenever an alert first triggers, on top of any admin-configured recipients.
            Requires the admin to have SMTP enabled in Settings.
          </p>
        </div>
        <Switch aria-label="Email me alert notifications" checked={form.notifyEmail} onCheckedChange={(v) => update({ ...form, notifyEmail: v })} />
      </div>
      <div className="max-w-xs space-y-1.5">
        <Label>Landing page</Label>
        <Select value={form.landingPage || "/"} onValueChange={(v) => update({ ...form, landingPage: v })}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            {(user?.isAdmin ? [...LANDING_PAGES, ...ADMIN_LANDING_PAGES] : LANDING_PAGES).map((p) => (
              <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-xs text-[var(--text-muted)]">Which page opens right after you sign in.</p>
      </div>
      <p className="text-xs text-[var(--text-faint)]">Changes are saved automatically.</p>
    </div>
  )
}

interface TotpStatus {
  enabled: boolean
  remainingRecoveryCodes: number
}

interface Enrollment {
  secret: string
  otpauthUrl: string
  qrCodePng: string
}

export function ProfilePage() {
  const { user, refresh } = useAuth()
  const queryClient = useQueryClient()
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const [code, setCode] = useState("")
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)
  const [disablePassword, setDisablePassword] = useState("")
  const [showDisable, setShowDisable] = useState(false)

  const statusQuery = useQuery({ queryKey: ["totp-status"], queryFn: () => api.get<TotpStatus>("/auth/2fa/status") })

  const enrollMutation = useMutation({
    mutationFn: () => api.post<Enrollment>("/auth/2fa/enroll"),
    onSuccess: (data) => setEnrollment(data),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to start enrollment"),
  })

  const confirmMutation = useMutation({
    mutationFn: () => api.post<{ recoveryCodes: string[] }>("/auth/2fa/confirm", { code }),
    onSuccess: (data) => {
      setRecoveryCodes(data.recoveryCodes)
      setEnrollment(null)
      setCode("")
      queryClient.invalidateQueries({ queryKey: ["totp-status"] })
      refresh()
      toast.success("Two-factor authentication enabled")
    },
    // Failure surfaces inline under the code field, not a toast — the code
    // being corrected is right there.
  })

  const disableMutation = useMutation({
    mutationFn: () => api.post("/auth/2fa/disable", { password: disablePassword }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["totp-status"] })
      refresh()
      setShowDisable(false)
      setDisablePassword("")
      toast.success("Two-factor authentication disabled")
    },
    // Failure surfaces inline under the password field, not a toast.
  })

  function copyRecoveryCodes() {
    if (!recoveryCodes) return
    navigator.clipboard.writeText(recoveryCodes.join("\n")).then(() => toast.success("Copied to clipboard")).catch(() => toast.error("Could not copy to clipboard"))
  }

  return (
    // No max-w cap here — matches SettingsPage, whose near-identical cards
    // (label + description + switch/button row) use the full width the
    // layout already gives them. The previous max-w-2xl (672px) squeezed
    // those same rows into a narrow column, wrapping text and crowding
    // controls together despite plenty of unused space beside it.
    <div className="space-y-4">
      <PageHeader
        title="Profile & Security"
        description={user ? `${user.username} · ${user.email}` : undefined}
        icon={UserRound}
      />

      <AppearanceCard />
      <PersonalPreferencesCard />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4" /> Two-factor authentication
          </CardTitle>
          <CardDescription>Require a code from an authenticator app in addition to your password.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {recoveryCodes ? (
            <div className="space-y-3">
              <div className="flex items-center gap-2 rounded-md border border-[var(--status-warn)] bg-[color-mix(in_oklab,var(--status-warn)_10%,transparent)] px-3 py-2 text-sm">
                <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--status-warn)]" />
                Save these recovery codes now — each can be used once if you lose access to your authenticator, and they won't be shown again.
              </div>
              <div className="grid grid-cols-2 gap-2 rounded-md bg-[var(--bg-muted)] p-3 font-mono text-sm">
                {recoveryCodes.map((c) => <span key={c}>{c}</span>)}
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="secondary" onClick={copyRecoveryCodes}>
                  <Copy className="h-3.5 w-3.5" /> Copy codes
                </Button>
                <Button size="sm" onClick={() => setRecoveryCodes(null)}>
                  <Check className="h-3.5 w-3.5" /> Done
                </Button>
              </div>
            </div>
          ) : enrollment ? (
            <div className="space-y-3">
              <p className="text-sm text-[var(--text-muted)]">Scan this QR code with your authenticator app, then enter the 6-digit code it generates.</p>
              {/* bg-white is deliberate, not a missed theme token: the PNG
                  has no transparency, and a QR scanner needs real light
                  quiet-zone contrast — var(--bg-surface) would break
                  scannability in dark mode. */}
              <img
                src={`data:image/png;base64,${enrollment.qrCodePng}`}
                alt="TOTP QR code"
                className="mx-auto h-48 w-48 rounded-md border border-[var(--border)] bg-white p-2"
              />
              <p className="text-center font-mono text-xs text-[var(--text-muted)]">{enrollment.secret}</p>
              <FormError
                message={
                  confirmMutation.error instanceof ApiError
                    ? confirmMutation.error.message
                    : confirmMutation.error
                      ? "That code didn't work — check your authenticator and try again."
                      : null
                }
              />
              <div className="flex items-end gap-2">
                <div className="flex-1 space-y-1.5">
                  <Label htmlFor="confirm-code">Verification code</Label>
                  <Input
                    id="confirm-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    placeholder="123456"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    aria-invalid={!!confirmMutation.error}
                    autoFocus
                  />
                </div>
                <Button loading={confirmMutation.isPending} disabled={!code} onClick={() => confirmMutation.mutate()}>
                  Confirm
                </Button>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setEnrollment(null)}>Cancel</Button>
            </div>
          ) : (
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {statusQuery.isLoading ? (
                  <Skeleton className="h-5 w-20" />
                ) : statusQuery.isError ? (
                  <span className="flex items-center gap-2">
                    <Badge variant="warn">Status unknown</Badge>
                    <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => void statusQuery.refetch()}>
                      Retry
                    </Button>
                  </span>
                ) : (
                  <Badge variant={statusQuery.data?.enabled ? "ok" : "default"}>{statusQuery.data?.enabled ? "Enabled" : "Disabled"}</Badge>
                )}
                {statusQuery.data?.enabled && (
                  <span className="text-xs text-[var(--text-muted)] tabular">
                    {statusQuery.data.remainingRecoveryCodes} recovery codes remaining
                  </span>
                )}
              </div>
              {statusQuery.data?.enabled ? (
                <Button size="sm" variant="destructive" onClick={() => setShowDisable((s) => !s)}>
                  <ShieldOff className="h-3.5 w-3.5" /> Disable
                </Button>
              ) : (
                <Button size="sm" loading={enrollMutation.isPending} onClick={() => enrollMutation.mutate()}>
                  {!enrollMutation.isPending && <KeyRound className="h-3.5 w-3.5" />} Enable 2FA
                </Button>
              )}
            </div>
          )}

          {showDisable && (
            <div className="space-y-2 border-t border-[var(--border)] pt-4">
              <FormError
                message={
                  disableMutation.error instanceof ApiError
                    ? disableMutation.error.message
                    : disableMutation.error
                      ? "Couldn't disable two-factor authentication — try again."
                      : null
                }
              />
              <div className="flex items-end gap-2">
                <div className="flex-1 space-y-1.5">
                  <Label htmlFor="disable-password">Confirm your password to disable 2FA</Label>
                  <Input
                    id="disable-password"
                    type="password"
                    autoComplete="current-password"
                    value={disablePassword}
                    onChange={(e) => setDisablePassword(e.target.value)}
                  />
                </div>
                <Button variant="destructive" disabled={!disablePassword || disableMutation.isPending} loading={disableMutation.isPending} onClick={() => disableMutation.mutate()}>
                  Disable 2FA
                </Button>
                <Button variant="ghost" onClick={() => setShowDisable(false)}>
                  Cancel
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <SessionsCard />
      <ApiKeysCard />
      <McpIntegrationCard />
    </div>
  )
}
