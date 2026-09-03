-- +goose Up
-- The raw ID token from an SSO login, kept only long enough to hand back to
-- the provider as id_token_hint on RP-Initiated Logout (sign out of Ferrum
-- *and* the identity provider in one click) — NULL for password logins.
ALTER TABLE sessions ADD COLUMN oidc_id_token TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN oidc_id_token;
