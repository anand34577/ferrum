package api

import (
	"context"
	"testing"

	"ferrum/internal/needle"
)

// TestSeedBuiltinNeedleProviderIsDefaultOnlyUntilAdminChoosesOtherwise
// exercises the "built-in assistant is always there, and is the default
// until an admin picks something else" contract: New() already seeds it
// once (so a fresh install has a working assistant with zero setup); this
// checks that a competing provider explicitly made the default SURVIVES the
// next seed pass (e.g. a server restart) instead of being silently demoted
// back to Needle, and that re-seeding never creates a second built-in
// provider row.
func TestSeedBuiltinNeedleProviderIsDefaultOnlyUntilAdminChoosesOtherwise(t *testing.T) {
	env := newTestEnv(t)
	if !env.server.needle.Available() {
		t.Skip("no needle binary bundled/configured for this platform")
	}
	ctx := context.Background()

	var providerCount int
	if err := env.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_providers WHERE base_url = ?`, needle.BaseURL).Scan(&providerCount); err != nil {
		t.Fatalf("counting needle providers: %v", err)
	}
	if providerCount != 1 {
		t.Fatalf("expected exactly one built-in provider row after New(), got %d", providerCount)
	}

	// Simulate an admin adding another provider and explicitly making it the
	// default — a later restart (another seed pass) must respect that choice.
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO ai_providers (id, name, base_url, model, is_enabled, is_default, created_at, updated_at)
		VALUES ('other-provider', 'Other', 'https://example.com/v1', '', 1, 0, 'now', 'now')`); err != nil {
		t.Fatalf("inserting competing provider: %v", err)
	}
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO ai_provider_models (id, provider_id, label, model_id, is_default, created_at)
		VALUES ('other-model', 'other-provider', 'Other model', 'other', 0, 'now')`); err != nil {
		t.Fatalf("inserting competing model: %v", err)
	}
	if err := env.server.clearOtherDefaultModels(ctx, env.db, "other-model"); err != nil {
		t.Fatalf("clearing other default models: %v", err)
	}
	if _, err := env.db.ExecContext(ctx, `UPDATE ai_provider_models SET is_default = 1 WHERE id = 'other-model'`); err != nil {
		t.Fatalf("making the competing model the default: %v", err)
	}

	env.server.seedBuiltinNeedleProvider(ctx)

	var defaultCount int
	if err := env.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_provider_models WHERE is_default = 1`).Scan(&defaultCount); err != nil {
		t.Fatalf("counting default models: %v", err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default model, got %d", defaultCount)
	}

	var defaultProviderBaseURL string
	if err := env.db.QueryRowContext(ctx, `
		SELECT p.base_url FROM ai_provider_models m
		JOIN ai_providers p ON p.id = m.provider_id
		WHERE m.is_default = 1`).Scan(&defaultProviderBaseURL); err != nil {
		t.Fatalf("finding default provider: %v", err)
	}
	if defaultProviderBaseURL != "https://example.com/v1" {
		t.Fatalf("expected the admin's chosen default to survive re-seeding, got %q", defaultProviderBaseURL)
	}

	if err := env.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_providers WHERE base_url = ?`, needle.BaseURL).Scan(&providerCount); err != nil {
		t.Fatalf("re-counting needle providers: %v", err)
	}
	if providerCount != 1 {
		t.Fatalf("re-seeding must not duplicate the built-in provider row, got %d", providerCount)
	}
}

// TestSeedBuiltinNeedleProviderBecomesDefaultWhenNoneIsSet covers the fresh-
// install path: if nothing holds the default slot (e.g. the previous
// default model row was deleted), Needle claims it so the assistant still
// works with zero setup.
func TestSeedBuiltinNeedleProviderBecomesDefaultWhenNoneIsSet(t *testing.T) {
	env := newTestEnv(t)
	if !env.server.needle.Available() {
		t.Skip("no needle binary bundled/configured for this platform")
	}
	ctx := context.Background()

	if _, err := env.db.ExecContext(ctx, `UPDATE ai_provider_models SET is_default = 0`); err != nil {
		t.Fatalf("clearing the default: %v", err)
	}

	env.server.seedBuiltinNeedleProvider(ctx)

	var defaultProviderBaseURL string
	if err := env.db.QueryRowContext(ctx, `
		SELECT p.base_url FROM ai_provider_models m
		JOIN ai_providers p ON p.id = m.provider_id
		WHERE m.is_default = 1`).Scan(&defaultProviderBaseURL); err != nil {
		t.Fatalf("finding default provider: %v", err)
	}
	if defaultProviderBaseURL != needle.BaseURL {
		t.Fatalf("expected Needle to claim the empty default slot, got %q", defaultProviderBaseURL)
	}
}
