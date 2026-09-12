import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError } from "@/lib/api"

interface NotificationSettings {
  gotifyEnabled: boolean
  gotifyUrl: string
  hasGotifyToken: boolean
  smtpEnabled: boolean
  smtpHost: string
  smtpPort: number
  smtpUsername: string
  hasSmtpPassword: boolean
  smtpFrom: string
  smtpTo: string
  smtpUseTls: boolean
}

/**
 * Outbound alert notifications — Gotify and SMTP, each independently
 * optional. Fires when a new alert instance triggers (internal/poller); see
 * internal/notify. Saved to the database, applied to the live evaluator
 * immediately — no restart required.
 */
export function NotificationsSettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "notifications"],
    queryFn: () => api.get<NotificationSettings>("/admin/settings/notifications"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Notifications</CardTitle>
        <CardDescription>
          Sends a message whenever an alert rule first triggers. Gotify and email are each optional and independent —
          enable either, both, or neither.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {query.isError ? (
          <ErrorState title="Couldn't load notification settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          // Keyed on the loaded data so a save remounts the form with the
          // server's version as the new baseline, instead of a useEffect
          // syncing state after render.
          <NotificationsForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function NotificationsForm({ initial }: { initial: NotificationSettings }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState({ ...initial, gotifyToken: "", smtpPassword: "" })

  const save = useMutation({
    mutationFn: () =>
      api.put<NotificationSettings>("/admin/settings/notifications", {
        gotifyEnabled: form.gotifyEnabled,
        gotifyUrl: form.gotifyUrl,
        ...(form.gotifyToken ? { gotifyToken: form.gotifyToken } : {}),
        smtpEnabled: form.smtpEnabled,
        smtpHost: form.smtpHost,
        smtpPort: form.smtpPort,
        smtpUsername: form.smtpUsername,
        ...(form.smtpPassword ? { smtpPassword: form.smtpPassword } : {}),
        smtpFrom: form.smtpFrom,
        smtpTo: form.smtpTo,
        smtpUseTls: form.smtpUseTls,
      }),
    onSuccess: (data) => {
      toast.success("Notification settings saved")
      queryClient.setQueryData(["admin", "settings", "notifications"], data)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save notification settings"),
  })

  const testGotify = useMutation({
    mutationFn: () =>
      api.post("/admin/settings/notifications/test", {
        channel: "gotify",
        gotifyUrl: form.gotifyUrl,
        ...(form.gotifyToken ? { gotifyToken: form.gotifyToken } : {}),
      }),
    onSuccess: () => toast.success("Test notification sent to Gotify"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Gotify test failed"),
  })

  const testSMTP = useMutation({
    mutationFn: () =>
      api.post("/admin/settings/notifications/test", {
        channel: "smtp",
        smtpHost: form.smtpHost,
        smtpPort: form.smtpPort,
        smtpUsername: form.smtpUsername,
        ...(form.smtpPassword ? { smtpPassword: form.smtpPassword } : {}),
        smtpFrom: form.smtpFrom,
        smtpTo: form.smtpTo,
        smtpUseTls: form.smtpUseTls,
      }),
    onSuccess: () => toast.success("Test email sent"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "SMTP test failed"),
  })

  return (
    <>
      <section className="space-y-3">
        <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
          <div>
            <p className="text-sm font-medium">Gotify</p>
            <p className="text-xs text-[var(--text-muted)]">Push notifications via a self-hosted Gotify server.</p>
          </div>
          <Switch checked={form.gotifyEnabled} onCheckedChange={(v) => setForm({ ...form, gotifyEnabled: v })} />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>Server URL</Label>
            <Input value={form.gotifyUrl} onChange={(e) => setForm({ ...form, gotifyUrl: e.target.value })} placeholder="https://gotify.example.com" />
          </div>
          <div className="space-y-1.5">
            <Label>Application token</Label>
            <Input
              type="password"
              value={form.gotifyToken}
              onChange={(e) => setForm({ ...form, gotifyToken: e.target.value })}
              placeholder={form.hasGotifyToken ? "•••••••• (unchanged — leave blank to keep it)" : "Application token"}
            />
          </div>
        </div>
        <Button size="sm" variant="outline" loading={testGotify.isPending} onClick={() => testGotify.mutate()} disabled={!form.gotifyUrl}>
          Send test notification
        </Button>
      </section>

      <section className="space-y-3 border-t border-[var(--border)] pt-5">
        <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
          <div>
            <p className="text-sm font-medium">Email (SMTP)</p>
            <p className="text-xs text-[var(--text-muted)]">Sends via any standard SMTP relay — port 465 uses implicit TLS automatically.</p>
          </div>
          <Switch checked={form.smtpEnabled} onCheckedChange={(v) => setForm({ ...form, smtpEnabled: v })} />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>Host</Label>
            <Input value={form.smtpHost} onChange={(e) => setForm({ ...form, smtpHost: e.target.value })} placeholder="smtp.example.com" />
          </div>
          <div className="space-y-1.5">
            <Label>Port</Label>
            <Input
              type="number"
              value={form.smtpPort}
              onChange={(e) => setForm({ ...form, smtpPort: Number(e.target.value) || 0 })}
              placeholder="587"
            />
          </div>
          <div className="space-y-1.5">
            <Label>Username</Label>
            <Input value={form.smtpUsername} onChange={(e) => setForm({ ...form, smtpUsername: e.target.value })} />
          </div>
          <div className="space-y-1.5">
            <Label>Password</Label>
            <Input
              type="password"
              value={form.smtpPassword}
              onChange={(e) => setForm({ ...form, smtpPassword: e.target.value })}
              placeholder={form.hasSmtpPassword ? "•••••••• (unchanged — leave blank to keep it)" : "Password"}
            />
          </div>
          <div className="space-y-1.5">
            <Label>From address</Label>
            <Input value={form.smtpFrom} onChange={(e) => setForm({ ...form, smtpFrom: e.target.value })} placeholder="ferrum@example.com" />
          </div>
          <div className="space-y-1.5">
            <Label>To (comma-separated)</Label>
            <Input value={form.smtpTo} onChange={(e) => setForm({ ...form, smtpTo: e.target.value })} placeholder="ops@example.com, oncall@example.com" />
          </div>
        </div>
        <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
          <p className="text-sm font-medium">Use STARTTLS</p>
          <Switch checked={form.smtpUseTls} onCheckedChange={(v) => setForm({ ...form, smtpUseTls: v })} />
        </div>
        <Button
          size="sm"
          variant="outline"
          loading={testSMTP.isPending}
          onClick={() => testSMTP.mutate()}
          disabled={!form.smtpHost || !form.smtpFrom || !form.smtpTo}
        >
          Send test email
        </Button>
      </section>

      <Button size="sm" loading={save.isPending} onClick={() => save.mutate()}>
        Save notification settings
      </Button>
    </>
  )
}
