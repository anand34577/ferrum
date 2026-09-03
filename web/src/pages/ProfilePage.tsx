import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Check, Copy, KeyRound, ShieldCheck, ShieldOff, UserRound } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { AppearanceCard } from "@/components/settings/AppearanceCard"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

interface TotpStatus {
  enabled: boolean
  remainingRecoveryCodes: number
}

interface Enrollment {
  secret: string
  otpauthUrl: string
  qrCodePng: string
}

export function ProfilePage() {
  const { user, refresh } = useAuth()
  const queryClient = useQueryClient()
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const [code, setCode] = useState("")
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)
  const [disablePassword, setDisablePassword] = useState("")
  const [showDisable, setShowDisable] = useState(false)

  const statusQuery = useQuery({ queryKey: ["totp-status"], queryFn: () => api.get<TotpStatus>("/auth/2fa/status") })

  const enrollMutation = useMutation({
    mutationFn: () => api.post<Enrollment>("/auth/2fa/enroll"),
    onSuccess: (data) => setEnrollment(data),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to start enrollment"),
  })

  const confirmMutation = useMutation({
    mutationFn: () => api.post<{ recoveryCodes: string[] }>("/auth/2fa/confirm", { code }),
    onSuccess: (data) => {
      setRecoveryCodes(data.recoveryCodes)
      setEnrollment(null)
      setCode("")
      queryClient.invalidateQueries({ queryKey: ["totp-status"] })
      refresh()
      toast.success("Two-factor authentication enabled")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Invalid code"),
  })

  const disableMutation = useMutation({
    mutationFn: () => api.post("/auth/2fa/disable", { password: disablePassword }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["totp-status"] })
      refresh()
      setShowDisable(false)
      setDisablePassword("")
      toast.success("Two-factor authentication disabled")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to disable"),
  })

  function copyRecoveryCodes() {
    if (!recoveryCodes) return
    navigator.clipboard.writeText(recoveryCodes.join("\n")).then(() => toast.success("Copied to clipboard"))
  }

  return (
    <div className="max-w-2xl space-y-4">
      <PageHeader
        title="Profile & Security"
        description={user ? `${user.username} · ${user.email}` : undefined}
        icon={UserRound}
      />

      <AppearanceCard />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4" /> Two-factor authentication
          </CardTitle>
          <CardDescription>Require a code from an authenticator app in addition to your password.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {recoveryCodes ? (
            <div className="space-y-3">
              <div className="flex items-center gap-2 rounded-md border border-[var(--status-warn)] bg-[color-mix(in_oklab,var(--status-warn)_10%,transparent)] px-3 py-2 text-sm">
                <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--status-warn)]" />
                Save these recovery codes now — each can be used once if you lose access to your authenticator, and they won't be shown again.
              </div>
              <div className="grid grid-cols-2 gap-2 rounded-md bg-[var(--bg-muted)] p-3 font-mono text-sm">
                {recoveryCodes.map((c) => <span key={c}>{c}</span>)}
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="secondary" onClick={copyRecoveryCodes}>
                  <Copy className="h-3.5 w-3.5" /> Copy codes
                </Button>
                <Button size="sm" onClick={() => setRecoveryCodes(null)}>
                  <Check className="h-3.5 w-3.5" /> Done
                </Button>
              </div>
            </div>
          ) : enrollment ? (
            <div className="space-y-3">
              <p className="text-sm text-[var(--text-muted)]">Scan this QR code with your authenticator app, then enter the 6-digit code it generates.</p>
              <img
                src={`data:image/png;base64,${enrollment.qrCodePng}`}
                alt="TOTP QR code"
                className="mx-auto h-48 w-48 rounded-md border border-[var(--border)] bg-white p-2"
              />
              <p className="text-center font-mono text-xs text-[var(--text-muted)]">{enrollment.secret}</p>
              <div className="flex items-end gap-2">
                <div className="flex-1 space-y-1.5">
                  <Label htmlFor="confirm-code">Verification code</Label>
                  <Input id="confirm-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" autoFocus />
                </div>
                <Button loading={confirmMutation.isPending} disabled={!code} onClick={() => confirmMutation.mutate()}>
                  Confirm
                </Button>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setEnrollment(null)}>Cancel</Button>
            </div>
          ) : (
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {statusQuery.isLoading ? (
                  <Skeleton className="h-5 w-20" />
                ) : statusQuery.isError ? (
                  <Badge variant="warn">Status unknown</Badge>
                ) : (
                  <Badge variant={statusQuery.data?.enabled ? "ok" : "default"}>{statusQuery.data?.enabled ? "Enabled" : "Disabled"}</Badge>
                )}
                {statusQuery.data?.enabled && (
                  <span className="text-xs text-[var(--text-muted)] tabular">
                    {statusQuery.data.remainingRecoveryCodes} recovery codes remaining
                  </span>
                )}
              </div>
              {statusQuery.data?.enabled ? (
                <Button size="sm" variant="destructive" onClick={() => setShowDisable((s) => !s)}>
                  <ShieldOff className="h-3.5 w-3.5" /> Disable
                </Button>
              ) : (
                <Button size="sm" loading={enrollMutation.isPending} onClick={() => enrollMutation.mutate()}>
                  {!enrollMutation.isPending && <KeyRound className="h-3.5 w-3.5" />} Enable 2FA
                </Button>
              )}
            </div>
          )}

          {showDisable && (
            <div className="flex items-end gap-2 border-t border-[var(--border)] pt-4">
              <div className="flex-1 space-y-1.5">
                <Label htmlFor="disable-password">Confirm your password to disable 2FA</Label>
                <Input id="disable-password" type="password" value={disablePassword} onChange={(e) => setDisablePassword(e.target.value)} />
              </div>
              <Button variant="destructive" disabled={!disablePassword || disableMutation.isPending} onClick={() => disableMutation.mutate()}>
                Confirm
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
