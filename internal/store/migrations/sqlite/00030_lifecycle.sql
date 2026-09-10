-- +goose Up
-- Guest lifecycle policies: fleet-wide snapshot retention (a periodic sweep
-- in internal/poller) plus a log of what it did/would do. Single-row
-- settings, same pattern as system_settings/security_settings. Starts
-- disabled (enforce = 0) and dry-run only — the sweep always evaluates and
-- logs what it would delete, but never actually deletes a snapshot until an
-- admin explicitly flips enforce on, so turning it on isn't a leap of faith.
CREATE TABLE lifecycle_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    retention_days      INTEGER NOT NULL DEFAULT 0, -- fleet-wide default snapshot retention window; 0 = no fleet-wide default (per-guest "retain:<N>d" tags still apply)
    enforce             INTEGER NOT NULL DEFAULT 0, -- 0 = dry-run only (log what would be deleted); 1 = actually delete
    updated_at          TEXT NOT NULL
);

-- lifecycle_actions is the retention sweep's audit trail: one row per
-- snapshot it decided was past its retention window, whether or not it
-- actually deleted it (dry_run distinguishes the two) — so enabling
-- enforcement later can be checked against a history of exactly what would
-- have happened.
CREATE TABLE lifecycle_actions (
    id              TEXT PRIMARY KEY,
    connection_id   TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    connection_name TEXT NOT NULL,
    guest_type      TEXT NOT NULL, -- 'qemu' | 'lxc'
    node            TEXT NOT NULL,
    vmid            INTEGER NOT NULL,
    guest_name      TEXT NOT NULL,
    action          TEXT NOT NULL, -- 'snapshot.delete'
    detail          TEXT NOT NULL, -- e.g. snapshot name + age
    dry_run         INTEGER NOT NULL, -- 1 = would have happened, not applied; 0 = actually applied
    status          TEXT NOT NULL DEFAULT 'ok', -- 'ok' | 'error'
    error           TEXT,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_lifecycle_actions_created_at ON lifecycle_actions(created_at DESC);

-- +goose Down
DROP TABLE lifecycle_actions;
DROP TABLE lifecycle_settings;
