-- +goose Up
CREATE TABLE alert_rules (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    metric        TEXT NOT NULL, -- 'node_cpu' | 'node_mem' | 'node_disk' | 'guest_cpu' | 'guest_mem'
    connection_id TEXT REFERENCES connections(id) ON DELETE CASCADE, -- NULL = applies to all connections
    threshold     REAL NOT NULL, -- percent, 0-100
    severity      TEXT NOT NULL DEFAULT 'warning', -- 'warning' | 'critical'
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL
);

CREATE TABLE alert_instances (
    id              TEXT PRIMARY KEY,
    rule_id         TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    connection_id   TEXT NOT NULL,
    connection_name TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    resource_name   TEXT NOT NULL,
    metric          TEXT NOT NULL,
    value           REAL NOT NULL,
    threshold       REAL NOT NULL,
    severity        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active', -- 'active' | 'resolved' | 'silenced'
    triggered_at    TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    resolved_at     TEXT,
    UNIQUE (rule_id, resource_id)
);
CREATE INDEX idx_alert_instances_status ON alert_instances(status);

-- +goose Down
DROP TABLE alert_instances;
DROP TABLE alert_rules;
