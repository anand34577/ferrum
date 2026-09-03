-- +goose Up
-- Org-wide defaults (id=1) applied to a user's first-ever preferences read,
-- before they've saved anything of their own — admin-editable from Settings.
-- Existing users with a user_preferences row are unaffected either way.
CREATE TABLE default_preferences (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    theme         TEXT NOT NULL DEFAULT 'system',
    accent        TEXT NOT NULL DEFAULT 'oxide',
    look          TEXT NOT NULL DEFAULT 'enterprise',
    landing_page  TEXT NOT NULL DEFAULT '/',
    updated_at    TEXT NOT NULL
);

-- notify_email: per-user opt-in to receive alert-trigger emails at their own
-- account email address, on top of (not instead of) the admin's globally
-- configured SMTP recipient list. landing_page: which route opens after
-- login, overriding the org-wide default above for this one user.
ALTER TABLE user_preferences ADD COLUMN notify_email INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_preferences ADD COLUMN landing_page TEXT;

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN landing_page;
ALTER TABLE user_preferences DROP COLUMN notify_email;
DROP TABLE default_preferences;
