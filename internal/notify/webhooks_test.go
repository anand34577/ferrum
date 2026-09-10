package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/config"
	"ferrum/internal/events"
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
	d := NewWebhookDispatcher(db)
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
	d := NewWebhookDispatcher(db)
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

func TestSendTestEventDeliversSynchronouslyAndReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	db := openTestDB(t)
	d := NewWebhookDispatcher(db)
	id := insertSubscription(t, db, server.URL, "s", nil, true)
	sub := WebhookSubscription{ID: id, URL: server.URL, Secret: "s"}
	if err := d.SendTestEvent(context.Background(), sub); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}
