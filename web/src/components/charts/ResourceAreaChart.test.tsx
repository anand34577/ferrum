import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { ResourceAreaChart } from "@/components/charts/ResourceAreaChart"

// ResponsiveContainer relies on ResizeObserver, which jsdom doesn't ship —
// report a fixed size once so the chart actually computes its layout.
class ResizeObserverMock {
  private cb: ResizeObserverCallback | undefined
  constructor(cb: ResizeObserverCallback) {
    this.cb = cb
  }
  observe = vi.fn((el: Element) => {
    this.cb?.([{ target: el, contentRect: { width: 400, height: 120 } } as ResizeObserverEntry], this as unknown as ResizeObserver)
  })
  unobserve = vi.fn()
  disconnect = vi.fn()
}
;(globalThis as Record<string, unknown>).ResizeObserver = ResizeObserverMock

const rows = [
  { time: 1700000000, cpu: 50, cpuBand: [50, 90] },
  { time: 1700000060, cpu: 75, cpuBand: [75, 95] },
]

describe("ResourceAreaChart", () => {
  it("renders the average series and the peak envelope band", () => {
    const { container } = render(
      <ResourceAreaChart
        data={rows}
        series={[{ key: "cpu", label: "CPU", color: "#bd5a2c", band: true, formatter: (v) => `${v}%` }]}
        yDomain={[0, 100]}
        height={120}
      />,
    )
    // The band + the main area produce two <path> elements.
    const paths = container.querySelectorAll("path.recharts-area-curve, path.recharts-area-area")
    expect(paths.length).toBeGreaterThanOrEqual(2)
    // Legend stays hidden for a single series.
    expect(screen.queryByText("CPU")).toBeNull()
  })

  it("shows a legend when several series are present", () => {
    render(
      <ResourceAreaChart
        data={[
          { time: 1700000000, netin: 10, netout: 5 },
          { time: 1700000060, netin: 20, netout: 8 },
        ]}
        series={[
          { key: "netin", label: "In", color: "#123456" },
          { key: "netout", label: "Out", color: "#654321" },
        ]}
        showLegend
        height={120}
      />,
    )
    expect(screen.getByText("In")).toBeInTheDocument()
    expect(screen.getByText("Out")).toBeInTheDocument()
  })
})
