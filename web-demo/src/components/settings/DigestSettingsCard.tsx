import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { toast } from "sonner"
import { useFormDirty } from "@/components/settings/use-form-dirty"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { FormError } from "@/components/ui/form-error"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { api, ApiError } from "@/lib/api"

// Mirrors internal/api/digest_settings.go's digestSettingsResponse.
interface DigestSettings {
  enabled: boolean
  intervalHours: number
  recipients: string[]
  lastSentAt?: string
}

const INTERVAL_OPTIONS = [
  { value: "24", label: "Daily" },
  { value: "168", label: "Weekly" },
  { value: "720", label: "Monthly" },
]

/**
 * A periodic email summary of fleet health — uptime, backup success rate,
 * active alerts, capacity trend — sent via the same SMTP config as the
 * Notifications card above (internal/digest reuses notify.Notifier, no
 * separate delivery channel).
 */
export function DigestSettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "digest"],
    queryFn: () => api.get<DigestSettings>("/admin/settings/digest"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Fleet health digest</CardTitle>
        <CardDescription>A periodic email summary of uptime, active alerts, and capacity trend across every connection.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load digest settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          <DigestForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

// The exact body a save sends — used for both the mutation and dirty
// tracking, so "dirty" always means "saving now would send something new".
function digestPayload(enabled: boolean, intervalHours: string, recipients: string) {
  return {
    enabled,
    intervalHours: Number(intervalHours),
    recipients: recipients.split(",").map((r) => r.trim()).filter(Boolean),
  }
}

function DigestForm({ initial }: { initial: DigestSettings }) {
  const queryClient = useQueryClient()
  const [enabled, setEnabled] = useState(initial.enabled)
  const [intervalHours, setIntervalHours] = useState(String(initial.intervalHours || 168))
  const [recipients, setRecipients] = useState(initial.recipients.join(", "))

  // Remounted via key={JSON.stringify(query.data)} on save, so dirty resets
  // for free. Compared against the form's own initial state pushed through
  // the same normalization — never the raw server response.
  const dirty = useFormDirty(
    digestPayload(enabled, intervalHours, recipients),
    digestPayload(initial.enabled, String(initial.intervalHours || 168), initial.recipients.join(", ")),
  )

  const save = useMutation({
    mutationFn: () =>
      api.put<DigestSettings>("/admin/settings/digest", digestPayload(enabled, intervalHours, recipients)),
    onSuccess: (data) => {
      toast.success("Digest settings saved")
      queryClient.setQueryData(["admin", "settings", "digest"], data)
    },
    // Failure surfaces inline via <FormError> below, not a toast.
  })

  const sendNow = useMutation({
    mutationFn: () => api.post("/admin/settings/digest/send-now", {}),
    onSuccess: () => toast.success("Digest sent"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to send digest"),
  })

  return (
    <>
      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Send digest</p>
          <p className="text-xs text-[var(--text-muted)]">
            {initial.lastSentAt ? `Last sent ${new Date(initial.lastSentAt).toLocaleString()}` : "Never sent yet."}
          </p>
        </div>
        <Switch aria-label="Send digest" checked={enabled} onCheckedChange={setEnabled} />
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label>Frequency</Label>
          <Select value={intervalHours} onValueChange={setIntervalHours}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {INTERVAL_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Recipients (comma-separated)</Label>
          <Textarea rows={1} value={recipients} onChange={(e) => setRecipients(e.target.value)} placeholder="ops@example.com, oncall@example.com" />
        </div>
      </div>
      <FormError
        message={save.error instanceof ApiError ? save.error.message : save.error ? "Couldn't save digest settings — try again." : null}
      />
      <div className="flex flex-wrap gap-2">
        <Button size="sm" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
          Save
        </Button>
        <Button size="sm" variant="outline" loading={sendNow.isPending} onClick={() => sendNow.mutate()} disabled={recipients.trim().length === 0}>
          Send now
        </Button>
        <p className="w-full text-xs text-[var(--text-faint)]">"Send now" delivers with the settings as last saved — save first if you just edited anything.</p>
      </div>
    </>
  )
}
