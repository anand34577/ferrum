-- +goose Up
-- Scopes an API key to what it's allowed to authenticate: "api" (the
-- existing behavior — full REST API, whatever the owning user can do) or
-- "mcp" (the MCP endpoint only). A generic "api" key deliberately does NOT
-- work against /mcp and vice versa — MCP clients (Claude, etc.) must be
-- handed a token the user explicitly created for that purpose, not
-- whatever REST key they already had lying around.
ALTER TABLE api_keys ADD COLUMN scope TEXT NOT NULL DEFAULT 'api';

-- +goose Down
ALTER TABLE api_keys DROP COLUMN scope;
