import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, ChevronDown, Copy, Plus, Send, Trash2, Webhook as WebhookIcon, XCircle } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError } from "@/lib/api"

// Mirrors internal/api/webhooks.go's webhookDTO / webhookDeliveryDTO and the
// knownEventTypes catalog.
interface Webhook {
  id: string
  name: string
  url: string
  secret?: string
  eventTypes: string[]
  active: boolean
  createdAt: string
  updatedAt: string
}
interface WebhookDelivery {
  id: string
  eventType: string
  eventId: string
  attempt: number
  statusCode?: number
  error?: string
  success: boolean
  createdAt: string
}

const EVENT_TYPES = [
  { value: "alert.triggered", label: "Alert triggered" },
  { value: "alert.resolved", label: "Alert resolved" },
  { value: "connection.up", label: "Connection reachable" },
  { value: "connection.down", label: "Connection unreachable" },
  { value: "guest.power_changed", label: "Guest power state changed" },
  { value: "task.completed", label: "Task completed" },
  { value: "ha.status_changed", label: "HA status changed" },
]

interface FormState {
  name: string
  url: string
  eventTypes: string[]
  active: boolean
}
const emptyForm: FormState = { name: "", url: "", eventTypes: EVENT_TYPES.map((t) => t.value), active: true }

export function WebhooksPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [formError, setFormError] = useState<string | null>(null)
  const [revealedSecret, setRevealedSecret] = useState<{ name: string; secret: string } | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const webhooksQuery = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api.get<Webhook[]>("/settings/webhooks/"),
  })

  const createMutation = useMutation({
    mutationFn: (body: FormState) => api.post<Webhook>("/settings/webhooks/", body),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ["webhooks"] })
      setDialogOpen(false)
      setForm(emptyForm)
      setFormError(null)
      if (created.secret) setRevealedSecret({ name: created.name, secret: created.secret })
    },
    onError: (err) => setFormError(err instanceof ApiError ? err.message : "Failed to create webhook"),
  })

  const toggleActiveMutation = useMutation({
    mutationFn: (hook: Webhook) => api.put<Webhook>(`/settings/webhooks/${hook.id}/`, { ...hook, active: !hook.active }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update webhook"),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/settings/webhooks/${id}/`),
    onSuccess: () => {
      toast.success("Webhook deleted")
      queryClient.invalidateQueries({ queryKey: ["webhooks"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete webhook"),
  })

  const testMutation = useMutation({
    mutationFn: (id: string) => api.post<{ delivered: boolean; error?: string }>(`/settings/webhooks/${id}/test`, {}),
    onSuccess: (res) => (res.delivered ? toast.success("Test event delivered") : toast.error(res.error ?? "Delivery failed")),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to send test event"),
  })

  function toggleEventType(value: string) {
    setForm((f) => ({
      ...f,
      eventTypes: f.eventTypes.includes(value) ? f.eventTypes.filter((t) => t !== value) : [...f.eventTypes, value],
    }))
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="Webhooks"
        description="Fan events (alerts, connection status, guest lifecycle) out to any HTTP endpoint — Slack, PagerDuty, n8n, or your own automation, signed with HMAC-SHA256 so receivers can verify authenticity."
        actions={
          <Button onClick={() => setDialogOpen(true)}>
            <Plus className="h-4 w-4" /> Add webhook
          </Button>
        }
      />

      {webhooksQuery.isLoading && (
        <div className="space-y-2">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      )}
      {webhooksQuery.isError && <ErrorState message="Failed to load webhooks" onRetry={() => webhooksQuery.refetch()} />}
      {webhooksQuery.data && webhooksQuery.data.length === 0 && (
        <EmptyState
          icon={WebhookIcon}
          title="No webhooks configured"
          description="Add one to get notified the moment an alert fires, a connection drops, or a guest changes power state."
        />
      )}

      <div className="space-y-2">
        {webhooksQuery.data?.map((hook) => (
          <WebhookRow
            key={hook.id}
            hook={hook}
            expanded={expandedId === hook.id}
            onToggleExpand={() => setExpandedId((id) => (id === hook.id ? null : hook.id))}
            onToggleActive={() => toggleActiveMutation.mutate(hook)}
            onTest={() => testMutation.mutate(hook.id)}
            onDelete={async () => {
              if (await confirm({ title: "Delete webhook?", description: `"${hook.name}" will stop receiving events immediately.`, destructive: true })) {
                deleteMutation.mutate(hook.id)
              }
            }}
            testPending={testMutation.isPending}
          />
        ))}
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add webhook</DialogTitle>
            <DialogDescription>The signing secret is shown once, right after creation — store it somewhere safe.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="wh-name">Name</Label>
              <Input id="wh-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Slack alerts channel" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="wh-url">Endpoint URL</Label>
              <Input id="wh-url" value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} placeholder="https://example.com/webhooks/ferrum" />
            </div>
            <div className="space-y-1.5">
              <Label>Event types</Label>
              <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                {EVENT_TYPES.map((t) => (
                  <label key={t.value} className="flex items-center gap-2 text-sm">
                    <Checkbox checked={form.eventTypes.includes(t.value)} onCheckedChange={() => toggleEventType(t.value)} />
                    {t.label}
                  </label>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={form.active} onCheckedChange={(active) => setForm({ ...form, active })} />
              <Label>Active</Label>
            </div>
            <FormError message={formError} />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              disabled={!form.name || !form.url || createMutation.isPending}
              onClick={() => createMutation.mutate(form)}
            >
              Create webhook
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!revealedSecret} onOpenChange={(open) => !open && setRevealedSecret(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>"{revealedSecret?.name}" created</DialogTitle>
            <DialogDescription>
              This signing secret won't be shown again. Use it to verify the <code className="font-mono text-xs">X-Ferrum-Signature</code> header (hex
              HMAC-SHA256 of the raw request body).
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-muted)] px-3 py-2">
            <code className="min-w-0 flex-1 truncate font-mono text-xs">{revealedSecret?.secret}</code>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                if (revealedSecret?.secret) navigator.clipboard.writeText(revealedSecret.secret)
                toast.success("Copied to clipboard")
              }}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
          <DialogFooter>
            <Button onClick={() => setRevealedSecret(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function WebhookRow({
  hook,
  expanded,
  onToggleExpand,
  onToggleActive,
  onTest,
  onDelete,
  testPending,
}: {
  hook: Webhook
  expanded: boolean
  onToggleExpand: () => void
  onToggleActive: () => void
  onTest: () => void
  onDelete: () => void
  testPending: boolean
}) {
  const deliveriesQuery = useQuery({
    queryKey: ["webhook-deliveries", hook.id],
    queryFn: () => api.get<WebhookDelivery[]>(`/settings/webhooks/${hook.id}/deliveries`),
    enabled: expanded,
  })

  return (
    <Card>
      <CardContent className="p-3">
        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={onToggleExpand}
            className="flex min-w-0 flex-1 items-center gap-2 text-left"
            aria-expanded={expanded}
            aria-label={`${expanded ? "Collapse" : "Expand"} delivery log for ${hook.name}`}
          >
            <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-[var(--text-faint)] transition-transform ${expanded ? "rotate-180" : ""}`} />
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{hook.name}</p>
              <p className="truncate font-mono text-xs text-[var(--text-muted)]">{hook.url}</p>
            </div>
          </button>
          <div className="hidden flex-wrap gap-1 md:flex">
            {hook.eventTypes.slice(0, 3).map((t) => (
              <Badge key={t} variant="outline" className="font-mono text-[10px]">
                {t}
              </Badge>
            ))}
            {hook.eventTypes.length > 3 && <Badge variant="outline" className="text-[10px]">+{hook.eventTypes.length - 3}</Badge>}
          </div>
          <Badge variant={hook.active ? "default" : "outline"}>{hook.active ? "Active" : "Paused"}</Badge>
          <Switch checked={hook.active} onCheckedChange={onToggleActive} aria-label={hook.active ? "Pause webhook" : "Activate webhook"} />
          <Button size="sm" variant="ghost" onClick={onTest} disabled={testPending}>
            <Send className="h-3.5 w-3.5" /> Test
          </Button>
          <Button size="sm" variant="ghost" onClick={onDelete} aria-label={`Delete ${hook.name}`}>
            <Trash2 className="h-3.5 w-3.5 text-[var(--status-error)]" />
          </Button>
        </div>

        {expanded && (
          <div className="mt-3 border-t border-[var(--border)] pt-3">
            {deliveriesQuery.isLoading && <Skeleton className="h-10 w-full" />}
            {deliveriesQuery.data && deliveriesQuery.data.length === 0 && (
              <p className="text-xs text-[var(--text-muted)]">No deliveries yet.</p>
            )}
            <ul className="space-y-1.5">
              {deliveriesQuery.data?.slice(0, 20).map((d) => (
                <li key={d.id} className="flex items-center gap-2 text-xs">
                  {d.success ? (
                    <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-[var(--status-ok)]" />
                  ) : (
                    <XCircle className="h-3.5 w-3.5 shrink-0 text-[var(--status-error)]" />
                  )}
                  <span className="font-mono text-[var(--text-muted)]">{d.eventType}</span>
                  <span className="text-[var(--text-faint)]">attempt {d.attempt}</span>
                  {d.statusCode !== undefined && <span className="text-[var(--text-faint)]">HTTP {d.statusCode}</span>}
                  {d.error && <span className="min-w-0 flex-1 truncate text-[var(--status-error)]">{d.error}</span>}
                  <span className="ml-auto shrink-0 text-[var(--text-faint)]">{new Date(d.createdAt).toLocaleString()}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
