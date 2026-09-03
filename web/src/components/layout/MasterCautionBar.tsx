import { AlertTriangle, ChevronRight, ShieldCheck, WifiOff } from "lucide-react"
import { Link } from "react-router-dom"
import { summarizeFleet, useFleetOverview } from "@/lib/fleet"
import { useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"

/**
 * The master-caution bar: an aviation-panel convention, so its "always
 * visible" nameplate behavior is specific to the Glass Flight Deck look —
 * quiet green "ALL SYSTEMS NOMINAL" when the fleet is clean, a banded
 * amber/red strip when it isn't, never disappearing. Every other look gets
 * the conventional reactive banner instead: nothing rendered when nominal,
 * a plain (unbanded) alert strip when something needs attention. Reads
 * fleet-wide, not per-connection — it fires on the worst state anywhere in
 * the fleet regardless of which page is open.
 */
export function MasterCautionBar() {
  const { data } = useFleetOverview()
  const { look } = useTheme()
  const totals = summarizeFleet(data)

  const critical = totals.alertsCritical
  const offline = totals.offline.length
  const warning = totals.alertsWarning
  const nominal = critical === 0 && offline === 0 && warning === 0
  const persistent = look === "glassFlightDeck"

  if (nominal && !persistent) return null

  const level: "ok" | "warn" | "error" = critical > 0 || offline > 0 ? "error" : warning > 0 ? "warn" : "ok"

  const parts: string[] = []
  if (critical > 0) parts.push(`${critical} critical alert${critical === 1 ? "" : "s"}`)
  if (offline > 0) parts.push(`${offline} connection${offline === 1 ? "" : "s"} offline`)
  if (warning > 0) parts.push(`${warning} warning${warning === 1 ? "" : "s"}`)
  if (nominal) parts.push("All systems nominal")

  return (
    <Link
      to="/alerts"
      role="status"
      className={cn(
        "group relative flex h-6 shrink-0 items-center justify-center gap-2 overflow-hidden px-4 text-center transition-colors",
        level === "error" && cn("bg-[var(--status-error)] text-white", persistent && "caution-band"),
        level === "warn" && cn("bg-[var(--status-warn)] text-black", persistent && "caution-band"),
        level === "ok" && "border-b border-[var(--border)] bg-[var(--bg-muted)] text-[var(--status-ok)]",
      )}
    >
      {level === "ok" ? <ShieldCheck className="h-3 w-3 shrink-0" aria-hidden /> : <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden />}
      <span className={cn("truncate text-[11px]", persistent ? "panel-label tracking-[0.12em]" : "font-medium")}>
        {persistent ? parts.join(" · ").toUpperCase() : parts.join(" · ")}
      </span>
      {offline > 0 && <WifiOff className="h-3.5 w-3.5 shrink-0" aria-hidden />}
      <ChevronRight className="h-3 w-3 shrink-0 opacity-60 transition-transform group-hover:translate-x-0.5" aria-hidden />
    </Link>
  )
}
