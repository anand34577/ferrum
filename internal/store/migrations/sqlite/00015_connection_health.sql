-- +goose Up
-- Tracks whether each configured Proxmox connection was reachable on the
-- alert evaluator's last poll — independent of alert_rules/alert_instances,
-- which only fire on *metric thresholds* and are silently skipped when a
-- host can't be reached at all. A cluster/node being completely offline is
-- the single most important thing to be notified about, and must not depend
-- on the admin having configured any threshold rule first.
CREATE TABLE connection_health (
    connection_id   TEXT PRIMARY KEY REFERENCES connections(id) ON DELETE CASCADE,
    connection_name TEXT NOT NULL,
    status          TEXT NOT NULL, -- 'up' | 'down'
    last_error      TEXT,          -- most recent failure detail, empty when status = 'up'
    since           TEXT NOT NULL, -- when the current status began
    updated_at      TEXT NOT NULL  -- last time this row was refreshed (every poll tick)
);

-- +goose Down
DROP TABLE connection_health;
