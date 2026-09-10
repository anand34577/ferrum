package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/events"
	"ferrum/internal/store"
)

// WebhookSubscription is one row of webhook_subscriptions — a third-party
// URL that wants a signed POST for a subset (or all) of the events on
// internal/events.Bus.
type WebhookSubscription struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"secret,omitempty"` // omitted from list responses — see internal/api/webhooks.go
	EventTypes []string  `json:"eventTypes"`       // empty means "all event types"
	Active     bool      `json:"active"`
	CreatedBy  string    `json:"createdBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// WebhookDelivery is one row of webhook_deliveries — a single attempt to
// deliver one event to one subscription.
type WebhookDelivery struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscriptionId"`
	EventType      string    `json:"eventType"`
	EventID        string    `json:"eventId"`
	Attempt        int       `json:"attempt"`
	StatusCode     *int      `json:"statusCode,omitempty"`
	Error          string    `json:"error,omitempty"`
	Success        bool      `json:"success"`
	CreatedAt      time.Time `json:"createdAt"`
}

const (
	// SignatureHeader carries the hex-encoded HMAC-SHA256 of the raw request
	// body, keyed by the subscription's secret — receivers verify it the
	// same way GitHub/Stripe-style webhooks do.
	SignatureHeader = "X-Ferrum-Signature"
	// EventTypeHeader/EventIDHeader let a receiver dispatch without parsing
	// the body first.
	EventTypeHeader = "X-Ferrum-Event"
	EventIDHeader   = "X-Ferrum-Delivery"

	webhookTimeout          = 10 * time.Second
	webhookMaxAttempts      = 4
	webhookRetryBaseDelay   = 2 * time.Second
	deliveryLogRetainedRows = 50 // per subscription — see pruneDeliveries
)

// WebhookDispatcher subscribes to an events.Bus and fans each event out to
// every active, matching subscription as a signed POST, retrying with
// backoff and recording every attempt for the delivery log. Mirrors
// Notifier's shape (own HTTP client, safe for concurrent use) but reads its
// subscription config from the database on every event rather than caching
// it, since subscriptions are expected to change far less often than events
// fire and this keeps CRUD trivially consistent.
type WebhookDispatcher struct {
	db     *store.DB
	client *http.Client
}

func NewWebhookDispatcher(db *store.DB) *WebhookDispatcher {
	return &WebhookDispatcher{db: db, client: &http.Client{Timeout: webhookTimeout}}
}

// Run subscribes to bus and delivers every event until ctx is cancelled.
// Intended to be started once at boot in its own goroutine, same as
// poller.AlertEvaluator.Run.
func (d *WebhookDispatcher) Run(ctx context.Context, bus *events.Bus) {
	ch, unsubscribe := bus.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			// Delivery involves network round trips and retry sleeps; run it
			// off the receive loop so one slow/unreachable webhook can't
			// delay delivery to the rest, or cause this subscriber's bus
			// buffer to fill and start dropping events.
			go d.deliverToAll(context.Background(), evt)
		}
	}
}

func (d *WebhookDispatcher) deliverToAll(ctx context.Context, evt events.Event) {
	subs, err := d.matchingSubscriptions(ctx, evt.Type)
	if err != nil {
		slog.Error("webhook dispatcher: loading subscriptions failed", "error", err)
		return
	}
	body, err := json.Marshal(evt)
	if err != nil {
		slog.Error("webhook dispatcher: marshaling event failed", "error", err)
		return
	}
	for _, sub := range subs {
		go d.deliverWithRetry(ctx, sub, evt, body)
	}
}

func (d *WebhookDispatcher) matchingSubscriptions(ctx context.Context, evtType events.Type) ([]WebhookSubscription, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, name, url, secret, event_types, active FROM webhook_subscriptions WHERE active = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		var eventTypesJSON string
		var active int
		if err := rows.Scan(&sub.ID, &sub.Name, &sub.URL, &sub.Secret, &eventTypesJSON, &active); err != nil {
			return nil, err
		}
		sub.Active = active == 1
		var types []string
		if err := json.Unmarshal([]byte(eventTypesJSON), &types); err != nil {
			slog.Warn("webhook dispatcher: subscription has malformed event_types, skipping", "subscriptionId", sub.ID, "error", err)
			continue
		}
		sub.EventTypes = types
		if subscriptionMatches(types, evtType) {
			out = append(out, sub)
		}
	}
	return out, rows.Err()
}

func subscriptionMatches(subscribed []string, evtType events.Type) bool {
	if len(subscribed) == 0 {
		return true // no filter configured = every event type
	}
	for _, t := range subscribed {
		if t == string(evtType) {
			return true
		}
	}
	return false
}

// deliverWithRetry POSTs body to sub.URL, retrying on failure (non-2xx
// response or transport error) with exponential backoff up to
// webhookMaxAttempts, logging every attempt to webhook_deliveries.
func (d *WebhookDispatcher) deliverWithRetry(ctx context.Context, sub WebhookSubscription, evt events.Event, body []byte) {
	delay := webhookRetryBaseDelay
	for attempt := 1; attempt <= webhookMaxAttempts; attempt++ {
		statusCode, err := d.deliverOnce(ctx, sub, evt, body)
		success := err == nil && statusCode >= 200 && statusCode < 300
		d.recordDelivery(ctx, sub.ID, evt, attempt, statusCode, err, success)
		if success {
			return
		}
		if attempt == webhookMaxAttempts {
			slog.Warn("webhook delivery exhausted retries", "subscriptionId", sub.ID, "url", sub.URL, "event", evt.Type, "attempts", attempt)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// deliverOnce makes one HTTP attempt. statusCode is -1 (reported as nil in
// the delivery log) when the request never got a response at all.
func (d *WebhookDispatcher) deliverOnce(ctx context.Context, sub WebhookSubscription, evt events.Event, body []byte) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, sub.URL, bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, signBody(sub.Secret, body))
	req.Header.Set(EventTypeHeader, string(evt.Type))
	req.Header.Set(EventIDHeader, evt.ID)

	resp, err := d.client.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, errors.New(http.StatusText(resp.StatusCode))
	}
	return resp.StatusCode, nil
}

// signBody computes the hex HMAC-SHA256 of body keyed by secret — receivers
// recompute the same digest over the raw bytes they read to verify
// authenticity, exactly as documented for X-Ferrum-Signature.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (d *WebhookDispatcher) recordDelivery(ctx context.Context, subscriptionID string, evt events.Event, attempt int, statusCode int, deliveryErr error, success bool) {
	var statusCodeCol *int
	if statusCode >= 0 {
		statusCodeCol = &statusCode
	}
	var errCol *string
	if deliveryErr != nil {
		msg := deliveryErr.Error()
		errCol = &msg
	}
	successCol := 0
	if success {
		successCol = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (id, subscription_id, event_type, event_id, attempt, status_code, error, success, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), subscriptionID, string(evt.Type), evt.ID, attempt, statusCodeCol, errCol, successCol, now,
	); err != nil {
		slog.Error("webhook dispatcher: recording delivery failed", "subscriptionId", subscriptionID, "error", err)
		return
	}
	d.pruneDeliveries(ctx, subscriptionID)
}

// pruneDeliveries keeps the delivery log bounded per subscription — best
// effort, a failure here just means the log grows a bit, never a reason to
// fail the delivery itself.
func (d *WebhookDispatcher) pruneDeliveries(ctx context.Context, subscriptionID string) {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM webhook_deliveries WHERE subscription_id = ? AND id NOT IN (
			SELECT id FROM webhook_deliveries WHERE subscription_id = ? ORDER BY created_at DESC LIMIT ?
		)`, subscriptionID, subscriptionID, deliveryLogRetainedRows)
	if err != nil {
		slog.Error("webhook dispatcher: pruning delivery log failed", "subscriptionId", subscriptionID, "error", err)
	}
}

// SendTestEvent synthesizes an events.TypeTest event and delivers it to a
// single subscription immediately (bypassing the event-type filter — a test
// send should always go through), for the "send test event" admin action.
// It runs the delivery synchronously (including retries) so the HTTP
// handler can report the final outcome.
func (d *WebhookDispatcher) SendTestEvent(ctx context.Context, sub WebhookSubscription) error {
	evt := events.Event{
		ID:        uuid.NewString(),
		Type:      events.TypeTest,
		Timestamp: time.Now().UTC(),
		Payload:   map[string]any{"message": "This is a test event from Ferrum."},
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	statusCode, err := d.deliverOnce(ctx, sub, evt, body)
	success := err == nil && statusCode >= 200 && statusCode < 300
	d.recordDelivery(ctx, sub.ID, evt, 1, statusCode, err, success)
	if !success {
		if err != nil {
			return err
		}
		return errors.New(http.StatusText(statusCode))
	}
	return nil
}
