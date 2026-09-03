-- +goose Up
-- Single-row settings tables (id is always 1) — admin-editable via the
-- Settings UI, so OIDC/SSO and outbound notifications no longer require
-- editing config.yaml and restarting the process.
CREATE TABLE oidc_settings (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    enabled       INTEGER NOT NULL DEFAULT 0,
    display_name  TEXT NOT NULL DEFAULT '',
    issuer_url    TEXT NOT NULL DEFAULT '',
    client_id     TEXT NOT NULL DEFAULT '',
    client_secret TEXT NOT NULL DEFAULT '', -- encrypted at rest (secrets.Box), same as connection credentials
    redirect_url  TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL
);

CREATE TABLE notification_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    gotify_enabled  INTEGER NOT NULL DEFAULT 0,
    gotify_url      TEXT NOT NULL DEFAULT '',
    gotify_token    TEXT NOT NULL DEFAULT '', -- encrypted at rest
    smtp_enabled    INTEGER NOT NULL DEFAULT 0,
    smtp_host       TEXT NOT NULL DEFAULT '',
    smtp_port       INTEGER NOT NULL DEFAULT 587,
    smtp_username   TEXT NOT NULL DEFAULT '',
    smtp_password   TEXT NOT NULL DEFAULT '', -- encrypted at rest
    smtp_from       TEXT NOT NULL DEFAULT '',
    smtp_to         TEXT NOT NULL DEFAULT '', -- comma-separated recipients
    smtp_use_tls    INTEGER NOT NULL DEFAULT 1,
    updated_at      TEXT NOT NULL
);

-- +goose Down
DROP TABLE notification_settings;
DROP TABLE oidc_settings;
