import { describe, expect, it } from "vitest"
import { csvField, toCsv } from "./download"

describe("csvField", () => {
  it("passes plain values through", () => {
    expect(csvField("pve1")).toBe("pve1")
    expect(csvField(42)).toBe("42")
    expect(csvField(undefined)).toBe("")
    expect(csvField(null)).toBe("")
  })

  it("quotes delimiters, quotes and newlines", () => {
    expect(csvField("a,b")).toBe('"a,b"')
    expect(csvField('say "hi"')).toBe('"say ""hi"""')
    expect(csvField("line1\nline2")).toBe('"line1\nline2"')
  })

  it("neutralises spreadsheet formulas", () => {
    expect(csvField("=HYPERLINK(1)")).toBe("'=HYPERLINK(1)")
    expect(csvField("+1")).toBe("'+1")
    expect(csvField("@cmd")).toBe("'@cmd")
    expect(csvField("-5")).toBe("'-5")
  })
})

describe("toCsv", () => {
  it("joins rows with CRLF under the header", () => {
    expect(toCsv(["Name", "Age"], [["Alice", 30], ["Bob, Jr", 25]])).toBe('Name,Age\r\nAlice,30\r\n"Bob, Jr",25')
  })
})
