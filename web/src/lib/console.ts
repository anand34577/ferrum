import type { ClusterResource } from "@/lib/api"

// Builds the URL for the fullscreen console viewer. Deliberately carries only
// non-secret routing info: the guest coordinates (connId/type/node/vmid) so
// ConsolePage can mint a fresh session on reconnect, plus a display name.
// The single-use ticket itself — the ticketed WebSocket path, and the PVE VNC
// ticket that doubles as the RFB password — is handed to the popup over
// postMessage (see openConsolePopup below) instead of pw=/ws= query params,
// which would sit visible in the address bar until React mounted and scrubbed
// them.
export function buildConsoleUrl(connId: string, guest: ClusterResource): string {
  const params = new URLSearchParams({
    kind: "vnc",
    name: `${guest.name ?? "guest"}${guest.vmid !== undefined ? ` (#${guest.vmid})` : ""}`,
    connId,
    type: guest.type,
    node: guest.node,
    ...(guest.vmid !== undefined ? { vmid: String(guest.vmid) } : {}),
  })
  return `/console?${params.toString()}`
}

// Builds the URL for the fullscreen shell (xterm.js) viewer — same
// non-secret shape as buildConsoleUrl, with the ticketed ws path handed over
// via postMessage rather than the URL. A node-level shell has no guest, so
// type/vmid are omitted.
export function buildShellUrl(connId: string, name: string, node: string, guest?: ClusterResource): string {
  const params = new URLSearchParams({
    kind: "shell",
    name,
    connId,
    node,
    ...(guest ? { type: guest.type } : {}),
    ...(guest?.vmid !== undefined ? { vmid: String(guest.vmid) } : {}),
  })
  return `/console?${params.toString()}`
}

// --- opener ↔ popup postMessage handoff -------------------------------------
//
// Console session tickets are single-use, so they must reach the popup
// exactly once without transiting the URL. The popup announces itself with
// `ferrum-console-ready`; the opener replies with `ferrum-console-params`
// carrying the ticketed WebSocket path and, for VNC, the PVE ticket that
// noVNC needs to answer the RFB credentials challenge.

export const CONSOLE_READY_MESSAGE = "ferrum-console-ready"
export const CONSOLE_PARAMS_MESSAGE = "ferrum-console-params"

/** The ticketed session the opener hands the popup once the latter is ready. */
export interface ConsoleHandoff {
  wsPath: string
  password?: string
}

// Narrows an untrusted postMessage payload into the handoff, or null when it
// isn't one. Pure on purpose so the message contract is unit-testable
// without a real popup window.
export function parseConsoleParams(data: unknown): ConsoleHandoff | null {
  if (typeof data !== "object" || data === null) return null
  const msg = data as { type?: unknown; wsPath?: unknown; password?: unknown }
  if (msg.type !== CONSOLE_PARAMS_MESSAGE || typeof msg.wsPath !== "string" || msg.wsPath === "") return null
  return {
    wsPath: msg.wsPath,
    ...(typeof msg.password === "string" && msg.password !== "" ? { password: msg.password } : {}),
  }
}

export function isConsoleReadyMessage(data: unknown): boolean {
  return typeof data === "object" && data !== null && (data as { type?: unknown }).type === CONSOLE_READY_MESSAGE
}

/**
 * Opens the console popup and hands it its single-use ticket over
 * postMessage: listens once for the popup's ready signal (only from the
 * window we opened, same origin), replies with the ticketed params using the
 * app's origin as targetOrigin, then removes the listener. If the popup never
 * signals ready (blocked, or closed before mounting) the listener just stays
 * quiet until the page itself goes away.
 */
export function openConsolePopup(url: string, features: string, handoff: ConsoleHandoff): void {
  const popup = window.open(url, "_blank", features)
  if (!popup) return // popup blocked — there is nothing to hand a ticket to
  const onMessage = (evt: MessageEvent) => {
    if (evt.source !== popup || evt.origin !== window.location.origin) return
    if (!isConsoleReadyMessage(evt.data)) return
    window.removeEventListener("message", onMessage)
    popup.postMessage({ type: CONSOLE_PARAMS_MESSAGE, ...handoff }, window.location.origin)
  }
  window.addEventListener("message", onMessage)
}
