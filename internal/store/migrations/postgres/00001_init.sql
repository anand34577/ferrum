-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    totp_secret   TEXT,
    totp_enabled  INTEGER NOT NULL DEFAULT 0,
    oidc_subject  TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    ip         TEXT,
    user_agent TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE connections (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    host                 TEXT NOT NULL,
    port                 INTEGER NOT NULL DEFAULT 8006,
    auth_type            TEXT NOT NULL, -- 'token' | 'password'
    token_id             TEXT,
    token_secret_enc     TEXT,
    username             TEXT,
    password_enc         TEXT,
    verify_tls           INTEGER NOT NULL DEFAULT 1,
    behind_reverse_proxy INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE rbac_permissions (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE,
    category    TEXT NOT NULL,
    description TEXT NOT NULL
);

CREATE TABLE rbac_roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    color       TEXT,
    is_system   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);

CREATE TABLE rbac_role_permissions (
    role_id       TEXT NOT NULL REFERENCES rbac_roles(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES rbac_permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE rbac_user_roles (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    TEXT NOT NULL REFERENCES rbac_roles(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL DEFAULT 'global', -- 'global' | 'connection' | 'node' | 'vm'
    scope_id   TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_rbac_user_roles_user_id ON rbac_user_roles(user_id);

CREATE TABLE audit_log (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    category   TEXT NOT NULL,
    target     TEXT,
    detail     TEXT,
    ip         TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE rbac_user_roles;
DROP TABLE rbac_role_permissions;
DROP TABLE rbac_roles;
DROP TABLE rbac_permissions;
DROP TABLE settings;
DROP TABLE connections;
DROP TABLE sessions;
DROP TABLE users;
