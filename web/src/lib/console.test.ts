import { describe, expect, it } from "vitest"
import type { ClusterResource } from "./api"
import { buildConsoleUrl } from "./console"

const guest: ClusterResource = {
  id: "qemu/100",
  type: "qemu",
  node: "pve1",
  vmid: 100,
  name: "web01",
  status: "running",
}

describe("buildConsoleUrl", () => {
  it("includes the minted ws path and a human-readable name", () => {
    const url = buildConsoleUrl("conn1", guest, "/ws/console/abc123")
    const params = new URLSearchParams(url.split("?")[1])

    expect(url.startsWith("/console?")).toBe(true)
    expect(params.get("ws")).toBe("/ws/console/abc123")
    expect(params.get("name")).toBe("web01 (#100)")
  })

  it("includes guest coordinates so ConsolePage can re-mint a session on reconnect", () => {
    const url = buildConsoleUrl("conn1", guest, "/ws/console/abc123")
    const params = new URLSearchParams(url.split("?")[1])

    expect(params.get("connId")).toBe("conn1")
    expect(params.get("type")).toBe("qemu")
    expect(params.get("node")).toBe("pve1")
    expect(params.get("vmid")).toBe("100")
  })

  it("carries the VNC ticket as pw so noVNC can answer PVE's credentials challenge", () => {
    const url = buildConsoleUrl("conn1", guest, "/ws/console/abc123", "ticket-xyz")
    const params = new URLSearchParams(url.split("?")[1])
    expect(params.get("pw")).toBe("ticket-xyz")

    // Optional: no ticket minted -> no pw param.
    const bare = new URLSearchParams(buildConsoleUrl("conn1", guest, "/ws/console/abc123").split("?")[1])
    expect(bare.get("pw")).toBeNull()
  })

  it("falls back to a generic label when the guest has no name", () => {
    const unnamed: ClusterResource = { ...guest, name: undefined }
    const url = buildConsoleUrl("conn1", unnamed, "/ws/console/abc123")
    const params = new URLSearchParams(url.split("?")[1])
    expect(params.get("name")).toBe("guest (#100)")
  })
})
