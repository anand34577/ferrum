-- +goose Up
-- Instance-wide kill switch for the general REST API-key surface (ScopeAPI
-- bearer tokens), mirroring mcp_enabled on the same row. Defaults ON since
-- the REST API predates this toggle and existing keys/integrations should
-- keep working after upgrade — unlike MCP, which is safe-by-default OFF.
ALTER TABLE agent_settings ADD COLUMN api_enabled INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE agent_settings DROP COLUMN api_enabled;
