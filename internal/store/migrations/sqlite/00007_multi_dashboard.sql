-- +goose Up
-- Replaces the single layout-per-user model with named, multiple dashboards
-- per user — existing layouts are preserved as each user's first dashboard.
CREATE TABLE dashboards (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    layout     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_dashboards_user_id ON dashboards(user_id);

INSERT INTO dashboards (id, user_id, name, layout, created_at, updated_at)
SELECT lower(hex(randomblob(16))), user_id, 'Main', layout, updated_at, updated_at FROM dashboard_layouts;

DROP TABLE dashboard_layouts;

ALTER TABLE user_preferences ADD COLUMN active_dashboard_id TEXT;

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN active_dashboard_id;

CREATE TABLE dashboard_layouts (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    layout     TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO dashboard_layouts (user_id, layout, updated_at)
SELECT user_id, layout, updated_at FROM dashboards d
WHERE d.id = (SELECT id FROM dashboards d2 WHERE d2.user_id = d.user_id ORDER BY created_at LIMIT 1);
DROP TABLE dashboards;
