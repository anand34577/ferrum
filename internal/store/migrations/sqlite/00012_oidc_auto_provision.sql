-- +goose Up
-- Defaults to 1 (auto-provision, the historical always-on behavior) so
-- existing SSO deployments keep working exactly as before this existed.
ALTER TABLE oidc_settings ADD COLUMN allow_auto_provision INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE oidc_settings DROP COLUMN allow_auto_provision;
