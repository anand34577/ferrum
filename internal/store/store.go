// Package store opens the application database (SQLite by default, Postgres
// optionally) and runs schema migrations + first-run seeding.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"ferrum/internal/config"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

// Open connects to the configured database, runs pending migrations, and
// seeds first-run data (RBAC catalog, default roles) if the users table is empty.
func Open(cfg config.DBConfig) (*DB, error) {
	switch cfg.Driver {
	case "postgres":
		return openWith("pgx", cfg.DSN, postgresMigrations, "migrations/postgres")
	default:
		if dir := filepath.Dir(cfg.Path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("creating db directory %s: %w", dir, err)
			}
		}
		dsn := cfg.Path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		return openWith("sqlite", dsn, sqliteMigrations, "migrations/sqlite")
	}
}

func openWith(driver, dsn string, fsys embed.FS, dir string) (*DB, error) {
	sqlDB, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s database: %w", driver, err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to %s database: %w", driver, err)
	}
	if driver == "sqlite" {
		sqlDB.SetMaxOpenConns(1) // modernc/sqlite: single-writer, avoids SQLITE_BUSY under our own load
	}

	goose.SetBaseFS(fsys)
	if err := goose.SetDialect(driver); err != nil {
		return nil, fmt.Errorf("setting migration dialect: %w", err)
	}
	if err := goose.Up(sqlDB, dir); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	slog.Info("migrations applied", "driver", driver)

	db := newDB(driver, sqlDB)
	seeded, err := seedIfEmpty(db)
	if err != nil {
		return nil, fmt.Errorf("seeding database: %w", err)
	}
	if seeded {
		slog.Info("seeded first-run data", "driver", driver)
	}

	return db, nil
}
