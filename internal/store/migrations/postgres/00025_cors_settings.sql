-- +goose Up
-- Opt-in CORS allow-list for the general REST API (browser-based 3rd-party
-- integrations calling with a bearer token need an explicit policy — the
-- SPA itself is same-origin and unaffected either way). Empty string (the
-- default) means CORS stays off, matching pre-existing same-origin-only
-- behavior.
ALTER TABLE system_settings ADD COLUMN cors_allowed_origins TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE system_settings DROP COLUMN cors_allowed_origins;
