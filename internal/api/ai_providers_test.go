package api

import (
	"context"
	"testing"

	"ferrum/internal/needle"
)

// TestSeedBuiltinNeedleProviderIsDefaultAndSticky exercises the "built-in
// assistant is always there and always the default" contract: New() already
// seeds it once; this checks that a competing provider set as default gets
// demoted back on the next seed pass (e.g. server restart), and that
// re-seeding never creates a second built-in provider row.
func TestSeedBuiltinNeedleProviderIsDefaultAndSticky(t *testing.T) {
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

	// Simulate an operator adding another provider and making it the
	// default — this must not survive the next seed pass.
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO ai_providers (id, name, base_url, model, is_enabled, is_default, created_at, updated_at)
		VALUES ('other-provider', 'Other', 'https://example.com/v1', '', 1, 0, 'now', 'now')`); err != nil {
		t.Fatalf("inserting competing provider: %v", err)
	}
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO ai_provider_models (id, provider_id, label, model_id, is_default, created_at)
		VALUES ('other-model', 'other-provider', 'Other model', 'other', 1, 'now')`); err != nil {
		t.Fatalf("inserting competing model: %v", err)
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
	if defaultProviderBaseURL != needle.BaseURL {
		t.Fatalf("expected the built-in Needle provider to be the default, got %q", defaultProviderBaseURL)
	}

	if err := env.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_providers WHERE base_url = ?`, needle.BaseURL).Scan(&providerCount); err != nil {
		t.Fatalf("re-counting needle providers: %v", err)
	}
	if providerCount != 1 {
		t.Fatalf("re-seeding must not duplicate the built-in provider row, got %d", providerCount)
	}
}
