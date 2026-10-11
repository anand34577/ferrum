import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { toast } from "sonner"
import { useFormDirty } from "@/components/settings/use-form-dirty"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError } from "@/lib/api"

interface SecuritySettings {
  sessionTtlHours: number
  loginMaxFailures: number
  loginLockoutMinutes: number
  require2faAdmins: boolean
}

/**
 * Session and login-lockout policy — admin-only, applied to the running
 * server immediately on save (no restart). Sessions already issued keep
 * whatever TTL was in effect when they were created.
 */
export function SecuritySettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "security"],
    queryFn: () => api.get<SecuritySettings>("/admin/settings/security"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Security</CardTitle>
        <CardDescription>Session lifetime, login lockout policy, and org-wide 2FA requirements.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load security settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        ) : (
          <SecuritySettingsForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function SecuritySettingsForm({ initial }: { initial: SecuritySettings }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState(initial)
  // Remounted via key={JSON.stringify(query.data)} on save, so dirty resets
  // for free; `initial` is the form's own starting state.
  const dirty = useFormDirty(form, initial)

  const save = useMutation({
    mutationFn: () => api.put<SecuritySettings>("/admin/settings/security", form),
    onSuccess: (data) => {
      toast.success("Security settings saved")
      queryClient.setQueryData(["admin", "settings", "security"], data)
    },
    // Failure surfaces inline via <FormError> below, not a toast — the error
    // has to outlive the toast's auto-dismiss, next to the fields to fix.
  })

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label>Session lifetime (hours)</Label>
          <Input
            type="number"
            value={form.sessionTtlHours}
            onChange={(e) => setForm({ ...form, sessionTtlHours: Number(e.target.value) || 0 })}
          />
          <p className="text-xs text-[var(--text-muted)]">720 = 30 days. Applies to sessions created after saving.</p>
        </div>
        <div className="space-y-1.5">
          <Label>Max failed logins</Label>
          <Input
            type="number"
            value={form.loginMaxFailures}
            onChange={(e) => setForm({ ...form, loginMaxFailures: Number(e.target.value) || 0 })}
          />
        </div>
        <div className="space-y-1.5">
          <Label>Lockout window (minutes)</Label>
          <Input
            type="number"
            value={form.loginLockoutMinutes}
            onChange={(e) => setForm({ ...form, loginLockoutMinutes: Number(e.target.value) || 0 })}
          />
        </div>
      </div>

      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Require 2FA for admins</p>
          <p className="text-xs text-[var(--text-muted)]">
            Admin accounts without two-factor authentication are blocked from everything except enrolling, on their next request.
          </p>
        </div>
        <Switch aria-label="Require 2FA for admins" checked={form.require2faAdmins} onCheckedChange={(v) => setForm({ ...form, require2faAdmins: v })} />
      </div>

      <FormError
        message={save.error instanceof ApiError ? save.error.message : save.error ? "Couldn't save security settings — try again." : null}
      />

      <Button size="sm" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
        Save security settings
      </Button>
    </>
  )
}
