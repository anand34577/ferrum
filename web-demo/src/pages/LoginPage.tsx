import { useQuery } from "@tanstack/react-query"
import { ShieldCheck } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AuthLayout } from "@/components/layout/AuthLayout"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

export function LoginPage() {
  const { refresh } = useAuth()
  const [form, setForm] = useState({ username: "", password: "" })
  const [submitting, setSubmitting] = useState(false)
  const [pendingToken, setPendingToken] = useState<string | null>(null)
  const [code, setCode] = useState("")

  const { data: sso } = useQuery({
    queryKey: ["oidc-config"],
    queryFn: () => api.get<{ enabled: boolean; displayName?: string }>("/auth/oidc/config"),
    staleTime: 60_000,
    retry: false,
  })

  useEffect(() => {
    const ssoError = new URLSearchParams(window.location.search).get("sso_error")
    if (ssoError === "no_account") {
      toast.error("No account exists for that SSO identity, and new accounts aren't created automatically. Ask an admin to create one.")
      window.history.replaceState(null, "", window.location.pathname)
    } else if (ssoError) {
      toast.error("Single sign-on failed. Please try again or use your password.")
      window.history.replaceState(null, "", window.location.pathname)
    }
  }, [])

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      const res = await api.post<{ requiresTotp?: string; pendingToken?: string }>("/auth/login", form)
      if (res.requiresTotp && res.pendingToken) {
        setPendingToken(res.pendingToken)
      } else {
        refresh()
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Login failed")
    } finally {
      setSubmitting(false)
    }
  }

  async function onSubmitTotp(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    try {
      await api.post("/auth/login/totp", { pendingToken, code })
      refresh()
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Verification failed")
      setCode("")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthLayout title={pendingToken ? "Two-factor verification" : "Sign in to Ferrum"}>
      {!pendingToken ? (
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="username">Username</Label>
            <Input
              id="username"
              autoFocus
              required
              autoComplete="username"
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              required
              autoComplete="current-password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
            />
          </div>
          <Button type="submit" className="w-full" loading={submitting}>
            Sign in
          </Button>

          {sso?.enabled && (
            <>
              <div className="flex items-center gap-3 text-xs text-[var(--text-faint)]">
                <div className="h-px flex-1 bg-[var(--border)]" />
                or
                <div className="h-px flex-1 bg-[var(--border)]" />
              </div>
              <Button
                type="button"
                variant="secondary"
                className="w-full"
                onClick={() => {
                  window.location.href = "/api/v1/auth/oidc/login"
                }}
              >
                Continue with {sso.displayName}
              </Button>
            </>
          )}
        </form>
      ) : (
        <form onSubmit={onSubmitTotp} className="space-y-4">
          <div className="flex items-start gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-2.5 text-xs leading-relaxed text-[var(--text-muted)]">
            <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-brand-500" />
            Enter the 6-digit code from your authenticator app, or one of your recovery codes.
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="code">Authentication code</Label>
            <Input
              id="code"
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="123456"
              inputMode="numeric"
              autoComplete="one-time-code"
              className="text-center font-mono text-lg tracking-[0.4em]"
            />
          </div>
          <Button type="submit" className="w-full" loading={submitting} disabled={!code}>
            Verify
          </Button>
          <button
            type="button"
            className="w-full text-center text-xs text-[var(--text-muted)] transition-colors hover:text-[var(--text)]"
            onClick={() => {
              setPendingToken(null)
              setCode("")
            }}
          >
            Back to sign in
          </button>
        </form>
      )}
    </AuthLayout>
  )
}
