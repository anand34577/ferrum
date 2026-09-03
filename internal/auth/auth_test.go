package auth

import (
	"context"
	"path/filepath"
	"testing"

	"ferrum/internal/config"
	"ferrum/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db)
}

func TestBootstrapCreatesFirstAdminOnly(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	needsSetup, err := svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needsSetup {
		t.Fatal("expected NeedsSetup to be true on a fresh database")
	}

	user, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !user.IsAdmin {
		t.Fatal("bootstrapped user must be an admin")
	}

	needsSetup, err = svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup after bootstrap: %v", err)
	}
	if needsSetup {
		t.Fatal("expected NeedsSetup to be false after bootstrap")
	}

	if _, err := svc.Bootstrap(ctx, "second", "second@example.com", "supersecret1"); err == nil {
		t.Fatal("expected a second Bootstrap call to fail")
	}
}

func TestLoginRoundTrip(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "correct-password"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if _, _, err := svc.Login(ctx, "admin", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("Login with wrong password: got %v, want ErrInvalidCredentials", err)
	}

	user, token, err := svc.Login(ctx, "admin", "correct-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty session token")
	}

	authed, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if authed.ID != user.ID {
		t.Fatalf("Authenticate returned user %q, want %q", authed.ID, user.ID)
	}

	if _, err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); err != ErrInvalidCredentials {
		t.Fatalf("Authenticate after logout: got %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticateRejectsUnknownToken(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Authenticate(context.Background(), "not-a-real-token"); err != ErrInvalidCredentials {
		t.Fatalf("Authenticate(unknown token): got %v, want ErrInvalidCredentials", err)
	}
}
