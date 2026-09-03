package store

import "testing"

func TestRebindLeavesSQLiteUntouched(t *testing.T) {
	db := &DB{driver: "sqlite"}
	query := `SELECT * FROM users WHERE id = ? AND name = ?`
	if got := db.rebind(query); got != query {
		t.Fatalf("rebind(sqlite) = %q, want unchanged %q", got, query)
	}
}

func TestRebindConvertsPlaceholdersForPostgres(t *testing.T) {
	db := &DB{driver: "postgres"}
	got := db.rebind(`SELECT * FROM users WHERE id = ? AND name = ?`)
	want := `SELECT * FROM users WHERE id = $1 AND name = $2`
	if got != want {
		t.Fatalf("rebind(postgres) = %q, want %q", got, want)
	}
}

func TestRebindHandlesNoPlaceholders(t *testing.T) {
	db := &DB{driver: "postgres"}
	query := `SELECT COUNT(*) FROM users`
	if got := db.rebind(query); got != query {
		t.Fatalf("rebind with no placeholders = %q, want unchanged %q", got, query)
	}
}

func TestTxRebindMatchesDBRebind(t *testing.T) {
	tx := &Tx{driver: "postgres"}
	got := tx.rebind(`UPDATE t SET a = ? WHERE b = ? AND c = ?`)
	want := `UPDATE t SET a = $1 WHERE b = $2 AND c = $3`
	if got != want {
		t.Fatalf("Tx.rebind(postgres) = %q, want %q", got, want)
	}
}
