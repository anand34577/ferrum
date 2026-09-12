import type { ClusterResource } from "@/lib/api"

// Builds the URL for the fullscreen console viewer, passing both the
// already-minted (single-use) WebSocket path for immediate connection and
// the guest coordinates so ConsolePage can mint a fresh session on reconnect.
// `password` is the PVE VNC ticket — PVE also uses it as the VNC-level
// password during the RFB handshake, so noVNC needs it to answer the
// credentials challenge. Same short-lived, single-use trust window as ws.
export function buildConsoleUrl(connId: string, guest: ClusterResource, wsPath: string, password?: string): string {
  const params = new URLSearchParams({
    kind: "vnc",
    ws: wsPath,
    name: `${guest.name ?? "guest"}${guest.vmid !== undefined ? ` (#${guest.vmid})` : ""}`,
    connId,
    type: guest.type,
    node: guest.node,
    ...(guest.vmid !== undefined ? { vmid: String(guest.vmid) } : {}),
    ...(password ? { pw: password } : {}),
  })
  return `/console?${params.toString()}`
}

// Builds the URL for the fullscreen shell (xterm.js) viewer — same
// reconnect-friendly shape as buildConsoleUrl, minus the VNC-only password.
// A node-level shell has no guest, so type/vmid are omitted.
export function buildShellUrl(connId: string, name: string, wsPath: string, node: string, guest?: ClusterResource): string {
  const params = new URLSearchParams({
    kind: "shell",
    ws: wsPath,
    name,
    connId,
    node,
    ...(guest ? { type: guest.type } : {}),
    ...(guest?.vmid !== undefined ? { vmid: String(guest.vmid) } : {}),
  })
  return `/console?${params.toString()}`
}
