-- +goose Up
-- Defaults to 0 (off): enabling RP-Initiated Logout unconditionally for an
-- existing SSO deployment would send the browser to a post-logout redirect
-- URL the provider hasn't been told to trust yet, breaking sign-out instead
-- of improving it. The admin opts in once they've registered it.
ALTER TABLE oidc_settings ADD COLUMN single_logout INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE oidc_settings DROP COLUMN single_logout;
