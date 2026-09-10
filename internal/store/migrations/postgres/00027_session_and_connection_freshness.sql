-- +goose Up
-- Sessions were always server-side (see internal/auth), but nothing tracked
-- when a session was last actually used — only when it was created and when
-- it expires. last_seen_at backs the Sessions panel (ProfilePage) so an
-- account owner can tell "this device, active 2 minutes ago" from a
-- forgotten login they should revoke.
ALTER TABLE sessions ADD COLUMN last_seen_at TEXT;

-- Smallest viable signal for "is this connection's credential still good":
-- bumped on every successful poll by the alert evaluator (see
-- internal/poller/certificates.go), read back both by the built-in
-- connection-staleness alert and any future settings-page display.
ALTER TABLE connections ADD COLUMN last_verified_at TEXT;

-- +goose Down
ALTER TABLE connections DROP COLUMN last_verified_at;
ALTER TABLE sessions DROP COLUMN last_seen_at;
