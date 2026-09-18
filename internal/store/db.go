package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// DB wraps *sql.DB so callers can always write queries using "?" placeholders
// (SQLite's native style) while Postgres gets them rebound to "$1, $2, ..."
// automatically. This is what lets internal/store/seed.go and future query
// code stay driver-agnostic without a full query builder.
type DB struct {
	*sql.DB
	driver string
}

func newDB(driver string, sqlDB *sql.DB) *DB {
	return &DB{DB: sqlDB, driver: driver}
}

func (d *DB) rebind(query string) string {
	if d.driver != "postgres" {
		return query
	}
	return rebindPostgres(query)
}

// rebindPostgres rewrites "?" placeholders to Postgres's "$1, $2, ..." style,
// skipping any "?" inside a single-quoted SQL string literal (tracking ''
// as an escaped quote, not a close) so a query with a literal "?" in a LIKE
// pattern or a Postgres JSONB "?"/"?|"/"?&" operator isn't corrupted into a
// bogus positional parameter — every query in this codebase currently only
// uses "?" as a placeholder, but this makes that an enforced invariant
// instead of a silent assumption the moment one doesn't.
func rebindPostgres(query string) string {
	var b strings.Builder
	n := 0
	inString := false
	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\'':
			inString = !inString
			b.WriteRune(r)
		case r == '?' && !inString:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(d.rebind(query), args...)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.rebind(query), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(d.rebind(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.rebind(query), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(d.rebind(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.rebind(query), args...)
}

// Begin starts a transaction whose Exec/Query/QueryRow calls are also rebound.
func (d *DB) Begin() (*Tx, error) {
	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

type Tx struct {
	*sql.Tx
	driver string
}

func (t *Tx) rebind(query string) string {
	if t.driver != "postgres" {
		return query
	}
	return rebindPostgres(query)
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.rebind(query), args...)
}

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.rebind(query), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(t.rebind(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(t.rebind(query), args...)
}
