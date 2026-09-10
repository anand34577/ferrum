import { useQuery } from "@tanstack/react-query"
import { api, type FleetHealthScore, type HealthScoreResult } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection, utilizationTone } from "@/lib/fleet"
import { cn } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

function scoreTone(score: number): string {
  // utilizationTone reads "higher = worse"; a health score is the inverse.
  const tone = utilizationTone(100 - score)
  return tone === "error" ? "text-[var(--status-error)]" : tone === "warn" ? "text-[var(--status-warn)]" : "text-[var(--status-ok)]"
}

function ScoreCard({ result }: { result: HealthScoreResult }) {
  return (
    <div className="flex h-full flex-col justify-center gap-3">
      <div className="text-center">
        <p className={cn("font-display text-4xl font-semibold tabular", scoreTone(result.score))}>{result.score}</p>
        <p className="text-xs text-[var(--text-muted)]">{result.connectionName ?? "Fleet health"} · out of 100</p>
      </div>
      <div className="space-y-1.5">
        {result.components.map((c) => {
          const pct = c.max > 0 ? (c.points / c.max) * 100 : 100
          return (
            <div key={c.label} className="space-y-0.5">
              <div className="flex items-center justify-between text-[10px] text-[var(--text-faint)]">
                <span>{c.label}</span>
                <span className="tabular">{c.points}/{c.max}</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-none bg-[var(--track)]">
                <div
                  className={cn("h-full", pct >= 80 ? "bg-[var(--status-ok)]" : pct >= 40 ? "bg-[var(--status-warn)]" : "bg-[var(--status-error)]")}
                  style={{ width: `${Math.max(0, Math.min(100, pct))}%` }}
                />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

/** 0-100 health rollup combining active alerts, node/guest status, backup
 * reliability, and connection reachability — see internal/api/health.go
 * for the exact scoring formula. Scoped to one connection via the widget's
 * connection setting, or fleet-wide (average, with the worst connection
 * called out) when set to "All connections". */
export function HealthScoreWidget({ settings }: { settings: WidgetSettings }) {
  const connId = scopedConnection(settings)

  const connectionQuery = useQuery({
    queryKey: ["health-score", connId],
    queryFn: ({ signal }) => api.get<HealthScoreResult>(`/connections/${connId}/health-score`, { signal }),
    enabled: connId !== "all",
    refetchInterval: 60_000,
  })
  const fleetQuery = useQuery({
    queryKey: ["health-score", "fleet"],
    queryFn: ({ signal }) => api.get<FleetHealthScore>("/health-score", { signal }),
    enabled: connId === "all",
    refetchInterval: 60_000,
  })

  const isError = connId === "all" ? fleetQuery.isError : connectionQuery.isError
  if (isError) return <WidgetError />

  if (connId !== "all") {
    if (!connectionQuery.data) return <p className="text-sm text-[var(--text-muted)]">Loading…</p>
    return <ScoreCard result={connectionQuery.data} />
  }

  const fleet = fleetQuery.data
  if (!fleet) return <p className="text-sm text-[var(--text-muted)]">Loading…</p>
  if (fleet.connections.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No connections configured yet.</p>
  }

  return (
    <div className="flex h-full flex-col gap-2 overflow-auto">
      <div className="text-center">
        <p className={cn("font-display text-4xl font-semibold tabular", scoreTone(fleet.score))}>{fleet.score}</p>
        <p className="text-xs text-[var(--text-muted)]">Fleet average · out of 100</p>
      </div>
      {fleet.worst && fleet.worst.score < 100 && (
        <p className="text-center text-[10px] text-[var(--text-faint)]">
          Lowest: <span className="font-medium text-[var(--text)]">{fleet.worst.connectionName}</span> at {fleet.worst.score}
        </p>
      )}
      <div className="mt-1 space-y-1 border-t border-[var(--border)] pt-2">
        {fleet.connections.map((c) => (
          <div key={c.connectionId} className="flex items-center justify-between text-xs">
            <span className="truncate">{c.connectionName}</span>
            <span className={cn("tabular font-medium", scoreTone(c.score))}>{c.score}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
