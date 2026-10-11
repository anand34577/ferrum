import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { LogOut, Monitor, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError, type Session } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"

/**
 * Every device currently signed in to this account — server-side sessions
 * backed by the `sessions` table (internal/auth), not stateless tokens, so
 * revoking one here takes effect immediately for that device's next
 * request. Mirrors ApiKeysCard's shape; see internal/api/sessions.go for
 * the endpoints.
 */
export function SessionsCard() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()

  const query = useQuery({
    queryKey: ["auth", "sessions"],
    queryFn: () => api.get<Session[]>("/profile/sessions/"),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => api.delete(`/profile/sessions/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["auth", "sessions"] })
      toast.success("Session revoked")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to revoke session"),
  })

  const revokeOthers = useMutation({
    mutationFn: () => api.delete<{ revoked: number }>("/profile/sessions/"),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["auth", "sessions"] })
      toast.success(data.revoked > 0 ? `Signed out of ${data.revoked} other session(s)` : "No other sessions to sign out of")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to sign out other sessions"),
  })

  async function handleRevoke(session: Session) {
    if (await confirm({
      title: "Revoke this session?",
      description: "That device will be signed out immediately.",
    })) {
      revoke.mutate(session.id)
    }
  }

  async function handleRevokeOthers() {
    if (await confirm({
      title: "Log out all other sessions?",
      description: "Every other signed-in device will be signed out. This device stays signed in.",
    })) {
      revokeOthers.mutate()
    }
  }

  const sessions = query.data ?? []
  const otherCount = sessions.filter((s) => !s.current).length

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle className="flex items-center gap-2">
            <Monitor className="h-4 w-4" /> Active sessions
          </CardTitle>
          <CardDescription>Devices currently signed in to your account.</CardDescription>
        </div>
        <Button size="sm" variant="secondary" disabled={otherCount === 0 || revokeOthers.isPending} onClick={handleRevokeOthers}>
          <LogOut className="h-3.5 w-3.5" /> Log out other sessions
        </Button>
      </CardHeader>
      <CardContent>
        {query.isError ? (
          <ErrorState title="Couldn't load sessions" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <div className="space-y-2" aria-busy>
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-11 w-full" />
          </div>
        ) : sessions.length === 0 ? (
          <p className="text-sm text-[var(--text-muted)]">No active sessions.</p>
        ) : (
          <div className="space-y-2">
            {sessions.map((session) => (
              <div key={session.id} className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] px-3 py-2.5">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate text-sm font-medium">{session.userAgent || "Unknown device"}</span>
                    {session.current && <Badge variant="ok">This device</Badge>}
                    {session.ip && <code className="rounded-sm bg-[var(--bg-muted)] px-1.5 py-0.5 text-xs text-[var(--text-muted)]">{session.ip}</code>}
                  </div>
                  <p className="mt-0.5 text-xs text-[var(--text-muted)]">
                    Signed in {formatRelativeTime(session.createdAt)} · Last active {session.lastSeenAt ? formatRelativeTime(session.lastSeenAt) : "unknown"}
                  </p>
                </div>
                {/* No revoke control on the current session — the badge says
                    why there's nothing to click, instead of a disabled trash
                    icon that reads as breakage. */}
                {!session.current && (
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    disabled={revoke.isPending}
                    className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
                    onClick={() => handleRevoke(session)}
                    aria-label="Revoke session"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
