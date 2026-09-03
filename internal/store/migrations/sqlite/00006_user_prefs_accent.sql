-- +goose Up
ALTER TABLE user_preferences ADD COLUMN accent TEXT NOT NULL DEFAULT 'oxide';

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN accent;
