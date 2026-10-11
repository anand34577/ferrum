package poller

import (
	"context"
	"testing"
	"time"

	"ferrum/internal/connections"
	"ferrum/internal/events"
	"ferrum/internal/pve"
)

func nodeAt(cpu float64) []pve.ClusterResource {
	return []pve.ClusterResource{{ID: "node/pve1", Type: "node", Node: "pve1", CPU: cpu}}
}

// drain counts the events of one type currently buffered on a subscription.
func drain(ch <-chan events.Event, typ events.Type) int {
	n := 0
	for {
		select {
		case evt := <-ch:
			if evt.Type == typ {
				n++
			}
		default:
			return n
		}
	}
}

// An alert that resolved and then breaches its threshold again is a new
// incident: it must announce itself again and carry the new start time. The
// instance row is reused across incidents (unique rule+resource), so "row
// already exists" must not be mistaken for "already announced".
func TestAlertRefiresAfterResolving(t *testing.T) {
	eval, db := newTestEvaluator(t)
	bus := events.New()
	eval.SetBus(bus)
	ch, cancel := bus.Subscribe()
	defer cancel()

	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}
	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}
	seen := func() map[string]map[string]bool { return map[string]map[string]bool{} }

	eval.evaluateConnection(ctx, conn, nodeAt(0.95), rules, seen())
	if got := drain(ch, events.TypeAlertTriggered); got != 1 {
		t.Fatalf("first breach published %d triggered events, want 1", got)
	}
	// Backdate the first incident so a refreshed start time is observable.
	if _, err := db.Exec(`UPDATE alert_instances SET triggered_at = '2020-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}

	eval.evaluateConnection(ctx, conn, nodeAt(0.10), rules, seen())
	// Still breaching on a later tick must NOT re-announce.
	eval.evaluateConnection(ctx, conn, nodeAt(0.96), rules, seen())
	if got := drain(ch, events.TypeAlertTriggered); got != 1 {
		t.Fatalf("second breach published %d triggered events, want 1", got)
	}
	eval.evaluateConnection(ctx, conn, nodeAt(0.97), rules, seen())
	if got := drain(ch, events.TypeAlertTriggered); got != 0 {
		t.Fatalf("an already-active alert re-announced itself %d time(s)", got)
	}

	var status, triggeredAt string
	var resolvedAt *string
	if err := db.QueryRow(`SELECT status, triggered_at, resolved_at FROM alert_instances`).Scan(&status, &triggeredAt, &resolvedAt); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
	if triggeredAt == "2020-01-01T00:00:00Z" {
		t.Error("triggered_at still shows the first incident's start time")
	}
	if resolvedAt != nil {
		t.Errorf("resolved_at = %q on an active alert, want NULL", *resolvedAt)
	}
}

// A silenced alert whose condition clears must resolve like any other —
// otherwise the mute outlives the incident and silently swallows every
// future breach on that resource.
func TestSilencedAlertResolvesAndLaterRefires(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}
	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}
	seen := func() map[string]map[string]bool { return map[string]map[string]bool{} }
	status := func() string {
		var s string
		if err := db.QueryRow(`SELECT status FROM alert_instances`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	eval.evaluateConnection(ctx, conn, nodeAt(0.95), rules, seen())
	if _, err := db.Exec(`UPDATE alert_instances SET status = 'silenced'`); err != nil {
		t.Fatal(err)
	}

	// Still breaching: the mute holds.
	eval.evaluateConnection(ctx, conn, nodeAt(0.96), rules, seen())
	if got := status(); got != "silenced" {
		t.Fatalf("status while still breaching = %q, want silenced", got)
	}

	eval.evaluateConnection(ctx, conn, nodeAt(0.10), rules, seen())
	if got := status(); got != "resolved" {
		t.Fatalf("status after the condition cleared = %q, want resolved", got)
	}

	eval.evaluateConnection(ctx, conn, nodeAt(0.95), rules, seen())
	if got := status(); got != "active" {
		t.Fatalf("status on the next breach = %q, want active (the old mute must not carry over)", got)
	}
}

// A timed silence holds until its deadline, then the alert comes back on its
// own if the condition is still firing.
func TestTimedSilenceExpires(t *testing.T) {
	eval, db := newTestEvaluator(t)
	bus := events.New()
	eval.SetBus(bus)
	ch, cancel := bus.Subscribe()
	defer cancel()

	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}
	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}
	seen := func() map[string]map[string]bool { return map[string]map[string]bool{} }
	silenceUntil := func(ts time.Time) {
		if _, err := db.Exec(`UPDATE alert_instances SET status = 'silenced', silenced_until = ?`, ts.UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (status string, until *string) {
		if err := db.QueryRow(`SELECT status, silenced_until FROM alert_instances`).Scan(&status, &until); err != nil {
			t.Fatal(err)
		}
		return
	}

	eval.evaluateConnection(ctx, conn, nodeAt(0.95), rules, seen())
	drain(ch, events.TypeAlertTriggered)

	silenceUntil(time.Now().Add(time.Hour))
	eval.expireSilences(ctx)
	eval.evaluateConnection(ctx, conn, nodeAt(0.95), rules, seen())
	if s, _ := read(); s != "silenced" {
		t.Fatalf("status before the deadline = %q, want silenced", s)
	}

	silenceUntil(time.Now().Add(-time.Minute))
	eval.expireSilences(ctx)
	s, until := read()
	if s != "active" {
		t.Fatalf("status after the deadline = %q, want active", s)
	}
	if until != nil {
		t.Errorf("silenced_until = %q after expiry, want NULL", *until)
	}
	if got := drain(ch, events.TypeAlertTriggered); got != 1 {
		t.Errorf("expiry published %d triggered events, want 1 (the bell must light up again)", got)
	}
}
