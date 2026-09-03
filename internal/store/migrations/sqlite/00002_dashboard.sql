-- +goose Up
CREATE TABLE dashboard_layouts (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    layout     TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE dashboard_layouts;
