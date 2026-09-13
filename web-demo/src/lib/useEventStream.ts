import { useEffect, useRef, useState } from "react"

/**
 * Mirrors the server's internal/events.Event (see internal/api/events.go) —
 * keep the field names in sync with that struct's JSON tags.
 */
export interface FerrumEvent {
  id: string
  type: string
  connectionId?: string
  resourceId?: string
  payload?: unknown
  timestamp: string
}

export type EventStreamStatus = "connecting" | "open" | "closed"

export interface UseEventStreamOptions {
  /** Restrict delivery to these event types only (client-side filter — the
   * server always sends everything on the bus to every subscriber). Omit to
   * receive every event type. */
  types?: string[]
  /** Cap on how many events are kept in `events` before the oldest are
   * dropped. Default 100. Pass 0 to disable buffering and rely solely on
   * `onEvent`. */
  maxBuffered?: number
  /** Called for every event as it arrives, in addition to the buffered
   * `events` list — handy for a reducer/store rather than local state. */
  onEvent?: (evt: FerrumEvent) => void
  /** Set to false to not connect at all (e.g. while a page that doesn't
   * need live updates is mounted). Default true. */
  enabled?: boolean
}

export interface UseEventStreamResult {
  /** Connection lifecycle: "connecting" while dialing or reconnecting after
   * a drop, "open" once the stream is live, "closed" only when disabled. */
  status: EventStreamStatus
  /** The most recently buffered events, oldest first, capped at
   * maxBuffered. */
  events: FerrumEvent[]
  /** Clears the buffered events list without affecting the connection. */
  clear: () => void
}

const DEFAULT_MAX_BUFFERED = 100
// Reconnect backoff: starts at 1s, doubles up to a 30s ceiling, with jitter
// so many tabs reconnecting after a server restart don't all hammer it at
// once.
const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30000

/**
 * Subscribes to the server's live event stream (GET /api/v1/events, SSE —
 * see internal/api/events.go) via EventSource, reconnecting automatically
 * with exponential backoff if the connection drops (which it will
 * periodically, since the stream sits behind the same request-timeout
 * middleware as the rest of the API — this is expected, not an error).
 */
export function useEventStream(options: UseEventStreamOptions = {}): UseEventStreamResult {
  const { types, maxBuffered = DEFAULT_MAX_BUFFERED, onEvent, enabled = true } = options
  const [status, setStatus] = useState<EventStreamStatus>("connecting")
  const [events, setEvents] = useState<FerrumEvent[]>([])

  // Keep the latest callback/filter in refs so the connect effect below
  // doesn't need them in its dependency array — reconnecting the whole
  // EventSource just because the caller passed a new inline onEvent
  // function on every render would be wasteful and would fight the backoff
  // timer.
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent
  const typesRef = useRef(types)
  typesRef.current = types

  useEffect(() => {
    if (!enabled) {
      setStatus("closed")
      return
    }

    let source: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let stopped = false

    const connect = () => {
      if (stopped) return
      setStatus("connecting")
      source = new EventSource("/api/v1/events", { withCredentials: true })

      source.onopen = () => {
        attempt = 0
        setStatus("open")
      }

      source.onmessage = (msg: MessageEvent<string>) => {
        let evt: FerrumEvent
        try {
          evt = JSON.parse(msg.data) as FerrumEvent
        } catch {
          return // malformed payload — drop rather than crash the stream
        }
        const filter = typesRef.current
        if (filter && filter.length > 0 && !filter.includes(evt.type)) return

        onEventRef.current?.(evt)
        if (maxBuffered > 0) {
          setEvents((prev) => {
            const next = [...prev, evt]
            return next.length > maxBuffered ? next.slice(next.length - maxBuffered) : next
          })
        }
      }

      source.onerror = () => {
        // EventSource retries transport errors on its own, but the server's
        // stream also ends deliberately every so often (request-timeout
        // middleware); in either case treat "not open" as needing our own
        // backoff+reconnect rather than trusting the browser's built-in
        // (unbounded, no-backoff) retry.
        source?.close()
        setStatus("connecting")
        if (stopped) return
        const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** attempt) * (0.75 + Math.random() * 0.5)
        attempt += 1
        reconnectTimer = setTimeout(connect, delay)
      }
    }

    connect()

    return () => {
      stopped = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      source?.close()
    }
  }, [enabled, maxBuffered])

  return {
    status,
    events,
    clear: () => setEvents([]),
  }
}
