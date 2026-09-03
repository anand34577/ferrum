package store

import (
	"path/filepath"
	"testing"

	"ferrum/internal/config"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenRunsMigrationsAndSeeds(t *testing.T) {
	db := openTestDB(t)

	var permCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rbac_permissions`).Scan(&permCount); err != nil {
		t.Fatalf("querying rbac_permissions: %v", err)
	}
	if permCount == 0 {
		t.Fatal("expected the RBAC permission catalog to be seeded on first open")
	}

	var roleCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rbac_roles WHERE is_system = 1`).Scan(&roleCount); err != nil {
		t.Fatalf("querying rbac_roles: %v", err)
	}
	if roleCount != len(builtinRoles) {
		t.Fatalf("seeded %d system roles, want %d", roleCount, len(builtinRoles))
	}

	var ruleCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alert_rules`).Scan(&ruleCount); err != nil {
		t.Fatalf("querying alert_rules: %v", err)
	}
	if ruleCount != len(defaultAlertRules) {
		t.Fatalf("seeded %d alert rules, want %d", ruleCount, len(defaultAlertRules))
	}

	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("querying users: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("expected no users to be seeded (first admin comes from the setup wizard), got %d", userCount)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	cfg := config.DBConfig{Driver: "sqlite", Path: dbPath}

	db1, err := Open(cfg)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db1.Close()

	db2, err := Open(cfg)
	if err != nil {
		t.Fatalf("second Open (reopening existing db) failed: %v", err)
	}
	defer db2.Close()

	var permCount int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM rbac_permissions`).Scan(&permCount); err != nil {
		t.Fatalf("querying rbac_permissions after reopen: %v", err)
	}
	if permCount != len(builtinPermissions) {
		t.Fatalf("reopening re-seeded or lost data: got %d permissions, want %d", permCount, len(builtinPermissions))
	}
}
