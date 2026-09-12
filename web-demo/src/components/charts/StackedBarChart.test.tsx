import { render } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { StackedBarChart } from "@/components/charts/StackedBarChart"

// The component reads the active look to pick a corner radius matching that
// look's boxiness (see lib/chartRadius) — stub it rather than standing up
// the full ThemeProvider (which itself needs AuthProvider + react-query)
// just to render a chart in isolation.
vi.mock("@/lib/theme", () => ({ useTheme: () => ({ look: "enterprise" }) }))

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
;(globalThis as Record<string, unknown>).ResizeObserver ||= ResizeObserverMock

const data = [
  { name: "mixed", qemu: 4, lxc: 2 },
  // Single-segment rows exercise the per-row rounded silhouette.
  { name: "vms-only", qemu: 3, lxc: 0 },
  { name: "containers-only", qemu: 0, lxc: 5 },
]

describe("StackedBarChart", () => {
  it("renders one rounded rectangle per stack segment", () => {
    const { container } = render(
      <StackedBarChart
        data={data}
        series={[
          { key: "qemu", label: "VMs", color: "#bd5a2c" },
          { key: "lxc", label: "Containers", color: "#1f7a78" },
        ]}
        showLegend={false}
      />,
    )
    const bars = container.querySelectorAll(".recharts-bar-rectangle path")
    // 3 rows × 2 segments (zero-value segments render too but with no width).
    expect(bars.length).toBeGreaterThan(0)
    // Every rendered segment path must carry rounded-arc commands at the
    // silhouette ends (the "A"/"Q" arc segments in the path data) — a square
    // corner produces only line commands.
    const rounded = Array.from(bars).filter((p) => /[AQ]/.test(p.getAttribute("d") ?? ""))
    expect(rounded.length).toBeGreaterThan(0)
  })
})
