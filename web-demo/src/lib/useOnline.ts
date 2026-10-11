import { useEffect, useState } from "react"

/**
 * Tracks the browser's network reachability. `navigator.onLine` is a
 * heuristic (it can't see a captive portal or a dropped uplink behind a
 * live NIC), but the events fire exactly when a real connectivity change
 * happens, which is all the offline banner needs.
 */
export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState(() => (typeof navigator === "undefined" ? true : navigator.onLine))

  useEffect(() => {
    const goOnline = () => setOnline(true)
    const goOffline = () => setOnline(false)
    window.addEventListener("online", goOnline)
    window.addEventListener("offline", goOffline)
    return () => {
      window.removeEventListener("online", goOnline)
      window.removeEventListener("offline", goOffline)
    }
  }, [])

  return online
}
