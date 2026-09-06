-- +goose Up
-- Long-lived bearer tokens for 3rd-party API/MCP access. A key authenticates
-- as its owning user (inherits whatever is_admin currently is — not
-- snapshotted), same trust model as a session cookie.
CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL, -- first chars of the plaintext key, shown in the UI so a user can tell keys apart
    key_hash     TEXT NOT NULL UNIQUE,
    last_used_at TEXT,
    expires_at   TEXT,
    created_at   TEXT NOT NULL,
    revoked_at   TEXT
);
CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);

-- +goose Down
DROP TABLE api_keys;
