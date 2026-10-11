import { useEffect, useRef, useSyncExternalStore } from "react"
import { useEventStream, type FerrumEvent, type EventStreamStatus } from "@/lib/useEventStream"

/**
 * The app's single live-event connection.
 *
 * `useEventStream` is per-component: every caller would open its own
 * EventSource. Two consumers need the stream today — the header's telemetry
 * pill (status) and the notification bell (alert events) — so this module
 * lets one owner mount <SseConnection /> and everyone else subscribe to the
 * shared status/event fan-out without extra connections.
 */

let status: EventStreamStatus = "connecting"
const statusListeners = new Set<() => void>()
const eventListeners = new Set<(evt: FerrumEvent) => void>()

function setStatus(next: EventStreamStatus) {
  if (next === status) return
  status = next
  statusListeners.forEach((l) => l())
}

/** Mirrors the shared connection's lifecycle without opening a stream. */
export function useSseStatus(): EventStreamStatus {
  return useSyncExternalStore(
    (cb) => {
      statusListeners.add(cb)
      return () => statusListeners.delete(cb)
    },
    () => status,
    () => status,
  )
}

/** Receives events of the given types (all types when omitted) from the
 * shared connection. The handler may change every render; it is kept in a
 * ref so subscribing never reconnects anything. */
export function useSseEvents(types: string[] | undefined, onEvent: (evt: FerrumEvent) => void) {
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent
  const typesRef = useRef(types)
  typesRef.current = types

  useEffect(() => {
    const listener = (evt: FerrumEvent) => {
      const filter = typesRef.current
      if (filter && filter.length > 0 && !filter.includes(evt.type)) return
      onEventRef.current(evt)
    }
    eventListeners.add(listener)
    return () => {
      eventListeners.delete(listener)
    }
  }, [])
}

/** Mount once (AppShell) to own the EventSource. Renders nothing. */
export function SseConnection() {
  useEventStream({
    maxBuffered: 0,
    onStatusChange: setStatus,
    onEvent: (evt) => eventListeners.forEach((fn) => fn(evt)),
  })
  return null
}
