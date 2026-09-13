import type { RRDPoint } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import { formatRRDTick, formatRate } from "@/lib/utils"

/**
 * Metric-shaping layer between PVE's RRD samples and the chart components.
 *
 * One chart = one unit family (percent, bytes, bytes/sec, load) so the Y axis
 * stays readable — mixing units on a shared axis is what made the old charts
 * hard to read. Each series can carry a "peak" companion (cf=MAX from PVE)
 * rendered as a translucent envelope band around the average line.
 */

export type RRDTimeframe = "hour" | "day" | "week" | "month" | "year"

export interface ChartRow {
  time: number
  [key: string]: number | [number, number] | undefined
}

/** Type-safe numeric read from a chart row (band tuples hide behind the
 * index signature — this filters them out). */
export function rowNum(row: ChartRow, key: string): number | undefined {
  const v = row[key]
  return typeof v === "number" ? v : undefined
}

/** Which RRD keys a chart family needs, mapped to its display series. */
export interface SeriesSpec {
  key: string
  label: string
  color: string
  /** Render the cf=MAX companion as an envelope band around this series. */
  maxKey?: string
}

const PCT = (v: number) => `${v.toFixed(1)}%`
export const rateFormatter = (v: number) => formatRate(v)
const countFormatter = (v: number) => v.toFixed(2)

/** formatBytes that tolerates undefined chart gaps. */
function formatRRDBytes(v: number): string {
  if (!Number.isFinite(v)) return "-"
  return formatBytes(v)
}

/**
 * Builds chart rows from an AVERAGE (+ optional MAX) RRD sample set.
 * Percent-like series (cpu, iowait) are scaled ×100; every series listed in
 * `specs` gets a `<key>MaxBand` [avg, max] tuple when MAX data covers it, so
 * ResourceAreaChart can draw peak envelopes.
 *
 * PSI pressure columns are normalized adaptively: some PVE releases store
 * them as 0..1 fractions, others as 0..100 — we detect the scale from the
 * data and emit uniform percentages.
 */
export function buildRRDRows(avg: RRDPoint[] | undefined, max: RRDPoint[] | undefined, specs: SeriesSpec[]): ChartRow[] {
  if (!avg || avg.length === 0) return []

  // Index the MAX samples by timestamp for band lookups.
  const maxByTime = new Map<number, RRDPoint>()
  for (const p of max ?? []) if (p.time) maxByTime.set(p.time, p)

  // Detect PSI scale once across all samples: if the largest observed value
  // is ≤ 1.5 the data is a fraction, so scale everything ×100.
  const pressureKeys = ["pressurecpusome", "pressureiosome", "pressureiofull", "pressurememorysome", "pressurememoryfull"] as const
  let psiScale = 100
  let psiSeen = false
  for (const p of avg) {
    for (const k of pressureKeys) {
      const v = p[k]
      if (typeof v === "number") {
        psiSeen = true
        psiScale = Math.min(psiScale, v > 1.5 ? 1 : 100)
      }
    }
  }

  const rows: ChartRow[] = []
  for (const p of avg) {
    if (!p.time) continue
    const row: ChartRow = { time: p.time }
    const maxP = maxByTime.get(p.time)

    const put = (key: string, avgVal: number | undefined, maxVal: number | undefined) => {
      if (typeof avgVal === "number" && Number.isFinite(avgVal)) row[key] = avgVal
      if (typeof avgVal === "number" && typeof maxVal === "number" && Number.isFinite(maxVal)) {
        // Envelope between the average line and the cf=MAX peak (ordered,
        // since RRD corner cases can briefly put MAX below AVERAGE).
        row[`${key}Band` as keyof ChartRow] = [Math.min(avgVal, maxVal), Math.max(avgVal, maxVal)] as [number, number]
      }
    }

    put("cpu", p.cpu !== undefined ? p.cpu * 100 : undefined, maxP?.cpu !== undefined ? maxP.cpu * 100 : undefined)
    put("mem", p.mem, maxP?.mem)
    put("swap", p.swap, maxP?.swap)
    put("netin", p.netin, maxP?.netin)
    put("netout", p.netout, maxP?.netout)
    put("diskread", p.diskread, maxP?.diskread)
    put("diskwrite", p.diskwrite, maxP?.diskwrite)
    put("iowait", p.iowait !== undefined ? p.iowait * 100 : undefined, maxP?.iowait !== undefined ? maxP.iowait * 100 : undefined)
    put("loadavg", p.loadavg, maxP?.loadavg)
    if (psiSeen) {
      put("pressurecpusome", p.pressurecpusome !== undefined ? p.pressurecpusome * psiScale : undefined,
        maxP?.pressurecpusome !== undefined ? maxP.pressurecpusome * psiScale : undefined)
      put("pressureiosome", p.pressureiosome !== undefined ? p.pressureiosome * psiScale : undefined,
        maxP?.pressureiosome !== undefined ? maxP.pressureiosome * psiScale : undefined)
      put("pressureiofull", p.pressureiofull !== undefined ? p.pressureiofull * psiScale : undefined,
        maxP?.pressureiofull !== undefined ? maxP.pressureiofull * psiScale : undefined)
      put("pressurememorysome", p.pressurememorysome !== undefined ? p.pressurememorysome * psiScale : undefined,
        maxP?.pressurememorysome !== undefined ? maxP.pressurememorysome * psiScale : undefined)
      put("pressurememoryfull", p.pressurememoryfull !== undefined ? p.pressurememoryfull * psiScale : undefined,
        maxP?.pressurememoryfull !== undefined ? maxP.pressurememoryfull * psiScale : undefined)
    }
    // Per-device guest stats (nics_*, blockstat_*) pass through as-is.
    if (p.extra) for (const [k, v] of Object.entries(p.extra)) put(k, v, maxP?.extra?.[k])

    rows.push(row)
  }

  // Drop series the caller didn't ask for so charts stay focused. (Bands are
  // requested implicitly via spec.maxKey.)
  const wanted = new Set<string>()
  for (const s of specs) {
    wanted.add(s.key)
    if (s.maxKey) wanted.add(`${s.key}Band`)
  }
  wanted.add("time")
  for (const row of rows) {
    for (const k of Object.keys(row)) {
      if (!wanted.has(k)) delete row[k]
    }
  }
  return rows
}

/** Standard metric groups shared by the node page and guest dialog. */
export const NODE_SERIES = {
  cpu: (max = false): SeriesSpec[] => [{ key: "cpu", label: "CPU", color: "var(--chart-1)", ...(max ? { maxKey: "cpu" } : {}) }],
  memory: (max = false): SeriesSpec[] => [
    { key: "mem", label: "RAM used", color: "var(--chart-2)", ...(max ? { maxKey: "mem" } : {}) },
    { key: "swap", label: "Swap used", color: "var(--chart-4)", ...(max ? { maxKey: "swap" } : {}) },
  ],
  network: (max = false): SeriesSpec[] => [
    { key: "netin", label: "In", color: "var(--chart-3)", ...(max ? { maxKey: "netin" } : {}) },
    { key: "netout", label: "Out", color: "var(--chart-5)", ...(max ? { maxKey: "netout" } : {}) },
  ],
  ioWait: (max = false): SeriesSpec[] => [
    { key: "iowait", label: "IO wait", color: "var(--chart-4)", ...(max ? { maxKey: "iowait" } : {}) },
  ],
  loadavg: (max = false): SeriesSpec[] => [{ key: "loadavg", label: "Load (1m)", color: "var(--chart-6)", ...(max ? { maxKey: "loadavg" } : {}) }],
  pressureIO: (): SeriesSpec[] => [
    { key: "pressureiosome", label: "IO some", color: "var(--chart-3)" },
    { key: "pressureiofull", label: "IO full", color: "var(--status-error)" },
  ],
  pressureMemory: (): SeriesSpec[] => [
    { key: "pressurememorysome", label: "Mem some", color: "var(--chart-3)" },
    { key: "pressurememoryfull", label: "Mem full", color: "var(--status-error)" },
  ],
}

export const GUEST_SERIES = {
  cpu: (max = false): SeriesSpec[] => NODE_SERIES.cpu(max),
  memory: (max = false): SeriesSpec[] => [
    { key: "mem", label: "Memory used", color: "var(--chart-2)", ...(max ? { maxKey: "mem" } : {}) },
  ],
  network: (max = false): SeriesSpec[] => NODE_SERIES.network(max),
  disk: (max = false): SeriesSpec[] => [
    { key: "diskread", label: "Read", color: "var(--chart-3)", ...(max ? { maxKey: "diskread" } : {}) },
    { key: "diskwrite", label: "Write", color: "var(--chart-5)", ...(max ? { maxKey: "diskwrite" } : {}) },
  ],
}

export const FORMATTERS = {
  pct: PCT,
  bytes: formatRRDBytes,
  rate: rateFormatter,
  count: countFormatter,
}

/** True when the RRD set actually contains any of the given keys — used to
 * hide PSI/per-device charts on PVE versions that don't provide them. */
export function hasAnySeries(rows: ChartRow[], keys: string[]): boolean {
  if (rows.length === 0) return false
  return keys.some((k) => rows.some((r) => typeof r[k] === "number"))
}

export { formatRRDTick }
