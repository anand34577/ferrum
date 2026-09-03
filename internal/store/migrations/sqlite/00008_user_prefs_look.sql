-- +goose Up
ALTER TABLE user_preferences ADD COLUMN look TEXT NOT NULL DEFAULT 'enterprise';

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN look;
