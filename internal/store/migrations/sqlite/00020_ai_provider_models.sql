-- +goose Up
-- A provider (one API endpoint + credential) can expose more than one
-- model — e.g. one OpenAI provider with gpt-4o-mini and gpt-4o, or one local
-- Ollama instance with several pulled models. label is the human-facing
-- name shown in the picker; model_id is the exact identifier sent to the
-- provider's API — kept as two fields since they're frequently different
-- (a friendly "GPT-4o mini" vs the wire value "gpt-4o-mini", or a local
-- runtime's tag like "qwen2.5-coder:7b-instruct-q4_K_M").
CREATE TABLE ai_provider_models (
    id          TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES ai_providers(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    is_default  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_ai_provider_models_provider_id ON ai_provider_models(provider_id);

-- Existing rows carried a single model directly on ai_providers — migrate
-- each into a first model row so upgraded installs don't lose their
-- configuration. The ai_providers.model column itself is left in place
-- (unused going forward, always written as '' by new code) rather than
-- dropped, since SQLite's column drop requires a full table rebuild that
-- isn't worth the risk for a column that's simply ignored from here on.
INSERT INTO ai_provider_models (id, provider_id, label, model_id, is_default, created_at)
SELECT lower(hex(randomblob(16))), id, model, model, is_default, updated_at
FROM ai_providers
WHERE model IS NOT NULL AND model != '';

-- +goose Down
DROP TABLE ai_provider_models;
