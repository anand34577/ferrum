import { AlertTriangle, Keyboard, Loader2, Maximize, RotateCcw, Wifi, WifiOff, X } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { CONSOLE_READY_MESSAGE, parseConsoleParams, type ConsoleHandoff } from "@/lib/console"

type ConnectionState = "connecting" | "connected" | "disconnected" | "error"

// How long to wait for the opener's postMessage handoff before concluding
// it is gone (popup refresh keeps window.opener alive without a listener —
// see the handoff effect below).
const HANDOFF_TIMEOUT_MS = 5000

type RFBConstructor = new (target: HTMLElement, url: string) => import("@novnc/novnc/lib/rfb.js").default

// noVNC 1.5 ships lib/rfb.js as CommonJS (`exports.default = RFB`), so the
// class's location depends on the bundler's CJS interop: under Vite the
// dynamic import's `.default` is the whole module.exports object (whose own
// `.default` is the class) — assuming `mod.default` *is* the class crashes
// at `new RFB(...)` with "d is not a constructor". Unwrap until we actually
// hold a constructor function.
async function loadRFB(): Promise<RFBConstructor> {
  const mod = (await import("@novnc/novnc/lib/rfb.js")) as unknown as Record<string, unknown>
  let candidate: unknown = mod?.default ?? mod
  if (candidate && typeof candidate === "object" && typeof (candidate as Record<string, unknown>).default === "function") {
    candidate = (candidate as Record<string, unknown>).default
  }
  if (typeof candidate !== "function") {
    throw new Error("Console library failed to load.")
  }
  return candidate as RFBConstructor
}

function missingSessionMessage(kind: string): string {
  if (kind === "ssh") return "Missing SSH session — open this from the SSH Shell dialog."
  if (kind === "shell") return "Missing shell session — open this from a Shell button."
  return "Missing console session — open this from a guest's console button."
}

// Mints a fresh session for whichever console kind this page is showing —
// each backend session token is single-use, so reconnecting needs a new one.
async function openSession(kind: string, connId: string, guestType: string | null, node: string, vmid: string | null) {
  const isGuestShell = kind === "shell" && guestType && vmid
  const path = isGuestShell
    ? `/connections/${connId}/guests/${guestType}/${node}/${vmid}/shell`
    : kind === "shell"
      ? `/connections/${connId}/nodes/${node}/shell`
      : `/connections/${connId}/guests/${guestType}/${node}/${vmid}/console`
  return api.post<{ wsPath: string; password?: string }>(path)
}

export function ConsolePage() {
  const [params] = useSearchParams()
  const kind = params.get("kind") ?? "vnc"
  const name = params.get("name") ?? "Console"
  // connId/type/node/vmid let us mint a fresh session on reconnect, since
  // each session token from the backend is single-use.
  const connId = params.get("connId")
  const guestType = params.get("type")
  const node = params.get("node")
  const vmid = params.get("vmid")
  // SSH sessions never reconnect silently — there is no stored Proxmox
  // connection to remint a ticket from, and the password isn't kept around
  // after the initial handoff. Reconnecting means reopening the SSH dialog.
  const canReconnect = kind === "ssh" ? false : kind === "shell" ? Boolean(connId && node) : Boolean(connId && guestType && node && vmid)

  const containerRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<import("@novnc/novnc/lib/rfb.js").default | null>(null)
  // PVE doubles the VNC ticket as the RFB-level password; kept in a ref so
  // the credentialsrequired listener always answers with the current ticket.
  const passwordRef = useRef<string | null>(null)
  // The opener hands the ticketed ws path (and the VNC password) over via
  // postMessage once this popup announces itself — see openConsolePopup in
  // lib/console.ts — so they never travel in this popup's URL.
  const [hadOpener] = useState(() => Boolean(window.opener))
  const [handoff, setHandoff] = useState<ConsoleHandoff | null>(null)
  // An opener that never answers (its listener is gone — the classic case is
  // a popup refresh, which keeps window.opener across the reload in
  // Chromium/Firefox) must not strand this page on "connecting": after the
  // deadline we fall through to the same disconnected/error states a direct
  // load gets. Tickets are single-use, so the escape is Reconnect.
  const [handoffTimedOut, setHandoffTimedOut] = useState(false)
  const [state, setState] = useState<ConnectionState>(() => {
    if (hadOpener) return "connecting"
    return canReconnect ? "disconnected" : "error"
  })
  const [errorMsg, setErrorMsg] = useState(() => (!hadOpener && !canReconnect ? missingSessionMessage(kind) : ""))
  const [attempt, setAttempt] = useState(0)
  const handoffWsPath = handoff?.wsPath ?? null
  // The first attempt's ticket situation: "handoff-waiting" while the
  // opener's postMessage handoff is still pending, "dead" when there is no
  // opener — or the opener stopped answering and the handoff timed out —
  // and "ready" once a ticket is in hand or a later attempt will mint its
  // own fresh session.
  const firstAttempt: "handoff-waiting" | "dead" | "ready" =
    attempt !== 0 || handoffWsPath ? "ready" : hadOpener && !handoffTimedOut ? "handoff-waiting" : "dead"

  const reconnect = useCallback(() => setAttempt((a) => a + 1), [])

  // Popup side of the ticket handoff: announce readiness, then receive the
  // ticketed ws path/password from the opener. The opener only posts the
  // params after this ready signal, so the listener is guaranteed to be
  // attached before they arrive (and StrictMode's dev double-mount is fine —
  // the remount re-announces before the opener's async reply lands). If no
  // params arrive within the deadline the opener is gone (popup refresh) —
  // drop to the disconnected/error states instead of spinning forever.
  useEffect(() => {
    const opener = window.opener as Window | null
    if (!opener) return
    opener.postMessage({ type: CONSOLE_READY_MESSAGE }, window.location.origin)
    let settled = false
    const onMessage = (evt: MessageEvent) => {
      if (evt.origin !== window.location.origin) return
      const ticket = parseConsoleParams(evt.data)
      if (!ticket) return
      settled = true
      window.removeEventListener("message", onMessage)
      passwordRef.current = ticket.password ?? null
      setHandoff(ticket)
    }
    window.addEventListener("message", onMessage)
    const timeout = window.setTimeout(() => {
      if (settled) return
      settled = true
      window.removeEventListener("message", onMessage)
      setHandoffTimedOut(true)
      if (canReconnect) {
        setState("disconnected")
      } else {
        setState("error")
        setErrorMsg(missingSessionMessage(kind))
      }
    }, HANDOFF_TIMEOUT_MS)
    return () => {
      window.clearTimeout(timeout)
      window.removeEventListener("message", onMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!containerRef.current || kind !== "vnc") return
    // First attempt: with the handoff still pending there is nothing to
    // connect with yet, and the "dead" case (no opener) already landed on
    // its terminal state above — connecting here would either race the
    // handed-off ticket or mint a session the user never asked for.
    if (firstAttempt !== "ready") return

    let cancelled = false
    let rfb: import("@novnc/novnc/lib/rfb.js").default | null = null
    setState("connecting")
    setErrorMsg("")

    async function connect() {
      let wsPath = attempt === 0 ? handoffWsPath : null
      if (!wsPath) {
        if (!connId || !guestType || !node || !vmid) {
          if (!cancelled) {
            setState("error")
            setErrorMsg("Missing console session — open this from a guest's console button.")
          }
          return
        }
        const res = await openSession(kind, connId, guestType, node, vmid)
        wsPath = res.wsPath
        passwordRef.current = res.password ?? null
      }
      if (cancelled || !containerRef.current) return

      const RFB = await loadRFB()
      if (cancelled || !containerRef.current) return

      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
      const url = `${protocol}//${window.location.host}${wsPath}`

      rfb = new RFB(containerRef.current, url)
      rfb.scaleViewport = true
      rfb.resizeSession = true
      rfbRef.current = rfb

      rfb.addEventListener("connect", () => !cancelled && setState("connected"))
      rfb.addEventListener("disconnect", () => !cancelled && setState("disconnected"))
      // PVE's VNC endpoint challenges for the ticket as the VNC password.
      rfb.addEventListener("credentialsrequired", () => {
        if (cancelled) return
        const password = passwordRef.current
        if (password) {
          rfb?.sendCredentials({ password })
          return
        }
        setState("error")
        setErrorMsg("Proxmox asked for console credentials this session doesn't have — reopen the console.")
      })
      rfb.addEventListener("securityfailure", (e: Event) => {
        if (!cancelled) {
          setState("error")
          setErrorMsg((e as CustomEvent).detail?.reason ?? "Security negotiation with the console failed.")
        }
      })
    }

    connect().catch((err: unknown) => {
      if (!cancelled) {
        setState("error")
        setErrorMsg(err instanceof Error ? err.message : "Failed to open console session.")
      }
    })

    return () => {
      cancelled = true
      rfb?.disconnect()
      rfbRef.current = null
    }
  }, [kind, firstAttempt, attempt, handoffWsPath, connId, guestType, node, vmid])

  function ctrlAltDel() {
    rfbRef.current?.sendCtrlAltDel()
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      document.exitFullscreen()
    } else {
      containerRef.current?.requestFullscreen()
    }
  }

  return (
    <div className="flex h-screen w-screen flex-col bg-black">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b border-white/10 bg-[#111] px-3 text-sm text-white/80">
        {state === "connecting" && <Loader2 className="h-3.5 w-3.5 animate-spin text-brand-400" />}
        {state === "connected" && <Wifi className="h-3.5 w-3.5 text-[var(--status-ok)]" />}
        {(state === "disconnected" || state === "error") && <WifiOff className="h-3.5 w-3.5 text-[var(--status-error)]" />}
        <span className="font-medium">{name}</span>
        <span className="text-xs text-white/40">
          {state === "connecting" && "Connecting..."}
          {state === "connected" && "Connected"}
          {state === "disconnected" && "Disconnected"}
          {state === "error" && "Error"}
        </span>
        <div className="ml-auto flex items-center gap-1">
          {kind === "vnc" && (
            <Button size="sm" variant="ghost" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={ctrlAltDel} disabled={state !== "connected"}>
              <Keyboard className="h-3.5 w-3.5" /> Ctrl+Alt+Del
            </Button>
          )}
          <Button size="icon" variant="ghost" aria-label="Toggle fullscreen" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={toggleFullscreen}>
            <Maximize className="h-3.5 w-3.5" />
          </Button>
          <Button size="icon" variant="ghost" aria-label="Close console" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={() => window.close()}>
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1">
        {kind === "shell" || kind === "ssh" ? (
          <ShellTerminal
            initialWsPath={attempt === 0 ? handoffWsPath : null}
            reconnectKey={attempt}
            mintSession={kind === "shell" && canReconnect ? () => openSession(kind, connId!, guestType, node!, vmid) : null}
            missingMessage={missingSessionMessage(kind)}
            onState={setState}
            onError={setErrorMsg}
          />
        ) : (
          <div ref={containerRef} className="h-full w-full" />
        )}

        {state === "error" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black text-center text-white">
            <AlertTriangle className="h-8 w-8 text-[var(--status-error)]" />
            <p className="max-w-sm text-sm text-white/70">{errorMsg}</p>
            {canReconnect && (
              <Button size="sm" variant="secondary" onClick={reconnect}>
                <RotateCcw className="h-3.5 w-3.5" /> Try again
              </Button>
            )}
          </div>
        )}
        {state === "disconnected" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black/80 text-center text-white">
            <WifiOff className="h-8 w-8 text-white/40" />
            <p className="text-sm text-white/70">Console session ended.</p>
            <Button size="sm" variant="secondary" onClick={reconnect}>
              <RotateCcw className="h-3.5 w-3.5" /> Reconnect
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

// ShellTerminal renders an xterm.js terminal over the same single-use
// websocket hand-off the VNC path uses (openGuestShell/openNodeShell mint a
// termproxy ticket instead of a VNC one; the backend proxies both through
// the same byte-relay). PVE's own console.js frames terminal I/O as
// UTF-8 text passed straight through the socket — no separate handshake once
// the websocket is authenticated by the ticket already embedded server-side.
function ShellTerminal({
  initialWsPath,
  reconnectKey,
  mintSession,
  missingMessage,
  onState,
  onError,
}: {
  initialWsPath: string | null
  reconnectKey: number
  mintSession: (() => Promise<{ wsPath: string }>) | null
  missingMessage: string
  onState: (s: ConnectionState) => void
  onError: (msg: string) => void
}) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!containerRef.current) return
    // First attempt with no ticket in hand: while the opener's postMessage
    // handoff is pending there is nothing to connect with yet, and once the
    // handoff times out (or with no opener at all — tickets are single-use,
    // so a refresh intentionally requires a new console session) the parent
    // lands on its disconnected/"Reconnect" state. Connecting here would
    // mint a session the user never asked for.
    if (reconnectKey === 0 && !initialWsPath) return
    let cancelled = false
    let socket: WebSocket | null = null
    let disposeTerm: (() => void) | null = null
    onState("connecting")

    async function connect() {
      const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")])
      await import("@xterm/xterm/css/xterm.css")
      if (cancelled || !containerRef.current) return

      let wsPath = reconnectKey === 0 ? initialWsPath : null
      if (!wsPath) {
        if (!mintSession) {
          if (!cancelled) {
            onState("error")
            onError(missingMessage)
          }
          return
        }
        wsPath = (await mintSession()).wsPath
      }
      if (cancelled || !containerRef.current) return

      const term = new Terminal({ cursorBlink: true, theme: { background: "#000000" }, fontSize: 13 })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(containerRef.current)
      fit.fit()
      const onResize = () => fit.fit()
      window.addEventListener("resize", onResize)

      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
      socket = new WebSocket(`${protocol}//${window.location.host}${wsPath}`)
      socket.binaryType = "arraybuffer"

      socket.addEventListener("open", () => !cancelled && onState("connected"))
      socket.addEventListener("close", () => !cancelled && onState("disconnected"))
      socket.addEventListener("error", () => {
        if (!cancelled) {
          onState("error")
          onError("Shell connection failed.")
        }
      })
      socket.addEventListener("message", (ev) => {
        // Raw bytes, not a per-frame TextDecoder: xterm keeps its own UTF-8
        // decoder state, so a multi-byte char split across frames stays intact.
        term.write(ev.data instanceof ArrayBuffer ? new Uint8Array(ev.data) : String(ev.data))
      })
      term.onData((data) => {
        if (socket?.readyState === WebSocket.OPEN) socket.send(data)
      })

      disposeTerm = () => {
        window.removeEventListener("resize", onResize)
        term.dispose()
      }
    }

    connect().catch((err: unknown) => {
      if (!cancelled) {
        onState("error")
        onError(err instanceof Error ? err.message : "Failed to open shell session.")
      }
    })

    return () => {
      cancelled = true
      socket?.close()
      disposeTerm?.()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reconnectKey, initialWsPath])

  return <div ref={containerRef} className="h-full w-full p-1" />
}
