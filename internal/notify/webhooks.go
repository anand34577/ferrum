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
	"sync"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/events"
	"ferrum/internal/secrets"
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
	webhookMaxBackoff       = time.Hour
	webhookOutboxRetention  = 24 * time.Hour  // how long an undeliverable event stays queued
	webhookSweepInterval    = 5 * time.Minute // how often queued events are re-attempted
	deliveryLogRetainedRows = 50              // per subscription — see pruneDeliveries
)

// WebhookDispatcher subscribes to an events.Bus and fans each event out to
// every active, matching subscription as a signed POST, retrying with
// backoff and recording every attempt for the delivery log. Mirrors
// Notifier's shape (own HTTP client, safe for concurrent use) but reads its
// subscription config from the database on every event rather than caching
// it, since subscriptions are expected to change far less often than events
// fire and this keeps CRUD trivially consistent.
type WebhookDispatcher struct {
	db      *store.DB
	secrets *secrets.Box // nil = signing secrets are stored/used as plaintext
	client  *http.Client

	mu       sync.Mutex
	inFlight map[string]bool // outbox keys ("eventId|subscriptionId") currently being delivered — see dispatchOutboxRow
}

func NewWebhookDispatcher(db *store.DB, box *secrets.Box) *WebhookDispatcher {
	return &WebhookDispatcher{db: db, secrets: box, client: &http.Client{Timeout: webhookTimeout}, inFlight: map[string]bool{}}
}

// outboxRow is one row of webhook_outbox: an (event, subscription) pair
// awaiting delivery (or mid-delivery).
type outboxRow struct {
	EventID        string
	SubscriptionID string
	EventType      string
	Payload        string
	Attempts       int
}

// Run subscribes to bus and delivers every event until ctx is cancelled.
// Delivery is at-least-once: each (event, subscription) pair is persisted
// to webhook_outbox before the first attempt and its row is deleted only
// once the receiver accepts the event, so anything queued (or mid-retry)
// when the process shuts down is picked up again after restart by the
// periodic sweep. The flip side of durability: a crash between the
// receiver's 2xx and the outbox delete can redeliver the event — receivers
// must dedupe on the X-Ferrum-Delivery header, which is stable per event.
// Intended to be started once at boot in its own goroutine, same as
// poller.AlertEvaluator.Run.
func (d *WebhookDispatcher) Run(ctx context.Context, bus *events.Bus) {
	// Queue sweeper: drains anything the previous run left behind, then
	// keeps re-attempting due rows. Runs on its own goroutine so a slow
	// receiver's retry sleeps never delay live event delivery.
	go func() {
		d.sweep(ctx)
		ticker := time.NewTicker(webhookSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.sweep(ctx)
			}
		}
	}()

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
			go d.deliverToAll(ctx, evt)
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
		// Durable queue first: until this row exists the event isn't
		// promised to the subscription, and once it does a crash (or an
		// exhausted receiver) can't lose it — the sweep keeps trying.
		if err := d.enqueueOutbox(ctx, sub.ID, evt, body); err != nil {
			slog.Error("webhook dispatcher: queueing event failed", "subscriptionId", sub.ID, "eventId", evt.ID, "error", err)
			continue
		}
		row := outboxRow{EventID: evt.ID, SubscriptionID: sub.ID, EventType: string(evt.Type), Payload: string(body)}
		go d.dispatchOutboxRow(ctx, row, sub)
	}
}

// enqueueOutbox durably records the (event, subscription) pair before any
// delivery attempt. The composite primary key makes re-publishing the same
// event idempotent per subscription: ON CONFLICT DO NOTHING keeps the
// original row — and its attempt state — instead of resetting the retry
// budget.
func (d *WebhookDispatcher) enqueueOutbox(ctx context.Context, subscriptionID string, evt events.Event, body []byte) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO webhook_outbox (event_id, subscription_id, event_type, payload, attempts, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, 0, NULL, ?)
		ON CONFLICT (event_id, subscription_id) DO NOTHING`,
		evt.ID, subscriptionID, string(evt.Type), string(body), time.Now().UTC().Format(time.RFC3339))
	return err
}

// dispatchOutboxRow delivers one outbox row, claiming an in-memory
// in-flight slot first so a sweep and a live dispatch (or two overlapping
// sweeps) can't hammer the same row concurrently. The guard is best-effort
// and in-memory only — a restart, or a claim racing a crash, can deliver
// twice. That is inherent to at-least-once delivery; receivers dedupe on
// the X-Ferrum-Delivery header.
func (d *WebhookDispatcher) dispatchOutboxRow(ctx context.Context, row outboxRow, sub WebhookSubscription) {
	key := row.EventID + "|" + row.SubscriptionID
	d.mu.Lock()
	if d.inFlight[key] {
		d.mu.Unlock()
		return
	}
	d.inFlight[key] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.inFlight, key)
		d.mu.Unlock()
	}()
	d.deliverWithRetry(ctx, row, sub)
}

// sweep re-attempts queued events: every due row (never attempted, or whose
// next_attempt_at has passed) goes through the same delivery path as a live
// event, and rows older than webhookOutboxRetention are deleted — a
// receiver unreachable that long is written off, with the delivery log
// preserving the history of what was tried.
func (d *WebhookDispatcher) sweep(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	cutoff := time.Now().UTC().Add(-webhookOutboxRetention).Format(time.RFC3339)
	rows, err := d.db.QueryContext(ctx, `
		SELECT event_id, subscription_id, event_type, payload, attempts
		FROM webhook_outbox
		WHERE (next_attempt_at IS NULL OR next_attempt_at <= ?) AND created_at > ?`, now, cutoff)
	if err != nil {
		slog.Error("webhook dispatcher: sweeping outbox failed", "error", err)
		return
	}
	var pending []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.EventID, &r.SubscriptionID, &r.EventType, &r.Payload, &r.Attempts); err != nil {
			rows.Close()
			slog.Error("webhook dispatcher: reading outbox row failed", "error", err)
			return
		}
		pending = append(pending, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("webhook dispatcher: sweeping outbox failed", "error", err)
		return
	}

	if _, err := d.db.ExecContext(ctx, `DELETE FROM webhook_outbox WHERE created_at <= ?`, cutoff); err != nil {
		slog.Error("webhook dispatcher: expiring outbox rows failed", "error", err)
	}

	// Each row dispatches in its own goroutine so one dead receiver's retry
	// sleeps can't serialize the whole sweep; dispatchOutboxRow's in-flight
	// guard keeps this from overlapping a live dispatch of the same row.
	var wg sync.WaitGroup
	for _, r := range pending {
		sub, err := d.subscriptionByID(ctx, r.SubscriptionID)
		if err != nil {
			// Deleting a subscription cascades its outbox rows away, so this
			// only races a delete or sees a transient DB error — skip.
			slog.Warn("webhook dispatcher: skipping outbox row with no usable subscription", "subscriptionId", r.SubscriptionID, "eventId", r.EventID, "error", err)
			continue
		}
		// A row whose dispatch already burned its attempt budget would
		// otherwise be re-selected every sweep without ever being retried
		// (the loop bound is cumulative) — reset it so the sweep keeps
		// trying fresh batches until the retention cutoff. The attempts
		// column is a retry-budget heuristic, not an audit record; the
		// delivery log preserves every attempt.
		if r.Attempts >= webhookMaxAttempts {
			r.Attempts = 0
		}
		wg.Add(1)
		go func(r outboxRow, sub WebhookSubscription) {
			defer wg.Done()
			d.dispatchOutboxRow(ctx, r, sub)
		}(r, sub)
	}
	wg.Wait()
}

// subscriptionByID loads one subscription for outbox delivery. Unlike
// matchingSubscriptions it doesn't filter on active: an event already
// accepted for a subscription is delivered even if it was deactivated
// afterwards (deleting it, by contrast, cascades the queue row away and
// does stop queued delivery).
func (d *WebhookDispatcher) subscriptionByID(ctx context.Context, id string) (WebhookSubscription, error) {
	var sub WebhookSubscription
	var secret, eventTypesJSON string
	var active int
	err := d.db.QueryRowContext(ctx,
		`SELECT id, name, url, secret, event_types, active FROM webhook_subscriptions WHERE id = ?`, id,
	).Scan(&sub.ID, &sub.Name, &sub.URL, &secret, &eventTypesJSON, &active)
	if err != nil {
		return WebhookSubscription{}, err
	}
	sub.Secret = d.decryptSecret(secret)
	sub.Active = active == 1
	var types []string
	if err := json.Unmarshal([]byte(eventTypesJSON), &types); err != nil {
		slog.Warn("webhook dispatcher: subscription has malformed event_types, skipping", "subscriptionId", sub.ID, "error", err)
		types = nil
	}
	sub.EventTypes = types
	return sub, nil
}

// decryptSecret returns the plaintext signing secret for a stored value.
// Values that fail to decrypt are returned as-is: rows written before
// encryption was introduced stored the plaintext (internal/api/webhooks.go
// encrypts on create), and those must keep signing until rewritten.
func (d *WebhookDispatcher) decryptSecret(enc string) string {
	if d.secrets == nil || enc == "" {
		return enc
	}
	if plain, err := d.secrets.Decrypt(enc); err == nil {
		return plain
	}
	return enc
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
		sub.Secret = d.decryptSecret(sub.Secret)
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

// deliverWithRetry POSTs the row's payload to sub.URL, retrying on failure
// (non-2xx response or transport error) with exponential backoff up to
// webhookMaxAttempts, logging every attempt to webhook_deliveries. It
// operates against the outbox row: success deletes the row (the event is
// delivered — the only point where it stops being queued), and each failure
// increments attempts and schedules next_attempt_at with the existing 2s
// doubling backoff, capped at webhookMaxBackoff, so the sweep picks the row
// up again if this process dies before the retries finish.
func (d *WebhookDispatcher) deliverWithRetry(ctx context.Context, row outboxRow, sub WebhookSubscription) {
	body := []byte(row.Payload)
	evt := events.Event{ID: row.EventID, Type: events.Type(row.EventType)}
	delay := webhookRetryBaseDelay
	for attempt := row.Attempts + 1; attempt <= webhookMaxAttempts; attempt++ {
		statusCode, err := d.deliverOnce(ctx, sub, evt, body)
		success := err == nil && statusCode >= 200 && statusCode < 300
		d.recordDelivery(ctx, sub.ID, evt, attempt, statusCode, err, success)
		if success {
			d.deleteOutbox(ctx, row)
			return
		}
		d.markOutboxFailed(ctx, row, attempt, time.Now().UTC().Add(delay))
		if attempt == webhookMaxAttempts {
			slog.Warn("webhook delivery exhausted retries", "subscriptionId", sub.ID, "url", sub.URL, "event", evt.Type, "attempts", attempt)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, webhookMaxBackoff)
	}
}

// deleteOutbox removes a row whose event its subscription accepted.
// Deleting it is the last step of delivery: a crash just before this point
// leaves the row queued and the event is redelivered on restart (dedupe on
// X-Ferrum-Delivery).
func (d *WebhookDispatcher) deleteOutbox(ctx context.Context, row outboxRow) {
	if _, err := d.db.ExecContext(ctx,
		`DELETE FROM webhook_outbox WHERE event_id = ? AND subscription_id = ?`, row.EventID, row.SubscriptionID); err != nil {
		slog.Error("webhook dispatcher: clearing delivered event failed", "subscriptionId", row.SubscriptionID, "eventId", row.EventID, "error", err)
	}
}

// markOutboxFailed records a failed attempt on the row: bumps attempts and
// schedules the sweep's next try at next.
func (d *WebhookDispatcher) markOutboxFailed(ctx context.Context, row outboxRow, attempts int, next time.Time) {
	if _, err := d.db.ExecContext(ctx,
		`UPDATE webhook_outbox SET attempts = ?, next_attempt_at = ? WHERE event_id = ? AND subscription_id = ?`,
		attempts, next.Format(time.RFC3339), row.EventID, row.SubscriptionID); err != nil {
		slog.Error("webhook dispatcher: updating outbox row failed", "subscriptionId", row.SubscriptionID, "eventId", row.EventID, "error", err)
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
