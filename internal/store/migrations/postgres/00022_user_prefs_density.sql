-- +goose Up
-- Table row/list density — "comfortable" (current default padding) or
-- "compact" (tighter rows) — same per-user + org-default pattern as look.
ALTER TABLE user_preferences ADD COLUMN density TEXT NOT NULL DEFAULT 'comfortable';
ALTER TABLE default_preferences ADD COLUMN density TEXT NOT NULL DEFAULT 'comfortable';

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN density;
ALTER TABLE default_preferences DROP COLUMN density;
