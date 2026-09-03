-- +goose Up
CREATE TABLE user_preferences (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    theme      TEXT NOT NULL DEFAULT 'system',
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE user_preferences;
