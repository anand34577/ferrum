import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"]

/** Which BYTE_UNITS index a value naturally falls into — the unit formatBytes
 * would pick on its own. Exposed so a chart axis can lock every tick to the
 * same unit (the axis's own max, not each tick's own magnitude) instead of
 * every tick picking its own — see formatBytesAtUnit. */
export function byteUnitIndex(bytes: number): number {
  if (!bytes) return 0
  return Math.min(BYTE_UNITS.length - 1, Math.max(0, Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024))))
}

/** Up to 2 decimal places, trimmed when exact (18 GB, not 18.00 GB) — never
 * fewer than the precision the value actually needs, capped at 2. */
export function formatBytes(bytes: number): string {
  if (!bytes) return "0 B"
  const i = byteUnitIndex(bytes)
  return formatBytesAtUnit(bytes, i)
}

/** formatBytes, but forced to a specific unit (from byteUnitIndex) rather
 * than picking its own — so every tick on one chart axis reads in the same
 * unit as the axis's max value, instead of each tick switching units
 * independently (the "4.7 GB" next to "9.3 GB" next to "1.1 TB" problem). */
export function formatBytesAtUnit(bytes: number, unitIndex: number): string {
  const value = bytes / 1024 ** unitIndex
  // toFixed(2) then re-parse to drop trailing zeros (19.20 -> 19.2, 18.00 -> 18).
  return `${parseFloat(value.toFixed(2))} ${BYTE_UNITS[unitIndex]}`
}

export function formatUptime(seconds: number): string {
  if (!seconds) return "-"
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

export function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`
}

/** Formats a bytes-per-second rate, e.g. 1.5 MB/s. */
export function formatRate(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec < 0) return "0 B/s"
  return `${formatBytes(bytesPerSec)}/s`
}

/** Adaptive precision percent for tooltips/charts: 12.3% (not 12% or 12.34%). */
export function formatPercentFine(pct: number): string {
  return `${pct.toFixed(1)}%`
}

/** Formats an RRD unix timestamp for chart axis ticks, with granularity
 * chosen by the visible time span (minutes → hours → days → months). */
export function formatRRDTick(unixSeconds: number, spanSeconds: number): string {
  const d = new Date(unixSeconds * 1000)
  if (spanSeconds <= 3 * 3600) return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  if (spanSeconds <= 4 * 86400) return d.toLocaleString([], { weekday: "short", hour: "2-digit" })
  if (spanSeconds <= 400 * 86400) return d.toLocaleDateString([], { month: "short", day: "numeric" })
  return d.toLocaleDateString([], { month: "short", year: "2-digit" })
}

/** Full timestamp for chart tooltips. */
export function formatRRDTooltip(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

/** Coarse relative time for "last checked 2m ago"-style status lines. */
export function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (!Number.isFinite(then)) return "unknown"
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (secs < 60) return `${secs}s ago`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  return `${Math.floor(secs / 86400)}d ago`
}

/** Badge tone for a guest/node running state — shared so "running"/"online"
 * vs "stopped"/"offline" doesn't drift between pages. */
export function guestStatusVariant(status?: string): "ok" | "warn" | "error" | "default" {
  if (status === "running" || status === "online") return "ok"
  if (status === "stopped" || status === "offline") return "error"
  return "default"
}

/** Guest power state as a dot color — running green, anything not running
 * (stopped/paused/unknown) red so a guest that's down reads as "attention"
 * everywhere, not as a neutral gray. */
export function guestDotStatus(status?: string): "ok" | "warn" | "error" {
  if (status === "running") return "ok"
  if (status === "stopped") return "error"
  return "warn"
}

