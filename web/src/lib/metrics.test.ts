import { describe, expect, it } from "vitest"
import { buildRRDRows, hasAnySeries, rowNum, type ChartRow } from "@/lib/metrics"
import type { RRDPoint } from "@/lib/api"

const avg: RRDPoint[] = [
  { time: 1700000000, cpu: 0.5, mem: 1024, netin: 100, netout: 50, swap: 2048, loadavg: 1.5, iowait: 0.1, pressureiosome: 0.3 },
  { time: 1700000060, cpu: 0.75, mem: 2048, netin: 200, netout: 80, swap: 4096, loadavg: 2.5, iowait: 0.2, pressureiosome: 0.6 },
]
const max: RRDPoint[] = [
  { time: 1700000000, cpu: 0.9, mem: 4096, netin: 500, netout: 300 },
  { time: 1700000060, cpu: 0.95, mem: 8192, netin: 900, netout: 700 },
]

describe("buildRRDRows", () => {
  it("scales fractions to percent and pairs avg with max bands", () => {
    const rows = buildRRDRows(avg, max, [{ key: "cpu", label: "CPU", color: "red", maxKey: "cpu" }])
    expect(rows).toHaveLength(2)
    expect(rows[0].cpu).toBe(50)
    expect(rows[1].cpu).toBe(75)
    expect(rows[0].cpuBand).toEqual([50, 90])
    expect(rows[1].cpuBand).toEqual([75, 95])
  })

  it("keeps requested byte/rate series and prunes everything else", () => {
    const rows = buildRRDRows(avg, undefined, [
      { key: "netin", label: "In", color: "blue" },
      { key: "netout", label: "Out", color: "green" },
    ])
    expect(rows[0].netin).toBe(100)
    expect(rows[0].netout).toBe(50)
    expect(Object.keys(rows[0]).sort()).toEqual(["netin", "netout", "time"])
  })

  it("omits bands when no MAX samples exist", () => {
    const rows = buildRRDRows(avg, undefined, [{ key: "cpu", label: "CPU", color: "red", maxKey: "cpu" }])
    expect(rows[0].cpuBand).toBeUndefined()
  })

  it("orders band tuples as [min, max] even if MAX dips below average", () => {
    const invertedMax = [{ time: 1700000000, cpu: 0.1 }]
    const rows = buildRRDRows([avg[0]], invertedMax, [{ key: "cpu", label: "CPU", color: "red", maxKey: "cpu" }])
    expect(rows[0].cpuBand).toEqual([10, 50])
  })

  it("normalizes PSI pressure: fraction-scale input becomes percent", () => {
    const rows = buildRRDRows(avg, undefined, [{ key: "pressureiosome", label: "IO some", color: "blue" }])
    expect(rows[0].pressureiosome).toBe(30)
    expect(rows[1].pressureiosome).toBe(60)
  })

  it("leaves PSI values alone when they are already percent-scale", () => {
    const pctAvg = avg.map((p) => ({ ...p, pressureiosome: p.pressureiosome! * 100 }))
    const rows = buildRRDRows(pctAvg, undefined, [{ key: "pressureiosome", label: "IO some", color: "blue" }])
    expect(rows[0].pressureiosome).toBe(30)
  })

  it("survives undefined input", () => {
    expect(buildRRDRows(undefined, undefined, [{ key: "cpu", label: "CPU", color: "red" }])).toEqual([])
  })
})

describe("hasAnySeries", () => {
  it("detects presence and absence of series data", () => {
    const rows: ChartRow[] = buildRRDRows(avg, undefined, [
      { key: "iowait", label: "IO", color: "x" },
      { key: "pressureiofull", label: "full", color: "x" },
    ])
    expect(hasAnySeries(rows, ["iowait"])).toBe(true)
    expect(hasAnySeries(rows, ["pressureiofull"])).toBe(false)
  })
})

describe("rowNum", () => {
  it("returns numbers and filters band tuples", () => {
    expect(rowNum({ time: 1, cpu: 42 }, "cpu")).toBe(42)
    expect(rowNum({ time: 1, cpuBand: [1, 2] }, "cpuBand")).toBeUndefined()
    expect(rowNum({ time: 1 }, "mem")).toBeUndefined()
  })
})
