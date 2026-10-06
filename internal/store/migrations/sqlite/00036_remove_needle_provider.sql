-- +goose Up
-- The built-in Needle 2 provider was removed. Drop any row it seeded (its
-- models cascade) so the AI Assistant doesn't keep trying to call the
-- now-unroutable needle:// sentinel URL.
DELETE FROM ai_providers WHERE base_url LIKE 'needle://%';
DELETE FROM settings WHERE key = 'ai.needle_provider_dismissed';

-- +goose Down
SELECT 1;
