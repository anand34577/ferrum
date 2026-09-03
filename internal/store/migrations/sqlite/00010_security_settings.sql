-- +goose Up
-- Single-row (id=1) security policy — admin-editable from Settings, applied
-- to the running server live (session TTL, login lockout) with no restart.
CREATE TABLE security_settings (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    session_ttl_hours     INTEGER NOT NULL DEFAULT 720, -- 30 days, matches the prior hardcoded default
    login_max_failures    INTEGER NOT NULL DEFAULT 5,
    login_lockout_minutes INTEGER NOT NULL DEFAULT 15,
    require_2fa_admins    INTEGER NOT NULL DEFAULT 0,
    updated_at            TEXT NOT NULL
);

-- +goose Down
DROP TABLE security_settings;
