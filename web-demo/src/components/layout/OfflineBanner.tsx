import { useEffect, useRef } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Loader2, WifiOff } from "lucide-react"
import { useOnlineStatus } from "@/lib/useOnline"

/**
 * Global offline banner. Until this existed, a dropped network surfaced only
 * as scattered per-widget errors — every query failing on its own cadence —
 * with no single honest "you're offline" state. One slim strip above the
 * header fixes that; on reconnect it invalidates every query so the
 * dashboard refills immediately instead of waiting out the poll intervals.
 */
export function OfflineBanner() {
  const online = useOnlineStatus()
  const queryClient = useQueryClient()
  const wasOffline = useRef(false)

  useEffect(() => {
    if (!online) {
      wasOffline.current = true
      return
    }
    if (!wasOffline.current) return
    wasOffline.current = false
    // Back online: refetch everything that isn't fresh rather than trusting
    // stale-while-reconnecting data after an unbounded offline window.
    void queryClient.invalidateQueries()
  }, [online, queryClient])

  if (online) return null

  return (
    <div
      className="flex h-7 shrink-0 items-center justify-center gap-2 bg-[var(--status-warn)] px-4 text-[11px] font-medium text-black"
      role="status"
    >
      <WifiOff className="h-3.5 w-3.5 shrink-0" aria-hidden />
      <span>You're offline — data shown may be stale, reconnecting…</span>
      <Loader2 className="h-3 w-3 shrink-0 animate-spin" aria-hidden />
    </div>
  )
}
