-- +goose Up
-- Admin-controlled behavior for the AI Assistant's tool-calling agent loop
-- and the MCP server — single-row settings table, same pattern as
-- notification_settings/security_settings. Both come with a safe-by-default
-- posture: MCP is OFF until an admin opts in, and the tool-call ceiling has
-- a generous but finite default rather than being a hardcoded Go constant
-- that would need a code change to adjust.
CREATE TABLE agent_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    mcp_enabled         INTEGER NOT NULL DEFAULT 0,
    max_tool_iterations INTEGER NOT NULL DEFAULT 8,
    updated_at          TEXT NOT NULL
);

-- +goose Down
DROP TABLE agent_settings;
