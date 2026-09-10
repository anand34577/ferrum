// Package events is a tiny in-process publish/subscribe hub for server-side
// state changes (alert triggers, connection health flips, and anything else
// wired up later). It has two consumers: the SSE endpoint (internal/api) that
// forwards events to logged-in browser sessions, and the outgoing webhook
// dispatcher (internal/notify) that fans them out to subscribed URLs.
//
// Deliberately just channels + a mutex — no external broker, no persistence.
// An event published while nobody is subscribed, or faster than a slow
// subscriber can drain, is simply lost; every consumer here treats the
// stream as best-effort, same as the poller's own periodic re-evaluation.
package events

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Type names the kind of state change an Event describes. New producers
// should add a constant here rather than sprinkling string literals.
type Type string

const (
	TypeAlertTriggered    Type = "alert.triggered"
	TypeAlertResolved     Type = "alert.resolved"
	TypeConnectionUp      Type = "connection.up"
	TypeConnectionDown    Type = "connection.down"
	TypeGuestPowerChanged Type = "guest.power_changed"
	TypeTaskCompleted     Type = "task.completed"
	TypeHAStatusChanged   Type = "ha.status_changed"
	TypeTest              Type = "test" // synthetic event used by the webhook "send test event" action
)

// Event is the structured payload broadcast to every subscriber and, from
// there, to SSE clients and webhook subscriptions. Payload carries the
// type-specific details (already JSON-serializable — a map[string]any or a
// concrete struct) so subscribers don't need to know about internal types.
type Event struct {
	ID           string    `json:"id"`
	Type         Type      `json:"type"`
	ConnectionID string    `json:"connectionId,omitempty"`
	ResourceID   string    `json:"resourceId,omitempty"`
	Payload      any       `json:"payload,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// subBufferSize bounds how far a subscriber can lag behind before it starts
// losing events rather than blocking Publish — Publish is called from
// synchronous poller/handler code paths and must never stall on a slow
// reader.
const subBufferSize = 64

// Bus is safe for concurrent use. The zero value is not usable; construct
// with New.
type Bus struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

func New() *Bus {
	return &Bus{subs: map[int]chan Event{}}
}

// Subscribe registers a new listener and returns a receive-only channel of
// events plus an Unsubscribe func the caller must call (typically via
// defer) once it stops reading, to release the channel and stop it filling
// up unread.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subBufferSize)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing)
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

// Publish broadcasts an event to every current subscriber. ID and Timestamp
// are filled in if the caller left them zero. Non-blocking: a subscriber
// whose buffer is full simply misses this event rather than stalling every
// other subscriber (and the publisher).
func (b *Bus) Publish(evt Event) {
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- evt:
		default:
			// Slow/stuck subscriber — drop rather than block. The SSE
			// handler and webhook dispatcher each drain their own channel
			// promptly, so this only triggers under real backpressure.
		}
	}
}

// SubscriberCount reports how many listeners are currently attached —
// useful for diagnostics/tests only.
func (b *Bus) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
