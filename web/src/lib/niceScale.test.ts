import { describe, expect, it } from "vitest"
import { computeNiceScale } from "@/lib/niceScale"

describe("computeNiceScale", () => {
  it("rounds an awkward max up to a nice round number", () => {
    const { max, ticks } = computeNiceScale(18.6)
    expect(max).toBeGreaterThanOrEqual(18.6)
    // Every tick should be a "nice" value, not an arbitrary fraction.
    for (const t of ticks) {
      expect(Number.isInteger(t) || t.toString().split(".")[1]?.length <= 1).toBe(true)
    }
    expect(ticks[0]).toBe(0)
    expect(ticks[ticks.length - 1]).toBe(max)
  })

  it("produces evenly-spaced ticks", () => {
    const { ticks } = computeNiceScale(100)
    const step = ticks[1] - ticks[0]
    for (let i = 1; i < ticks.length; i++) {
      expect(ticks[i] - ticks[i - 1]).toBeCloseTo(step)
    }
  })

  it("never returns a degenerate 0..0 scale for an all-zero series", () => {
    const { max } = computeNiceScale(0)
    expect(max).toBeGreaterThan(0)
  })
})
