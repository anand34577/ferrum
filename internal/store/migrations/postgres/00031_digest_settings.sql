-- +goose Up
-- Scheduled fleet health digest (email/webhook summary of uptime, backup
-- success rate, active alerts, capacity) — single-row settings, same
-- pattern as system_settings/notification_settings. last_sent_at drives the
-- scheduler's due-time check (internal/digest.Scheduler); it's NULL until
-- the first send.
CREATE TABLE digest_settings (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    enabled        INTEGER NOT NULL DEFAULT 0,
    interval_hours INTEGER NOT NULL DEFAULT 168,
    recipients     TEXT NOT NULL DEFAULT '',
    last_sent_at   TEXT,
    updated_at     TEXT NOT NULL
);

-- +goose Down
DROP TABLE digest_settings;
