package digest

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"ferrum/internal/store"
)

// Settings is the single (id=1) digest_settings row — same single-row
// pattern as system_settings/security_settings. Lives in this package
// (rather than internal/api, where every other *_settings row lives) because
// Scheduler needs to read it on every tick and internal/api already depends
// on internal/digest for the HTTP handlers, so the dependency can't run the
// other way.
type Settings struct {
	Enabled       bool
	IntervalHours int
	Recipients    []string // extra emails, on top of the admin's global SMTP "to" list
	LastSentAt    time.Time // zero means never sent
}

// DefaultIntervalHours is one week — the digest is meant to be a periodic
// roll-up, not a daily nag.
const DefaultIntervalHours = 7 * 24

func defaultSettings() Settings {
	return Settings{Enabled: false, IntervalHours: DefaultIntervalHours}
}

func LoadSettings(ctx context.Context, db *store.DB) (Settings, error) {
	var enabled int
	var recipients string
	var lastSentAt sql.NullString
	row := defaultSettings()
	err := db.QueryRowContext(ctx, `
		SELECT enabled, interval_hours, recipients, last_sent_at FROM digest_settings WHERE id = 1`).
		Scan(&enabled, &row.IntervalHours, &recipients, &lastSentAt)
	if err == sql.ErrNoRows {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	row.Enabled = enabled == 1
	row.Recipients = splitRecipients(recipients)
	if lastSentAt.Valid && lastSentAt.String != "" {
		if t, err := time.Parse(time.RFC3339, lastSentAt.String); err == nil {
			row.LastSentAt = t
		}
	}
	return row, nil
}

func SaveSettings(ctx context.Context, db *store.DB, s Settings) error {
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	var lastSentAt *string
	if !s.LastSentAt.IsZero() {
		v := s.LastSentAt.UTC().Format(time.RFC3339)
		lastSentAt = &v
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO digest_settings (id, enabled, interval_hours, recipients, last_sent_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			enabled = excluded.enabled, interval_hours = excluded.interval_hours,
			recipients = excluded.recipients, last_sent_at = excluded.last_sent_at, updated_at = excluded.updated_at`,
		enabled, s.IntervalHours, strings.Join(s.Recipients, ","), lastSentAt, time.Now().UTC().Format(time.RFC3339))
	return err
}

// markSent stamps last_sent_at without disturbing the rest of the row —
// used by the scheduler right after a send attempt (success or failure: a
// failed send shouldn't retry every checkInterval and spam a broken relay).
func markSent(ctx context.Context, db *store.DB, at time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE digest_settings SET last_sent_at = ?, updated_at = ? WHERE id = 1`,
		at.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	return err
}

func splitRecipients(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
