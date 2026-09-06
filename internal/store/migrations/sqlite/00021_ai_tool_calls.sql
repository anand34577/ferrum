-- +goose Up
-- Full trace of every tool the AI Assistant or an MCP client executed on a
-- user's behalf — every call, not just mutating ones, so a user can see
-- exactly what the LLM looked at or did, and an admin can review the same
-- across every user. Deliberately separate from audit_log: that table only
-- ever records state-changing actions (its long-standing convention across
-- the whole app), while this one is a complete read+write trace scoped to
-- agent activity specifically.
CREATE TABLE ai_tool_calls (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source     TEXT NOT NULL, -- "chat" (AI Assistant) | "mcp" (external MCP client)
    tool       TEXT NOT NULL,
    args       TEXT,          -- JSON, best-effort — never contains secrets, tool args are IDs/enums
    ok         INTEGER NOT NULL,
    error      TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_ai_tool_calls_user_id ON ai_tool_calls(user_id, created_at);
CREATE INDEX idx_ai_tool_calls_created_at ON ai_tool_calls(created_at);

-- +goose Down
DROP TABLE ai_tool_calls;
