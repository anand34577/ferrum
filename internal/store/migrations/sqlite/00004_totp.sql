-- +goose Up
CREATE TABLE user_totp_recovery_codes (
    id        TEXT PRIMARY KEY,
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TEXT
);
CREATE INDEX idx_totp_recovery_user_id ON user_totp_recovery_codes(user_id);

-- +goose Down
DROP TABLE user_totp_recovery_codes;
