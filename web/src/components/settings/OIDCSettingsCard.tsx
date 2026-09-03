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

interface OIDCSettings {
  enabled: boolean
  displayName: string
  issuerUrl: string
  clientId: string
  redirectUrl: string
  allowAutoProvision: boolean
  singleLogout: boolean
  postLogoutRedirectUrl?: string
  hasSecret: boolean
}

/**
 * Single sign-on configuration — admin-only, saved straight to the database
 * (PUT /admin/settings/oidc). No restart required: the server swaps in a
 * live OIDC client the moment this saves successfully.
 */
export function OIDCSettingsCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "oidc"],
    queryFn: () => api.get<OIDCSettings>("/admin/settings/oidc"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Single sign-on (OIDC)</CardTitle>
        <CardDescription>
          Let users sign in via an external identity provider (Keycloak, Authentik, Entra ID, Okta, ...). Local
          username/password login keeps working regardless.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load SSO settings" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          // Keyed on the loaded data so a save (which returns a fresh
          // OIDCSettings) remounts the form with the server's version as the
          // new baseline, instead of a useEffect syncing state after render.
          <OIDCSettingsForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function OIDCSettingsForm({ initial }: { initial: OIDCSettings }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState({ ...initial, clientSecret: "" })

  const save = useMutation({
    mutationFn: () =>
      api.put<OIDCSettings>("/admin/settings/oidc", {
        enabled: form.enabled,
        displayName: form.displayName,
        issuerUrl: form.issuerUrl,
        clientId: form.clientId,
        redirectUrl: form.redirectUrl,
        allowAutoProvision: form.allowAutoProvision,
        singleLogout: form.singleLogout,
        // Omit entirely when blank — the server keeps the stored secret.
        ...(form.clientSecret ? { clientSecret: form.clientSecret } : {}),
      }),
    onSuccess: (data) => {
      toast.success("SSO settings saved")
      queryClient.setQueryData(["admin", "settings", "oidc"], data)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save SSO settings"),
  })

  return (
    <>
      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Enable SSO</p>
          <p className="text-xs text-[var(--text-muted)]">Shows a "Continue with ..." button on the login page.</p>
        </div>
        <Switch checked={form.enabled} onCheckedChange={(v) => setForm({ ...form, enabled: v })} />
      </div>

      <div className="flex items-center justify-between rounded-md border border-[var(--border)] px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">Auto-create new accounts</p>
          <p className="text-xs text-[var(--text-muted)]">
            When off, a first-time SSO login is refused unless a local account with a matching (verified) email already exists —
            useful when accounts should only be provisioned by an admin.
          </p>
        </div>
        <Switch checked={form.allowAutoProvision} onCheckedChange={(v) => setForm({ ...form, allowAutoProvision: v })} />
      </div>

      <div className="rounded-md border border-[var(--border)] px-3 py-2.5">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium">Also sign out at the identity provider (Single Logout)</p>
            <p className="text-xs text-[var(--text-muted)]">
              Requires registering the URL below with your provider first — turning this on before doing so breaks sign-out
              (the provider rejects the redirect with "invalid_redirect_uri").
            </p>
          </div>
          <Switch checked={form.singleLogout} onCheckedChange={(v) => setForm({ ...form, singleLogout: v })} />
        </div>
        {form.postLogoutRedirectUrl && (
          <div className="mt-2.5 flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-sm border border-[var(--border)] bg-[var(--bg-muted)] px-2 py-1 text-[11px]">
              {form.postLogoutRedirectUrl}
            </code>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => navigator.clipboard.writeText(form.postLogoutRedirectUrl!).then(() => toast.success("Copied"))}
            >
              Copy
            </Button>
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label>Display name</Label>
          <Input value={form.displayName} onChange={(e) => setForm({ ...form, displayName: e.target.value })} placeholder="Keycloak" />
        </div>
        <div className="space-y-1.5">
          <Label>Issuer URL</Label>
          <Input
            value={form.issuerUrl}
            onChange={(e) => setForm({ ...form, issuerUrl: e.target.value })}
            placeholder="https://idp.example.com/realms/myrealm"
          />
        </div>
        <div className="space-y-1.5">
          <Label>Client ID</Label>
          <Input value={form.clientId} onChange={(e) => setForm({ ...form, clientId: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label>Client secret</Label>
          <Input
            type="password"
            value={form.clientSecret}
            onChange={(e) => setForm({ ...form, clientSecret: e.target.value })}
            placeholder={form.hasSecret ? "•••••••• (unchanged — leave blank to keep it)" : "Client secret"}
          />
        </div>
        <div className="space-y-1.5 sm:col-span-2">
          <Label>Redirect URL</Label>
          <Input
            value={form.redirectUrl}
            onChange={(e) => setForm({ ...form, redirectUrl: e.target.value })}
            placeholder="https://ferrum.example.com/api/v1/auth/oidc/callback"
          />
          <p className="text-xs text-[var(--text-muted)]">Register this exact URL as a redirect/callback URI with your provider.</p>
        </div>
      </div>

      <Button size="sm" loading={save.isPending} onClick={() => save.mutate()}>
        Save SSO settings
      </Button>
    </>
  )
}
