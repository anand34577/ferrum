import { describe, expect, it } from "vitest"
import type { ClusterResource } from "./api"
import {
  CONSOLE_PARAMS_MESSAGE,
  CONSOLE_READY_MESSAGE,
  buildConsoleUrl,
  buildShellUrl,
  isConsoleReadyMessage,
  parseConsoleParams,
} from "./console"

const guest: ClusterResource = {
  id: "qemu/100",
  type: "qemu",
  node: "pve1",
  vmid: 100,
  name: "web01",
  status: "running",
}

describe("buildConsoleUrl", () => {
  it("carries only non-secret params — the ws path and VNC ticket never enter the URL", () => {
    const url = buildConsoleUrl("conn1", guest)
    const params = new URLSearchParams(url.split("?")[1])

    expect(url.startsWith("/console?")).toBe(true)
    expect(params.get("kind")).toBe("vnc")
    expect(params.get("name")).toBe("web01 (#100)")
    expect(params.get("ws")).toBeNull()
    expect(params.get("pw")).toBeNull()
  })

  it("includes guest coordinates so ConsolePage can re-mint a session on reconnect", () => {
    const url = buildConsoleUrl("conn1", guest)
    const params = new URLSearchParams(url.split("?")[1])

    expect(params.get("connId")).toBe("conn1")
    expect(params.get("type")).toBe("qemu")
    expect(params.get("node")).toBe("pve1")
    expect(params.get("vmid")).toBe("100")
  })

  it("falls back to a generic label when the guest has no name", () => {
    const unnamed: ClusterResource = { ...guest, name: undefined }
    const url = buildConsoleUrl("conn1", unnamed)
    const params = new URLSearchParams(url.split("?")[1])
    expect(params.get("name")).toBe("guest (#100)")
  })
})

describe("buildShellUrl", () => {
  it("keeps the shell kind and node, and never embeds the ticketed ws path", () => {
    const url = buildShellUrl("conn1", "pve1", "pve1")
    const params = new URLSearchParams(url.split("?")[1])

    expect(url.startsWith("/console?")).toBe(true)
    expect(params.get("kind")).toBe("shell")
    expect(params.get("name")).toBe("pve1")
    expect(params.get("node")).toBe("pve1")
    expect(params.get("ws")).toBeNull()
    expect(params.get("type")).toBeNull()
    expect(params.get("vmid")).toBeNull()
  })

  it("tags guest shells with the guest's type/vmid coordinates", () => {
    const params = new URLSearchParams(buildShellUrl("conn1", "web01", "pve1", guest).split("?")[1])
    expect(params.get("type")).toBe("qemu")
    expect(params.get("vmid")).toBe("100")
  })
})

describe("parseConsoleParams", () => {
  it("accepts the opener's params message and normalizes the optional password", () => {
    expect(parseConsoleParams({ type: CONSOLE_PARAMS_MESSAGE, wsPath: "/ws/console/abc123", password: "ticket-xyz" })).toEqual({
      wsPath: "/ws/console/abc123",
      password: "ticket-xyz",
    })
    // Shell handoffs carry no password; an empty one is dropped too.
    expect(parseConsoleParams({ type: CONSOLE_PARAMS_MESSAGE, wsPath: "/ws/shell/abc123" })).toEqual({ wsPath: "/ws/shell/abc123" })
    expect(parseConsoleParams({ type: CONSOLE_PARAMS_MESSAGE, wsPath: "/ws/shell/abc123", password: "" })).toEqual({ wsPath: "/ws/shell/abc123" })
  })

  it("rejects wrong types, missing/empty wsPath, and non-object payloads", () => {
    expect(parseConsoleParams({ type: CONSOLE_READY_MESSAGE, wsPath: "/ws/console/abc123" })).toBeNull()
    expect(parseConsoleParams({ type: CONSOLE_PARAMS_MESSAGE })).toBeNull()
    expect(parseConsoleParams({ type: CONSOLE_PARAMS_MESSAGE, wsPath: "" })).toBeNull()
    expect(parseConsoleParams(null)).toBeNull()
    expect(parseConsoleParams(undefined)).toBeNull()
    expect(parseConsoleParams("ferrum-console-params")).toBeNull()
  })
})

describe("isConsoleReadyMessage", () => {
  it("matches only the popup's ready signal", () => {
    expect(isConsoleReadyMessage({ type: CONSOLE_READY_MESSAGE })).toBe(true)
    expect(isConsoleReadyMessage({ type: "something-else" })).toBe(false)
    expect(isConsoleReadyMessage({ type: CONSOLE_READY_MESSAGE, extra: true })).toBe(true)
    expect(isConsoleReadyMessage(null)).toBe(false)
    expect(isConsoleReadyMessage(undefined)).toBe(false)
    expect(isConsoleReadyMessage(CONSOLE_READY_MESSAGE)).toBe(false)
  })
})
