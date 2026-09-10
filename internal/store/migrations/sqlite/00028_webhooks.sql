-- +goose Up
-- Outgoing webhook subscriptions: fan out internal/events.Bus events to
-- third-party URLs as signed POSTs. event_types is a JSON array of
-- events.Type strings (e.g. ["alert.triggered","connection.down"]); an
-- empty array means "all event types".
CREATE TABLE webhook_subscriptions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL, -- HMAC-SHA256 signing key, stored as-is (not a login credential; rotated by deleting/recreating)
    event_types TEXT NOT NULL DEFAULT '[]',
    active      INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- Delivery log: the last N attempts per subscription, for diagnosing a
-- webhook a third party says it never received. Rows are pruned to a fixed
-- count per subscription by the dispatcher itself (see internal/notify).
CREATE TABLE webhook_deliveries (
    id              TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    event_id        TEXT NOT NULL,
    attempt         INTEGER NOT NULL,
    status_code     INTEGER,       -- NULL when the request itself failed (timeout, DNS, connection refused, ...)
    error           TEXT,
    success         INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_webhook_deliveries_subscription ON webhook_deliveries(subscription_id, created_at DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhook_subscriptions;
