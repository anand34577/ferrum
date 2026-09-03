package poller

import (
	"context"
	"path/filepath"
	"testing"

	"ferrum/internal/config"
	"ferrum/internal/connections"
	"ferrum/internal/pve"
	"ferrum/internal/store"
)

func newTestEvaluator(t *testing.T) (*AlertEvaluator, *store.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewAlertEvaluator(db, connections.New(db, nil)), db
}

// insertRule persists a rule row so alert_instances' FK on rule_id is
// satisfiable, then returns the in-memory rule struct evaluateConnection expects.
func insertRule(t *testing.T, db *store.DB, id, metric string, threshold float64) rule {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO alert_rules (id, name, metric, threshold, severity, enabled, created_at) VALUES (?, ?, ?, ?, 'warning', 1, '2026-01-01T00:00:00Z')`,
		id, id, metric, threshold,
	); err != nil {
		t.Fatalf("inserting alert_rules row: %v", err)
	}
	return rule{ID: id, Metric: metric, Threshold: threshold}
}

func TestEvaluateConnectionOpensAlertAboveThreshold(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}

	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}
	resources := []pve.ClusterResource{
		{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.95},
	}

	eval.evaluateConnection(ctx, conn, resources, rules, map[string]map[string]bool{})

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alert_instances WHERE status = 'active'`).Scan(&count); err != nil {
		t.Fatalf("querying alert_instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("active alert count = %d, want 1", count)
	}

	var value float64
	if err := db.QueryRow(`SELECT value FROM alert_instances WHERE status = 'active'`).Scan(&value); err != nil {
		t.Fatalf("querying alert value: %v", err)
	}
	if value != 95 {
		t.Fatalf("alert value = %v, want 95 (95%% CPU)", value)
	}
}

func TestEvaluateConnectionDoesNotAlertBelowThreshold(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}

	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}
	resources := []pve.ClusterResource{
		{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.50},
	}

	eval.evaluateConnection(ctx, conn, resources, rules, map[string]map[string]bool{})

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alert_instances`).Scan(&count); err != nil {
		t.Fatalf("querying alert_instances: %v", err)
	}
	if count != 0 {
		t.Fatalf("alert_instances count = %d, want 0 (below threshold)", count)
	}
}

func TestEvaluateConnectionAutoResolvesWhenBackUnderThreshold(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}
	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}

	// First pass: trigger the alert.
	eval.evaluateConnection(ctx, conn, []pve.ClusterResource{{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.95}}, rules, map[string]map[string]bool{})

	var activeCount int
	db.QueryRow(`SELECT COUNT(*) FROM alert_instances WHERE status = 'active'`).Scan(&activeCount)
	if activeCount != 1 {
		t.Fatalf("expected 1 active alert after first pass, got %d", activeCount)
	}

	// Second pass: value drops back down — the instance should auto-resolve.
	eval.evaluateConnection(ctx, conn, []pve.ClusterResource{{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.10}}, rules, map[string]map[string]bool{})

	db.QueryRow(`SELECT COUNT(*) FROM alert_instances WHERE status = 'active'`).Scan(&activeCount)
	if activeCount != 0 {
		t.Fatalf("expected 0 active alerts after resolving, got %d", activeCount)
	}
	var resolvedCount int
	db.QueryRow(`SELECT COUNT(*) FROM alert_instances WHERE status = 'resolved'`).Scan(&resolvedCount)
	if resolvedCount != 1 {
		t.Fatalf("expected 1 resolved alert, got %d", resolvedCount)
	}
}

func TestEvaluateConnectionCoversAllResourceKinds(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	conn := connections.Info{ID: "conn1", Name: "Test Cluster"}

	rules := []rule{
		insertRule(t, db, "r-node-mem", "node_mem", 90),
		insertRule(t, db, "r-guest-mem", "guest_mem", 90),
		insertRule(t, db, "r-storage", "storage_usage", 90),
	}
	resources := []pve.ClusterResource{
		{ID: "node/pve1", Type: "node", Node: "pve1", Mem: 95, MaxMem: 100},
		{ID: "qemu/100", Type: "qemu", Node: "pve1", Name: "web01", Mem: 96, MaxMem: 100},
		{ID: "storage/local", Type: "storage", Node: "pve1", Storage: "local", Disk: 97, MaxDisk: 100},
		{ID: "pool/prod", Type: "pool"}, // unhandled resource type — must be skipped, not error
	}

	eval.evaluateConnection(ctx, conn, resources, rules, map[string]map[string]bool{})

	rows, err := db.Query(`SELECT metric, resource_name, value FROM alert_instances WHERE status = 'active' ORDER BY metric`)
	if err != nil {
		t.Fatalf("querying alert_instances: %v", err)
	}
	defer rows.Close()

	type got struct {
		metric, name string
		value        float64
	}
	var results []got
	for rows.Next() {
		var g got
		if err := rows.Scan(&g.metric, &g.name, &g.value); err != nil {
			t.Fatalf("scanning row: %v", err)
		}
		results = append(results, g)
	}

	want := []got{
		{"guest_mem", "web01", 96},
		{"node_mem", "pve1", 95},
		{"storage_usage", "local", 97},
	}
	if len(results) != len(want) {
		t.Fatalf("got %d active alerts, want %d: %+v", len(results), len(want), results)
	}
	for i, w := range want {
		if results[i] != w {
			t.Fatalf("result[%d] = %+v, want %+v", i, results[i], w)
		}
	}
}

func TestEvaluateConnectionKeepsAlertsFromDifferentConnectionsSeparate(t *testing.T) {
	eval, db := newTestEvaluator(t)
	ctx := context.Background()
	rules := []rule{insertRule(t, db, "rule1", "node_cpu", 90)}

	// Two different connections whose nodes happen to share the same raw PVE
	// resource id ("node/pve1") — this is the bug class the resourceID
	// prefixing in evaluateConnection exists to prevent.
	eval.evaluateConnection(ctx, connections.Info{ID: "connA", Name: "A"}, []pve.ClusterResource{{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.95}}, rules, map[string]map[string]bool{})
	eval.evaluateConnection(ctx, connections.Info{ID: "connB", Name: "B"}, []pve.ClusterResource{{ID: "node/pve1", Type: "node", Node: "pve1", CPU: 0.95}}, rules, map[string]map[string]bool{})

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alert_instances WHERE status = 'active'`).Scan(&count); err != nil {
		t.Fatalf("querying alert_instances: %v", err)
	}
	if count != 2 {
		t.Fatalf("active alert count across two connections = %d, want 2 (one per connection)", count)
	}
}
