import { AlertTriangle, ChevronRight, ShieldCheck, WifiOff } from "lucide-react"
import { Link } from "react-router-dom"
import { summarizeFleet, useFleetOverview } from "@/lib/fleet"
import { cn } from "@/lib/utils"

/**
 * The master-caution bar: a persistent nameplate strip pinned above the
 * header — quiet green "ALL SYSTEMS NOMINAL" when the fleet is clean, a
 * banded amber/red strip when it isn't. Unlike a toast, it never disappears:
 * an operator glancing at any page always sees fleet-wide status first.
 * Reads fleet-wide, not per-connection — it fires on the worst state
 * anywhere in the fleet regardless of which page is open.
 */
export function MasterCautionBar() {
  const { data } = useFleetOverview()
  const totals = summarizeFleet(data)

  const critical = totals.alertsCritical
  const offline = totals.offline.length
  const warning = totals.alertsWarning
  const nominal = critical === 0 && offline === 0 && warning === 0

  const level: "ok" | "warn" | "error" = critical > 0 || offline > 0 ? "error" : warning > 0 ? "warn" : "ok"

  const parts: string[] = []
  if (critical > 0) parts.push(`${critical} CRITICAL ALERT${critical === 1 ? "" : "S"}`)
  if (offline > 0) parts.push(`${offline} CONNECTION${offline === 1 ? "" : "S"} OFFLINE`)
  if (warning > 0) parts.push(`${warning} WARNING${warning === 1 ? "" : "S"}`)
  if (nominal) parts.push("ALL SYSTEMS NOMINAL")

  return (
    <Link
      to="/alerts"
      role="status"
      className={cn(
        "group relative flex h-6 shrink-0 items-center justify-center gap-2 overflow-hidden px-4 text-center transition-colors",
        level === "error" && "caution-band bg-[var(--status-error)] text-white",
        level === "warn" && "caution-band bg-[var(--status-warn)] text-black",
        level === "ok" && "border-b border-[var(--border)] bg-[var(--bg-muted)] text-[var(--status-ok)]",
      )}
    >
      {level === "ok" ? <ShieldCheck className="h-3 w-3 shrink-0" aria-hidden /> : <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden />}
      <span className="panel-label truncate text-[10px] tracking-[0.12em]">{parts.join(" · ")}</span>
      {offline > 0 && <WifiOff className="h-3.5 w-3.5 shrink-0" aria-hidden />}
      <ChevronRight className="h-3 w-3 shrink-0 opacity-60 transition-transform group-hover:translate-x-0.5" aria-hidden />
    </Link>
  )
}
