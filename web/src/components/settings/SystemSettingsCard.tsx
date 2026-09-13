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
import { api, ApiError } from "@/lib/api"

interface SystemSettings {
  alertPollSeconds: number
  corsAllowedOrigins: string
}

/**
 * General operational knobs that used to be Go constants (a rebuild away
 * from changing) — starts with the alert-rule poll interval. Applied to the
 * running server immediately on save, no restart needed.
 */
export function SystemSettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "system"],
    queryFn: () => api.get<SystemSettings>("/admin/settings/system"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>System</CardTitle>
        <CardDescription>Background job timing — applied live, no restart required.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load system settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <Skeleton className="h-9 w-full max-w-xs" />
        ) : (
          <SystemSettingsForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function SystemSettingsForm({ initial }: { initial: SystemSettings }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState(initial)
  // Remounted via key={JSON.stringify(query.data)} on save, so dirty resets
  // for free; `initial` is the form's own starting state.
  const dirty = useFormDirty(form, initial)

  const save = useMutation({
    mutationFn: () => api.put<SystemSettings>("/admin/settings/system", form),
    onSuccess: (data) => {
      toast.success("System settings saved")
      queryClient.setQueryData(["admin", "settings", "system"], data)
    },
    // Failure surfaces inline via <FormError> below, not a toast.
  })

  return (
    <>
      <div className="max-w-xs space-y-1.5">
        <Label>Alert evaluation interval (seconds)</Label>
        <Input
          type="number"
          min={10}
          max={3600}
          value={form.alertPollSeconds}
          onChange={(e) => setForm({ ...form, alertPollSeconds: Number(e.target.value) || 0 })}
        />
        <p className="text-xs text-[var(--text-muted)]">
          How often threshold alert rules are re-checked against every connection. Lower values catch problems sooner at the cost of more load on your Proxmox hosts.
        </p>
      </div>
      <div className="max-w-md space-y-1.5">
        <Label>CORS allowed origins</Label>
        <Input
          value={form.corsAllowedOrigins}
          onChange={(e) => setForm({ ...form, corsAllowedOrigins: e.target.value })}
          placeholder="https://app.example.com, https://another-app.example.com"
        />
        <p className="text-xs text-[var(--text-muted)]">
          Comma-separated origins allowed to call the REST API directly from a browser (e.g. a 3rd-party web app using an API key). Leave blank (the default) to
          keep the API same-origin only — this has no effect on Ferrum's own web app, which always works.
        </p>
      </div>
      <FormError
        message={save.error instanceof ApiError ? save.error.message : save.error ? "Couldn't save system settings — try again." : null}
      />
      <Button size="sm" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
        Save system settings
      </Button>
    </>
  )
}
