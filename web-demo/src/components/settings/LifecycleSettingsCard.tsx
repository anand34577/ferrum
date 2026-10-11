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

// Mirrors internal/api/lifecycle.go's lifecycleSettingsResponse.
interface LifecycleSettings {
  retentionDays: number
  enforce: boolean
}

/**
 * Fleet-wide snapshot retention window (a guest can override with a
 * `retain:<N>d` tag). Always runs as a dry-run — logging what it *would*
 * delete to /lifecycle/actions — until "enforce" is explicitly turned on;
 * see internal/poller/lifecycle.go.
 */
export function LifecycleSettingsCard() {
  const query = useQuery({
    queryKey: ["settings", "lifecycle"],
    queryFn: () => api.get<LifecycleSettings>("/settings/lifecycle"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Snapshot retention</CardTitle>
        <CardDescription>
          Deletes snapshots older than this window automatically — a guest tagged <code className="font-mono text-xs">retain:14d</code> overrides it.
          Nothing is ever deleted until "Enforce" is turned on below; until then every decision is only logged.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load lifecycle settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          <LifecycleForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function LifecycleForm({ initial }: { initial: LifecycleSettings }) {
  const queryClient = useQueryClient()
  const [retentionDays, setRetentionDays] = useState(String(initial.retentionDays || 0))
  const [enforce, setEnforce] = useState(initial.enforce)

  // Remounted via key={JSON.stringify(query.data)} on save, so dirty resets
  // for free. Both sides normalized the way a save normalizes them (the
  // string field vs the number sent), so "dirty" means "saving now would
  // send something new".
  const dirty = useFormDirty(
    { retentionDays: Number(retentionDays) || 0, enforce },
    { retentionDays: initial.retentionDays || 0, enforce: initial.enforce },
  )

  const save = useMutation({
    mutationFn: () =>
      api.put<LifecycleSettings>("/settings/lifecycle", {
        retentionDays: Number(retentionDays) || 0,
        enforce,
      }),
    onSuccess: (data) => {
      toast.success("Lifecycle settings saved")
      queryClient.setQueryData(["settings", "lifecycle"], data)
    },
    // Failure surfaces inline via <FormError> below, not a toast.
  })

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label>Fleet-wide retention (days)</Label>
          <Input
            type="number"
            min={0}
            value={retentionDays}
            onChange={(e) => setRetentionDays(e.target.value)}
            placeholder="0 = no fleet-wide default"
          />
        </div>
      </div>
      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Enforce (actually delete)</p>
          <p className="text-xs text-[var(--text-muted)]">Off = dry-run only, every decision still logged to Audit Log.</p>
        </div>
        <Switch aria-label="Enforce (actually delete)" checked={enforce} onCheckedChange={setEnforce} />
      </div>
      <FormError
        message={save.error instanceof ApiError ? save.error.message : save.error ? "Couldn't save lifecycle settings — try again." : null}
      />
      <Button size="sm" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
        Save
      </Button>
    </>
  )
}
