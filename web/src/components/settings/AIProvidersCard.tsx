import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bot, Check, CheckCircle2, Pencil, Plug, Plus, RefreshCw, Star, Trash2, X } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, ApiError, type AIModel, type AIProvider } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"

interface FormState {
  name: string
  baseUrl: string
  apiKey: string
  isEnabled: boolean
}

const emptyForm: FormState = { name: "", baseUrl: "", apiKey: "", isEnabled: true }

const PRESETS = [
  { label: "OpenAI", baseUrl: "https://api.openai.com/v1" },
  { label: "Ollama (local)", baseUrl: "http://localhost:11434/v1" },
  { label: "LM Studio (local)", baseUrl: "http://localhost:1234/v1" },
  { label: "Needle 2 (built-in, local)", baseUrl: "needle://local" },
]

// Ferrum's sentinel base URL (internal/needle.BaseURL) for the optional,
// no-API-key, no-network built-in provider — see README "Built-in LLM
// (Needle 2)" for how an admin installs the binary this depends on.
const NEEDLE_BUILTIN_URL = "needle://local"

/**
 * Admin-managed OpenAI-chat-completions-compatible providers backing the AI
 * Assistant page (see AIAssistantPage.tsx) — OpenAI, Ollama, LM Studio,
 * LocalAI, OpenRouter, or any other server speaking the same API shape.
 * Each provider (one endpoint + credential) can expose any number of
 * models, each with a friendly label distinct from the exact model ID sent
 * to the API.
 */
export function AIProvidersCard() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [open, setOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [discoveredModels, setDiscoveredModels] = useState<string[] | null>(null)

  const query = useQuery({
    queryKey: ["admin", "settings", "ai", "providers"],
    queryFn: () => api.get<AIProvider[]>("/admin/settings/ai/providers/"),
  })
  const editingProvider = query.data?.find((p) => p.id === editingId) ?? null

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "settings", "ai", "providers"] })
    queryClient.invalidateQueries({ queryKey: ["ai", "providers"] })
  }

  function startCreate() {
    setEditingId(null)
    setForm(emptyForm)
    setDiscoveredModels(null)
    setOpen(true)
  }

  function startEdit(p: AIProvider) {
    setEditingId(p.id)
    setForm({ name: p.name, baseUrl: p.baseUrl, apiKey: "", isEnabled: p.isEnabled })
    setDiscoveredModels(null)
    setOpen(true)
  }

  // Ad-hoc discovery — works before the provider is even saved, so admins
  // don't need to already know their runtime's exact model IDs.
  const discover = useMutation({
    mutationFn: () =>
      api.post<{ ok: boolean; via: string; models?: string[] }>("/admin/settings/ai/providers/test", {
        baseUrl: form.baseUrl,
        apiKey: form.apiKey || undefined,
      }),
    onSuccess: (res) => {
      if (res.models && res.models.length > 0) {
        setDiscoveredModels(res.models)
        toast.success(`Found ${res.models.length} model${res.models.length === 1 ? "" : "s"} — click one below to add it`)
      } else {
        setDiscoveredModels([])
        toast.success(`Reachable via ${res.via} — this runtime doesn't list models, add one manually below`)
      }
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Couldn't reach provider"),
  })

  const save = useMutation({
    mutationFn: () => {
      const body = { name: form.name, baseUrl: form.baseUrl, isEnabled: form.isEnabled, ...(form.apiKey ? { apiKey: form.apiKey } : {}) }
      return editingId
        ? api.put(`/admin/settings/ai/providers/${editingId}`, body)
        : api.post<{ id: string }>("/admin/settings/ai/providers/", body)
    },
    onSuccess: (res) => {
      invalidate()
      if (editingId) {
        toast.success("Provider updated")
      } else {
        // Stay open, switch into edit mode so the admin can add models
        // immediately without a second "add provider" round trip.
        toast.success("Provider added — now add a model below")
        setEditingId((res as { id: string }).id)
      }
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save provider"),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.delete(`/admin/settings/ai/providers/${id}`),
    onSuccess: () => {
      toast.success("Provider removed")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove provider"),
  })

  const test = useMutation({
    mutationFn: (id: string) => api.post<{ ok: boolean; via: string }>(`/admin/settings/ai/providers/${id}/test`),
    onMutate: (id) => setTestingId(id),
    onSuccess: () => toast.success("Provider reachable"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Test failed"),
    onSettled: () => setTestingId(null),
  })

  async function handleRemove(p: AIProvider) {
    if (await confirm({ title: `Remove "${p.name}"?`, description: `Its ${p.models.length} model(s) will no longer be selectable in the AI Assistant.` })) {
      remove.mutate(p.id)
      if (editingId === p.id) setOpen(false)
    }
  }

  const providers = query.data ?? []

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle className="flex items-center gap-2">
            <Bot className="h-4 w-4" /> AI providers &amp; models
          </CardTitle>
          <CardDescription>OpenAI-compatible endpoints for the AI Assistant — OpenAI, Ollama, LM Studio, LocalAI, or any other 3rd-party API.</CardDescription>
        </div>
        <Button size="sm" onClick={startCreate}>
          <Plus className="h-3.5 w-3.5" /> Add provider
        </Button>
      </CardHeader>
      <CardContent>
        {query.isError ? (
          <ErrorState title="Couldn't load AI providers" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-2" aria-busy>
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : providers.length === 0 ? (
          <p className="text-sm text-[var(--text-muted)]">No AI providers configured yet — add one so users can use the AI Assistant.</p>
        ) : (
          <div className="space-y-2">
            {providers.map((p) => (
              <div key={p.id} className="rounded-md border border-[var(--border)] px-3 py-2.5">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium">{p.name}</span>
                      <Badge variant={p.isEnabled ? "ok" : "default"}>{p.isEnabled ? "Enabled" : "Disabled"}</Badge>
                    </div>
                    <p className="mt-0.5 truncate text-xs text-[var(--text-muted)]">
                      {p.baseUrl}{p.hasApiKey ? " · API key set" : ""} · updated {formatRelativeTime(p.updatedAt)}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button size="icon-sm" variant="ghost" loading={testingId === p.id} onClick={() => test.mutate(p.id)} aria-label={`Test ${p.name}`}>
                      {testingId !== p.id && <Plug className="h-3.5 w-3.5" />}
                    </Button>
                    <Button size="icon-sm" variant="ghost" onClick={() => startEdit(p)} aria-label={`Edit ${p.name}`}>
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                    <Button size="icon-sm" variant="ghost" onClick={() => handleRemove(p)} aria-label={`Remove ${p.name}`}>
                      <Trash2 className="h-3.5 w-3.5 text-[var(--status-error)]" />
                    </Button>
                  </div>
                </div>
                {p.models.length > 0 ? (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {p.models.map((m) => (
                      <span key={m.id} className="inline-flex items-center gap-1 rounded-sm border border-[var(--border)] px-2 py-0.5 text-xs text-[var(--text-muted)]">
                        {m.isDefault && <Star className="h-2.5 w-2.5 fill-current text-brand-500" />}
                        {m.label}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="mt-2 text-xs text-[var(--status-warn)]">No models added yet — click edit to add one.</p>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingId ? "Edit provider" : "Add AI provider"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            {!editingId && (
              <div className="flex flex-wrap gap-1.5">
                {PRESETS.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => setForm((f) => ({ ...f, name: f.name || preset.label, baseUrl: preset.baseUrl }))}
                    className="rounded-md border border-[var(--border)] px-2.5 py-1 text-xs text-[var(--text-muted)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text)]"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            )}
            <div className="space-y-1.5">
              <Label htmlFor="ai-name">Name</Label>
              <Input id="ai-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. OpenAI, Home Ollama" autoFocus />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ai-base-url">Base URL</Label>
              <Input
                id="ai-base-url"
                value={form.baseUrl}
                onChange={(e) => {
                  setDiscoveredModels(null)
                  setForm({ ...form, baseUrl: e.target.value })
                }}
                placeholder="https://api.openai.com/v1"
              />
              {form.baseUrl === NEEDLE_BUILTIN_URL && (
                <p className="text-xs text-[var(--text-muted)]">
                  Runs entirely on this server — no API key, no outbound network call. Requires the Needle 2 CLI binary to be installed
                  and pointed at via <code className="rounded-sm bg-[var(--bg-muted)] px-1">FERRUM_NEEDLE_BIN</code> (see README "Built-in LLM
                  (Needle 2)"); "Discover" below will fail until it's installed.
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ai-key">API key {editingId && <span className="text-[var(--text-muted)]">(leave blank to keep current)</span>}</Label>
              <Input
                id="ai-key"
                type="password"
                value={form.apiKey}
                onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
                placeholder="Not required for most local runtimes"
                disabled={form.baseUrl === NEEDLE_BUILTIN_URL}
              />
            </div>
            <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
              <div>
                <p className="text-sm font-medium">Enabled</p>
                <p className="text-xs text-[var(--text-muted)]">Visible to users in the AI Assistant picker.</p>
              </div>
              <Switch checked={form.isEnabled} onCheckedChange={(v) => setForm({ ...form, isEnabled: v })} />
            </div>

            <div className="flex items-center justify-between">
              <Label>Models</Label>
              <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-xs" disabled={!form.baseUrl.trim()} loading={discover.isPending} onClick={() => discover.mutate()}>
                {!discover.isPending && <RefreshCw className="h-3 w-3" />} Discover
              </Button>
            </div>
            {!editingId ? (
              <p className="text-xs text-[var(--text-muted)]">Save the provider first, then add its models below.</p>
            ) : (
              <ModelsEditor providerId={editingId} models={editingProvider?.models ?? []} discovered={discoveredModels} onChanged={invalidate} />
            )}
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setOpen(false)}>{editingId ? "Done" : "Cancel"}</Button>
            {!editingId && (
              <Button loading={save.isPending} disabled={!form.name.trim() || !form.baseUrl.trim()} onClick={() => save.mutate()}>
                <CheckCircle2 className="h-3.5 w-3.5" /> Save &amp; continue
              </Button>
            )}
            {editingId && (
              <Button loading={save.isPending} onClick={() => save.mutate()}>
                <CheckCircle2 className="h-3.5 w-3.5" /> Save changes
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

/** Add/edit/remove the models under one already-saved provider. */
function ModelsEditor({ providerId, models, discovered, onChanged }: { providerId: string; models: AIModel[]; discovered: string[] | null; onChanged: () => void }) {
  const confirm = useConfirm()
  const [label, setLabel] = useState("")
  const [modelId, setModelId] = useState("")

  const add = useMutation({
    mutationFn: (body: { label: string; modelId: string; isDefault?: boolean }) => api.post(`/admin/settings/ai/providers/${providerId}/models/`, body),
    onSuccess: () => {
      onChanged()
      setLabel("")
      setModelId("")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add model"),
  })

  const setDefault = useMutation({
    mutationFn: (id: string) => api.put(`/admin/settings/ai/providers/${providerId}/models/${id}`, { isDefault: true }),
    onSuccess: onChanged,
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to set default"),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.delete(`/admin/settings/ai/providers/${providerId}/models/${id}`),
    onSuccess: onChanged,
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove model"),
  })

  async function handleRemove(m: AIModel) {
    if (await confirm({ title: `Remove "${m.label}"?`, description: "Users won't be able to select this model anymore." })) {
      remove.mutate(m.id)
    }
  }

  const undiscovered = (discovered ?? []).filter((id) => !models.some((m) => m.modelId === id))

  return (
    <div className="space-y-2.5">
      {models.length > 0 && (
        <div className="space-y-1.5">
          {models.map((m) => (
            <div key={m.id} className="flex items-center justify-between gap-2 rounded-md border border-[var(--border)] px-2.5 py-1.5">
              <div className="min-w-0">
                <p className="truncate text-sm">{m.label}</p>
                <p className="truncate font-mono text-xs text-[var(--text-muted)]">{m.modelId}</p>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  disabled={m.isDefault}
                  loading={setDefault.isPending}
                  onClick={() => setDefault.mutate(m.id)}
                  aria-label={m.isDefault ? `${m.label} is the default model` : `Make ${m.label} the default model`}
                  title={m.isDefault ? "Default model" : "Make default"}
                >
                  <Star className={m.isDefault ? "h-3.5 w-3.5 fill-current text-brand-500" : "h-3.5 w-3.5"} />
                </Button>
                <Button size="icon-sm" variant="ghost" onClick={() => handleRemove(m)} aria-label={`Remove ${m.label}`}>
                  <X className="h-3.5 w-3.5 text-[var(--status-error)]" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {undiscovered.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {undiscovered.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() => add.mutate({ label: id, modelId: id, isDefault: models.length === 0 })}
              className="inline-flex items-center gap-1 rounded-md border border-dashed border-[var(--border-strong)] px-2.5 py-1 text-xs text-[var(--text-muted)] transition-colors hover:border-brand-500 hover:text-[var(--text)]"
            >
              <Plus className="h-3 w-3" /> {id}
            </button>
          ))}
        </div>
      )}

      <div className="flex items-end gap-2">
        <div className="flex-1 space-y-1">
          <Label htmlFor="model-label" className="text-xs">Display name</Label>
          <Input id="model-label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="GPT-4o mini" className="h-8 text-sm" />
        </div>
        <div className="flex-1 space-y-1">
          <Label htmlFor="model-id" className="text-xs">Model ID</Label>
          <Input id="model-id" value={modelId} onChange={(e) => setModelId(e.target.value)} placeholder="gpt-4o-mini" className="h-8 font-mono text-sm" />
        </div>
        <Button
          size="sm"
          loading={add.isPending}
          disabled={!label.trim() || !modelId.trim()}
          onClick={() => add.mutate({ label, modelId, isDefault: models.length === 0 })}
        >
          <Check className="h-3.5 w-3.5" /> Add
        </Button>
      </div>
    </div>
  )
}
