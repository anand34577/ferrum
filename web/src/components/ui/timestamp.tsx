import { cn, formatRelativeTime } from "@/lib/utils"

/**
 * One timestamp rendering for the whole app. Before this existed the same
 * "when did this happen" job was done three ways per page (raw
 * toLocaleString, a custom two-line stack, and formatRelativeTime) — see
 * the audit's consistency findings.
 *
 * Default renders relative ("4m ago") with the full locale timestamp on
 * hover/title; `mode="absolute"` renders the full timestamp inline (audit /
 * forensic surfaces) with the relative age in the title instead. `tabular`
 * keeps digits aligned in table columns.
 */
export function Timestamp({
  iso,
  mode = "relative",
  className,
}: {
  iso: string | undefined
  mode?: "relative" | "absolute"
  className?: string
}) {
  if (!iso) return <span className={className}>—</span>
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return <span className={className}>{iso}</span>
  const full = d.toLocaleString([], { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
  return (
    <span className={cn("tabular", className)} title={mode === "absolute" ? formatRelativeTime(iso) : full}>
      {mode === "absolute" ? full : formatRelativeTime(iso)}
    </span>
  )
}
