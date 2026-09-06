-- +goose Up
-- OpenAI-chat-completions-compatible providers (OpenAI, Ollama, LM Studio,
-- LocalAI, OpenRouter, ...) that back the AI Assistant. api_key_enc is
-- optional — most local runtimes don't require one.
CREATE TABLE ai_providers (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    base_url     TEXT NOT NULL, -- e.g. https://api.openai.com/v1, http://localhost:11434/v1
    api_key_enc  TEXT,          -- encrypted at rest (secrets.Box); NULL when not required
    model        TEXT NOT NULL,
    is_enabled   INTEGER NOT NULL DEFAULT 1,
    is_default   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- +goose Down
DROP TABLE ai_providers;
