import { useEffect, useRef, useState } from "react"
import type { GuestLiveStatus } from "@/lib/api"

export interface LiveRates {
  netin?: number
  netout?: number
  diskread?: number
  diskwrite?: number
}

/**
 * Derives bytes/sec rates from a guest's CUMULATIVE status counters
 * (netin/netout/diskread/diskwrite grow since guest start — PVE only exposes
 * rates for its own RRD graphs). Each new poll is diffed against the
 * previous one. If the counter goes backwards (guest restarted) the rate is
 * skipped for one sample instead of reporting nonsense.
 */
export function useLiveRates(status: GuestLiveStatus | undefined): LiveRates {
  const prev = useRef<{ t: number; netin: number; netout: number; diskread: number; diskwrite: number } | null>(null)
  const [rates, setRates] = useState<LiveRates>({})

  useEffect(() => {
    if (!status || status.status !== "running") {
      prev.current = null
      setRates({})
      return
    }
    const now = Date.now() / 1000
    const p = prev.current
    const cur = {
      netin: status.netin ?? 0,
      netout: status.netout ?? 0,
      diskread: status.diskread ?? 0,
      diskwrite: status.diskwrite ?? 0,
    }
    if (p && now > p.t + 0.5) {
      const dt = now - p.t
      const rate = (c: number, last: number) => (c >= last ? (c - last) / dt : undefined)
      setRates({
        netin: rate(cur.netin, p.netin),
        netout: rate(cur.netout, p.netout),
        diskread: rate(cur.diskread, p.diskread),
        diskwrite: rate(cur.diskwrite, p.diskwrite),
      })
    }
    prev.current = { t: now, ...cur }
  }, [status])

  return rates
}
