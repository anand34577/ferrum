import { AlertTriangle, Keyboard, Loader2, Maximize, RotateCcw, Wifi, WifiOff, X } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"

type ConnectionState = "connecting" | "connected" | "disconnected" | "error"

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

export function ConsolePage() {
  const [params] = useSearchParams()
  const initialWsPath = params.get("ws")
  const initialPassword = params.get("pw")
  const name = params.get("name") ?? "Console"
  // connId/type/node/vmid let us mint a fresh console session on reconnect,
  // since each session token from the backend is single-use.
  const connId = params.get("connId")
  const guestType = params.get("type")
  const node = params.get("node")
  const vmid = params.get("vmid")

  const containerRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<import("@novnc/novnc/lib/rfb.js").default | null>(null)
  // PVE doubles the VNC ticket as the RFB-level password; kept in a ref so
  // the credentialsrequired listener always answers with the current ticket.
  const passwordRef = useRef<string | null>(initialPassword)
  const [state, setState] = useState<ConnectionState>("connecting")
  const [errorMsg, setErrorMsg] = useState("")
  const [attempt, setAttempt] = useState(0)

  const reconnect = useCallback(() => setAttempt((a) => a + 1), [])

  useEffect(() => {
    if (!containerRef.current) return

    let cancelled = false
    let rfb: import("@novnc/novnc/lib/rfb.js").default | null = null
    setState("connecting")
    setErrorMsg("")

    async function connect() {
      let wsPath = attempt === 0 ? initialWsPath : null
      if (!wsPath) {
        if (!connId || !guestType || !node || !vmid) {
          if (!cancelled) {
            setState("error")
            setErrorMsg("Missing console session — open this from a guest's console button.")
          }
          return
        }
        const res = await api.post<{ wsPath: string; password?: string }>(`/connections/${connId}/guests/${guestType}/${node}/${vmid}/console`)
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
  }, [attempt, initialWsPath, connId, guestType, node, vmid])

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
        {state === "connected" && <Wifi className="h-3.5 w-3.5 text-green-500" />}
        {(state === "disconnected" || state === "error") && <WifiOff className="h-3.5 w-3.5 text-red-500" />}
        <span className="font-medium">{name}</span>
        <span className="text-xs text-white/40">
          {state === "connecting" && "Connecting..."}
          {state === "connected" && "Connected"}
          {state === "disconnected" && "Disconnected"}
          {state === "error" && "Error"}
        </span>
        <div className="ml-auto flex items-center gap-1">
          <Button size="sm" variant="ghost" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={ctrlAltDel} disabled={state !== "connected"}>
            <Keyboard className="h-3.5 w-3.5" /> Ctrl+Alt+Del
          </Button>
          <Button size="icon" variant="ghost" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={toggleFullscreen}>
            <Maximize className="h-3.5 w-3.5" />
          </Button>
          <Button size="icon" variant="ghost" className="text-white/70 hover:bg-white/10 hover:text-white" onClick={() => window.close()}>
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <div className="relative min-h-0 flex-1">
        <div ref={containerRef} className="h-full w-full" />

        {state === "error" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black text-center text-white">
            <AlertTriangle className="h-8 w-8 text-red-500" />
            <p className="max-w-sm text-sm text-white/70">{errorMsg}</p>
            {(connId && guestType && node && vmid) && (
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
