import { useState } from "react"
import { AuthLayout } from "@/components/layout/AuthLayout"
import { Button } from "@/components/ui/button"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

export function SetupPage() {
  const { refresh } = useAuth()
  const [form, setForm] = useState({ username: "", email: "", password: "" })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await api.post("/auth/setup", form)
      refresh()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Setup failed — the server didn't accept the account.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthLayout
      title="Welcome to Ferrum"
      subtitle="Create the first admin account to get started."
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <FormError message={error} />
        <div className="space-y-1.5">
          <Label htmlFor="username">Username</Label>
          <Input
            id="username"
            required
            autoFocus
            autoComplete="username"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            required
            autoComplete="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            aria-describedby="setup-password-hint"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <p id="setup-password-hint" className="text-xs text-[var(--text-faint)]">Minimum 8 characters.</p>
        </div>
        <Button type="submit" className="w-full" loading={submitting}>
          Create admin account
        </Button>
      </form>
    </AuthLayout>
  )
}
