package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/config"
	"ferrum/internal/events"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testBox(t *testing.T) *secrets.Box {
	t.Helper()
	box, err := secrets.New("webhook-test-secret")
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	return box
}

func insertSubscription(t *testing.T, db *store.DB, url, secret string, eventTypes []string, active bool) string {
	t.Helper()
	id := uuid.NewString()
	typesJSON, err := json.Marshal(eventTypes)
	if err != nil {
		t.Fatalf("marshal event types: %v", err)
	}
	activeCol := 0
	if active {
		activeCol = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO webhook_subscriptions (id, name, url, secret, event_types, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "test sub", url, secret, string(typesJSON), activeCol, now, now,
	); err != nil {
		t.Fatalf("inserting subscription: %v", err)
	}
	return id
}

func TestSubscriptionMatches(t *testing.T) {
	cases := []struct {
		name       string
		subscribed []string
		evtType    events.Type
		want       bool
	}{
		{"empty filter matches everything", nil, events.TypeAlertTriggered, true},
		{"exact match", []string{"alert.triggered", "connection.down"}, events.TypeAlertTriggered, true},
		{"no match", []string{"connection.down"}, events.TypeAlertTriggered, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subscriptionMatches(tc.subscribed, tc.evtType); got != tc.want {
				t.Errorf("subscriptionMatches(%v, %v) = %v, want %v", tc.subscribed, tc.evtType, got, tc.want)
			}
		})
	}
}

func TestSignBodyIsDeterministicAndKeyed(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	sig1 := signBody("secret-a", body)
	sig2 := signBody("secret-a", body)
	sig3 := signBody("secret-b", body)
	if sig1 != sig2 {
		t.Fatal("same secret+body should produce the same signature")
	}
	if sig1 == sig3 {
		t.Fatal("different secrets should produce different signatures")
	}
}

func TestWebhookDispatcherDeliversMatchingEventAndSignsBody(t *testing.T) {
	var (
		gotSignature string
		gotEventType string
		gotBody      []byte
	)
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		gotSignature = r.Header.Get(SignatureHeader)
		gotEventType = r.Header.Get(EventTypeHeader)
		w.WriteHeader(http.StatusOK)
		received <- struct{}{}
	}))
	defer server.Close()

	db := openTestDB(t)
	secret := "shh-its-a-secret"
	insertSubscription(t, db, server.URL, secret, nil, true)

	bus := events.New()
	d := NewWebhookDispatcher(db, testBox(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, bus)

	// Give Run a moment to subscribe before publishing.
	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: events.TypeAlertTriggered, ResourceID: "qemu/100"})

	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook was never delivered")
	}

	if gotEventType != string(events.TypeAlertTriggered) {
		t.Fatalf("event type header = %q, want %q", gotEventType, events.TypeAlertTriggered)
	}
	wantSig := signBody(secret, gotBody)
	if gotSignature != wantSig {
		t.Fatalf("signature = %q, want %q (recomputed over received body)", gotSignature, wantSig)
	}

	// The delivery log write happens after the response is already returned
	// to the server handler above, so poll briefly rather than racing it.
	deadline := time.Now().Add(2 * time.Second)
	var count, success int
	for {
		err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success), 0) FROM webhook_deliveries`).Scan(&count, &success)
		if err != nil {
			t.Fatalf("querying webhook_deliveries: %v", err)
		}
		if count >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 1 || success != 1 {
		t.Fatalf("deliveries = %d (success=%d), want 1 (success=1)", count, success)
	}
}

func TestWebhookDispatcherSkipsInactiveAndNonMatchingSubscriptions(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db := openTestDB(t)
	insertSubscription(t, db, server.URL, "s1", nil, false)                        // inactive
	insertSubscription(t, db, server.URL, "s2", []string{"connection.down"}, true) // wrong type

	bus := events.New()
	d := NewWebhookDispatcher(db, testBox(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, bus)

	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: events.TypeAlertTriggered})
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("expected no deliveries, got %d", got)
	}
}

// --- outbox (durable, at-least-once delivery) ---

// A delivered event (receiver 200) must have its outbox row deleted and a
// success entry in the delivery log.
func TestWebhookDeliveryClearsOutboxRowOnSuccess(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		received <- struct{}{}
	}))
	defer server.Close()

	db := openTestDB(t)
	insertSubscription(t, db, server.URL, "s", nil, true)

	bus := events.New()
	d := NewWebhookDispatcher(db, testBox(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, bus)

	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: events.TypeAlertTriggered})

	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook was never delivered")
	}

	waitFor(t, 2*time.Second, "outbox row to be deleted", func() (bool, string) {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_outbox`).Scan(&count); err != nil {
			return false, err.Error()
		}
		return count == 0, fmt.Sprintf("outbox rows = %d, want 0", count)
	})

	var total, success int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success), 0) FROM webhook_deliveries`).Scan(&total, &success); err != nil {
		t.Fatalf("querying deliveries: %v", err)
	}
	if total != 1 || success != 1 {
		t.Fatalf("deliveries = %d (success=%d), want 1 (success=1)", total, success)
	}
}

// A failing receiver leaves the row queued with its attempt counter and
// next retry time recorded, so the sweep can pick it up after a restart.
func TestWebhookFailureRetainsOutboxRowWithBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	db := openTestDB(t)
	insertSubscription(t, db, server.URL, "s", nil, true)

	bus := events.New()
	d := NewWebhookDispatcher(db, testBox(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, bus)

	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: events.TypeAlertTriggered})

	// The first failed attempt marks the row immediately (before the retry
	// sleep), so this resolves without waiting out the full backoff chain.
	waitFor(t, 3*time.Second, "outbox row to record the failed attempt", func() (bool, string) {
		var attempts int
		var next sql.NullString
		err := db.QueryRow(`SELECT attempts, next_attempt_at FROM webhook_outbox`).Scan(&attempts, &next)
		if err != nil {
			return false, err.Error()
		}
		if attempts < 1 {
			return false, fmt.Sprintf("attempts = %d, want >= 1", attempts)
		}
		if !next.Valid || next.String == "" {
			return false, "next_attempt_at is NULL, want a scheduled retry time"
		}
		return true, ""
	})

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_outbox`).Scan(&count); err != nil {
		t.Fatalf("querying outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox rows = %d, want 1 (failed delivery must stay queued)", count)
	}
}

// sweep() re-attempts a due row — one a previous run (or an exhausted
// in-process retry budget) left queued — and deletes it once the recovered
// receiver accepts the event.
func TestSweepDispatchesDueRowAndDeletesItAfterReceiverRecovers(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db := openTestDB(t)
	subID := insertSubscription(t, db, server.URL, "s", nil, true)

	// Seed the queue the way a failed live attempt (or a crash mid-retry)
	// would have: attempts=1 and a next_attempt_at that has already passed.
	evt := events.Event{ID: "evt-due", Type: events.TypeAlertTriggered, Timestamp: time.Now().UTC()}
	body, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshaling event: %v", err)
	}
	now := time.Now().UTC()
	past := now.Add(-time.Second).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO webhook_outbox (event_id, subscription_id, event_type, payload, attempts, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)`,
		evt.ID, subID, string(evt.Type), string(body), past, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("seeding outbox row: %v", err)
	}

	d := NewWebhookDispatcher(db, testBox(t))
	down.Store(false) // receiver recovered
	d.sweep(context.Background())

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_outbox`).Scan(&count); err != nil {
		t.Fatalf("querying outbox: %v", err)
	}
	if count != 0 {
		t.Fatalf("outbox rows = %d after sweep, want 0 (due row must be delivered and deleted)", count)
	}
	var total, success, maxAttempt int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success), 0), COALESCE(MAX(attempt), 0) FROM webhook_deliveries`).Scan(&total, &success, &maxAttempt); err != nil {
		t.Fatalf("querying deliveries: %v", err)
	}
	if total != 1 || success != 1 || maxAttempt != 2 {
		t.Fatalf("deliveries = %d (success=%d, attempt=%d), want 1 (success=1, attempt=2)", total, success, maxAttempt)
	}
}

// Re-publishing the same event must not duplicate or reset the queue row —
// the composite (event_id, subscription_id) key with ON CONFLICT DO NOTHING
// keeps the original row and its attempt state.
func TestOutboxInsertIsIdempotentPerSubscription(t *testing.T) {
	db := openTestDB(t)
	subID := insertSubscription(t, db, "http://receiver.invalid/hook", "s", nil, true)
	d := NewWebhookDispatcher(db, testBox(t))
	ctx := context.Background()

	evt := events.Event{ID: "evt-dup", Type: events.TypeAlertTriggered, Timestamp: time.Now().UTC()}
	body, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshaling event: %v", err)
	}
	if err := d.enqueueOutbox(ctx, subID, evt, body); err != nil {
		t.Fatalf("first enqueueOutbox: %v", err)
	}
	// Simulate a previously-failed attempt, then re-publish the same event:
	// the row must keep its attempt state, not be reset to a fresh one.
	if _, err := db.Exec(`UPDATE webhook_outbox SET attempts = 3`); err != nil {
		t.Fatalf("seeding attempts: %v", err)
	}
	if err := d.enqueueOutbox(ctx, subID, evt, body); err != nil {
		t.Fatalf("duplicate enqueueOutbox: %v", err)
	}

	var count, attempts int
	if err := db.QueryRow(`SELECT COUNT(*), MAX(attempts) FROM webhook_outbox`).Scan(&count, &attempts); err != nil {
		t.Fatalf("querying outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox rows = %d, want 1 (duplicate insert must be a no-op)", count)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d after duplicate insert, want 3 (row state preserved)", attempts)
	}
}

// waitFor polls cond until it reports true or the deadline passes, failing
// with the condition's last explanation. Delivery log/outbox writes race the
// receiver's HTTP response, so polling beats sleeping.
func waitFor(t *testing.T, within time.Duration, what string, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		ok, detail := cond()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %s", what, detail)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSendTestEventDeliversSynchronouslyAndReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	db := openTestDB(t)
	d := NewWebhookDispatcher(db, testBox(t))
	id := insertSubscription(t, db, server.URL, "s", nil, true)
	sub := WebhookSubscription{ID: id, URL: server.URL, Secret: "s"}
	if err := d.SendTestEvent(context.Background(), sub); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

// TestWebhookSecretRoundTripAndLegacyPlaintextFallback covers the at-rest
// secret migration: a subscription whose stored secret is encrypted must be
// signed with the decrypted value, and one still holding a plaintext secret
// from before encryption existed must keep working unchanged.
func TestWebhookSecretRoundTripAndLegacyPlaintextFallback(t *testing.T) {
	box := testBox(t)
	secret := "legacy-plaintext-secret"
	enc, err := box.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypting secret: %v", err)
	}

	var (
		mu       sync.Mutex
		bodies   []string
		sigs     []string
		received = make(chan struct{}, 2)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		sigs = append(sigs, r.Header.Get(SignatureHeader))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		received <- struct{}{}
	}))
	defer server.Close()

	db := openTestDB(t)
	insertSubscription(t, db, server.URL, enc, nil, true)    // encrypted at rest
	insertSubscription(t, db, server.URL, secret, nil, true) // legacy plaintext row

	bus := events.New()
	d := NewWebhookDispatcher(db, box)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, bus)

	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: events.TypeAlertTriggered})

	for i := 0; i < 2; i++ {
		select {
		case <-received:
		case <-time.After(3 * time.Second):
			t.Fatalf("delivery %d of 2 never arrived", i+1)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	// One event fanned out to both subscriptions — identical bodies, so both
	// signatures must match the PLAINTEXT secret (decrypted, or passed through).
	if len(bodies) != 2 {
		t.Fatalf("got %d deliveries, want 2", len(bodies))
	}
	for i, sig := range sigs {
		if want := signBody(secret, []byte(bodies[i])); sig != want {
			t.Fatalf("delivery %d signature = %q, want %q (signed with the plaintext secret)", i, sig, want)
		}
	}
}
