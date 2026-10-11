import { describe, expect, it } from "vitest"
import { addDays, addMonths, isSameDay, monthGrid, rangeToInstants, startOfDay } from "./dates"

describe("monthGrid", () => {
  it("always returns six full weeks starting on the requested weekday", () => {
    for (const weekStartsOn of [0, 1, 6]) {
      const grid = monthGrid(new Date(2026, 1, 1), weekStartsOn) // February 2026
      expect(grid).toHaveLength(42)
      expect(grid[0].getDay()).toBe(weekStartsOn)
      for (let i = 1; i < grid.length; i++) {
        expect(isSameDay(grid[i], addDays(grid[i - 1], 1))).toBe(true)
      }
    }
  })

  it("contains every day of the month", () => {
    const grid = monthGrid(new Date(2026, 9, 15), 1) // October 2026
    const inMonth = grid.filter((d) => d.getMonth() === 9)
    expect(inMonth).toHaveLength(31)
    expect(inMonth[0].getDate()).toBe(1)
  })
})

describe("date arithmetic", () => {
  it("rolls over month and year boundaries", () => {
    expect(isSameDay(addDays(new Date(2025, 11, 31), 1), new Date(2026, 0, 1))).toBe(true)
    expect(isSameDay(addDays(new Date(2026, 2, 1), -1), new Date(2026, 1, 28))).toBe(true)
    expect(addMonths(new Date(2026, 0, 31), 1).getMonth()).toBe(1)
  })

  it("startOfDay drops the time", () => {
    const d = startOfDay(new Date(2026, 4, 5, 17, 42, 9))
    expect([d.getHours(), d.getMinutes(), d.getSeconds()]).toEqual([0, 0, 0])
  })
})

describe("rangeToInstants", () => {
  it("covers both end days in full (half-open upper bound)", () => {
    const { from, to } = rangeToInstants({ from: new Date(2026, 2, 3, 15), to: new Date(2026, 2, 5, 9) })
    expect(new Date(from!).getTime()).toBe(new Date(2026, 2, 3).getTime())
    expect(new Date(to!).getTime()).toBe(new Date(2026, 2, 6).getTime())
  })

  it("treats a lone start day as a one-day range", () => {
    const { from, to } = rangeToInstants({ from: new Date(2026, 2, 3) })
    expect(new Date(to!).getTime() - new Date(from!).getTime()).toBe(24 * 3600 * 1000)
  })

  it("returns nothing for an empty range", () => {
    expect(rangeToInstants({})).toEqual({ from: undefined, to: undefined })
  })
})
