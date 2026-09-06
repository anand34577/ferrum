-- +goose Up
-- General operational knobs that were previously Go constants requiring a
-- rebuild to change — single-row settings table, same pattern as
-- security_settings/agent_settings. Starts with the alert-rule poll
-- interval (was a hardcoded 60s in cmd/ferrum/main.go); more knobs can be
-- added as columns here as they're identified, rather than each getting its
-- own one-off table.
CREATE TABLE system_settings (
    id                     INTEGER PRIMARY KEY CHECK (id = 1),
    alert_poll_seconds     INTEGER NOT NULL DEFAULT 60,
    updated_at             TEXT NOT NULL
);

-- +goose Down
DROP TABLE system_settings;
