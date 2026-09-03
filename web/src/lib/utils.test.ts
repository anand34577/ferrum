import { describe, expect, it } from "vitest"
import { cn, formatBytes, formatPercent, formatUptime } from "./utils"

describe("formatBytes", () => {
  it("formats zero as 0 B", () => {
    expect(formatBytes(0)).toBe("0 B")
  })

  it("keeps raw byte counts unrounded", () => {
    expect(formatBytes(512)).toBe("512 B")
  })

  it("formats kilobytes with one decimal below 10", () => {
    expect(formatBytes(1536)).toBe("1.5 KB")
  })

  it("drops the decimal once the value reaches double digits", () => {
    expect(formatBytes(10 * 1024)).toBe("10 KB")
  })

  it("formats gigabytes", () => {
    expect(formatBytes(2.5 * 1024 ** 3)).toBe("2.5 GB")
  })

  it("formats terabytes", () => {
    expect(formatBytes(1024 ** 4)).toBe("1.0 TB")
  })
})

describe("formatUptime", () => {
  it("returns a dash for zero", () => {
    expect(formatUptime(0)).toBe("-")
  })

  it("formats minutes only under an hour", () => {
    expect(formatUptime(45 * 60)).toBe("45m")
  })

  it("formats hours and minutes under a day", () => {
    expect(formatUptime(2 * 3600 + 30 * 60)).toBe("2h 30m")
  })

  it("formats days and hours once over a day", () => {
    expect(formatUptime(3 * 86400 + 5 * 3600)).toBe("3d 5h")
  })
})

describe("formatPercent", () => {
  it("converts a 0-1 fraction to a rounded percent string", () => {
    expect(formatPercent(0.5)).toBe("50%")
  })

  it("rounds to the nearest whole percent", () => {
    expect(formatPercent(0.876)).toBe("88%")
  })

  it("handles zero", () => {
    expect(formatPercent(0)).toBe("0%")
  })
})

describe("cn", () => {
  it("merges class names and resolves Tailwind conflicts", () => {
    expect(cn("px-2 py-1", "px-4")).toBe("py-1 px-4")
  })

  it("drops falsy values", () => {
    expect(cn("a", false, undefined, null, "b")).toBe("a b")
  })
})
