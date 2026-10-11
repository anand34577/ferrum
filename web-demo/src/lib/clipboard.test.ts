import { afterEach, describe, expect, it, vi } from "vitest"
import { copyText } from "./clipboard"

const original = Object.getOwnPropertyDescriptor(navigator, "clipboard")

function setClipboard(value: unknown) {
  Object.defineProperty(navigator, "clipboard", { value, configurable: true })
}

afterEach(() => {
  if (original) Object.defineProperty(navigator, "clipboard", original)
  else delete (navigator as { clipboard?: unknown }).clipboard
  vi.restoreAllMocks()
})

describe("copyText", () => {
  it("uses the async clipboard API when it exists", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    setClipboard({ writeText })
    expect(await copyText("secret")).toBe(true)
    expect(writeText).toHaveBeenCalledWith("secret")
  })

  it("falls back instead of throwing on an insecure (plain HTTP) origin", async () => {
    setClipboard(undefined)
    const exec = vi.fn().mockReturnValue(true)
    document.execCommand = exec
    expect(await copyText("secret")).toBe(true)
    expect(exec).toHaveBeenCalledWith("copy")
    expect(document.querySelector("textarea")).toBeNull()
  })

  it("falls back when the clipboard API rejects", async () => {
    setClipboard({ writeText: vi.fn().mockRejectedValue(new Error("denied")) })
    document.execCommand = vi.fn().mockReturnValue(true)
    expect(await copyText("secret")).toBe(true)
  })

  it("reports failure when neither path works", async () => {
    setClipboard(undefined)
    document.execCommand = vi.fn().mockReturnValue(false)
    expect(await copyText("secret")).toBe(false)
  })
})
