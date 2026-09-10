import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell, Loader2 } from "lucide-react"
import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api, type AlertInstance } from "@/lib/api"
import { useEventStream } from "@/lib/useEventStream"
import { cn, formatAlertValue } from "@/lib/utils"

const SEVERITY_DOT: Record<string, string> = { critical: "bg-[var(--status-error)]", warning: "bg-[var(--status-warn)]" }

/** Header notification bell — the only place an active alert is visible
 * outside the Alerts page itself. The 30s poll is the fallback; the SSE
 * subscription below invalidates both queries the moment the poller
 * actually detects a change, so in practice the badge updates live instead
 * of waiting out the interval. */
export function NotificationBell() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)

  const summaryQuery = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.get<{ warning: number; critical: number }>("/alerts/summary"),
    refetchInterval: 30_000,
  })
  const total = (summaryQuery.data?.critical ?? 0) + (summaryQuery.data?.warning ?? 0)

  const activeAlertsQuery = useQuery({
    queryKey: ["alerts", "active"],
    queryFn: () => api.get<AlertInstance[]>("/alerts/?status=active"),
    enabled: open,
    staleTime: 10_000,
  })

  // Live push: a triggered/resolved alert invalidates both queries
  // immediately instead of waiting up to 30s for the next poll. A brand new
  // critical alert also gets a toast, since it's the one case worth
  // interrupting the operator for even if the bell is closed.
  useEventStream({
    types: ["alert.triggered", "alert.resolved"],
    maxBuffered: 0,
    onEvent: (evt) => {
      queryClient.invalidateQueries({ queryKey: ["alerts-summary"] })
      queryClient.invalidateQueries({ queryKey: ["alerts", "active"] })
      if (evt.type === "alert.triggered") {
        const payload = evt.payload as { severity?: string; resourceName?: string } | undefined
        if (payload?.severity === "critical") {
          toast.error(payload.resourceName ? `Critical alert: ${payload.resourceName}` : "New critical alert", {
            action: { label: "View", onClick: () => navigate("/alerts") },
          })
        }
      }
    },
  })

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        className="relative flex h-9 w-9 items-center justify-center rounded-md text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)]"
        aria-label={total > 0 ? `${total} active alert${total === 1 ? "" : "s"}` : "Notifications — nothing active"}
      >
        <Bell className="h-4 w-4" aria-hidden />
        {total > 0 && (
          <span
            className={cn(
              "absolute right-1 top-1 flex h-3.5 min-w-3.5 items-center justify-center rounded-sm px-0.5 text-[9px] font-bold leading-none text-white",
              summaryQuery.data?.critical ? "bg-[var(--status-error)]" : "bg-[var(--status-warn)]",
            )}
            aria-hidden
          >
            {total > 9 ? "9+" : total}
          </span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuLabel>Active alerts</DropdownMenuLabel>
        {activeAlertsQuery.isLoading && (
          <div className="flex justify-center px-2.5 py-4">
            <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
          </div>
        )}
        {!activeAlertsQuery.isLoading && (activeAlertsQuery.data?.length ?? 0) === 0 && (
          <div className="px-2.5 py-4 text-center text-xs text-[var(--text-muted)]">Nothing needs attention.</div>
        )}
        {activeAlertsQuery.data?.slice(0, 8).map((a) => (
          <DropdownMenuItem key={a.id} onSelect={() => navigate("/alerts")} className="items-start gap-2.5">
            <span className={cn("mt-1 h-1.5 w-1.5 shrink-0 rounded-full", SEVERITY_DOT[a.severity] ?? "bg-[var(--text-faint)]")} aria-hidden />
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{a.resourceName}</p>
              <p className="truncate text-xs text-[var(--text-muted)]">
                {a.connectionName} · {formatAlertValue(a.metric, a.value, a.threshold)}
              </p>
            </div>
          </DropdownMenuItem>
        ))}
        {activeAlertsQuery.data && activeAlertsQuery.data.length > 8 && (
          <p className="px-2.5 pb-1 text-center text-[10px] text-[var(--text-faint)]">+{activeAlertsQuery.data.length - 8} more</p>
        )}
        {(activeAlertsQuery.data?.length ?? 0) > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => navigate("/alerts")} className="justify-center text-xs font-medium text-brand-600 dark:text-brand-400">
              View all alerts
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
