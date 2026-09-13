-- +goose Up
-- Durable delivery queue for outgoing webhooks (internal/notify). Each
-- matching (event, subscription) pair is written here BEFORE the first
-- delivery attempt, so a crash mid-delivery or a temporarily unreachable
-- receiver no longer loses the event — the dispatcher deletes a row once
-- the receiver accepts it, and a periodic sweep re-attempts rows whose
-- next_attempt_at has come due (previously delivery was at-most-once,
-- backed only by the event bus's in-memory channel). The composite primary
-- key makes re-publishing the same event idempotent per subscription.
-- Rows older than 24h are abandoned by the sweep (unreachable receiver);
-- the webhook_deliveries log preserves the attempt history.
CREATE TABLE webhook_outbox (
    event_id        TEXT NOT NULL,
    subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    payload         TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT, -- NULL = due now; otherwise the sweep's earliest retry time
    created_at      TEXT NOT NULL,
    PRIMARY KEY (event_id, subscription_id)
);
CREATE INDEX idx_webhook_outbox_subscription_due ON webhook_outbox(subscription_id, next_attempt_at);

-- +goose Down
DROP TABLE webhook_outbox;
