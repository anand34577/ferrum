package auth

import (
	"context"
	"testing"
)

func TestListSessionsMarksCurrentAndScopesByUser(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	admin, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	other, err := svc.createUser(ctx, "other", "other@example.com", "supersecret1", false)
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}

	tokenA, err := svc.CreateSession(ctx, admin.ID, "10.0.0.1", "agent-a")
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	if _, err := svc.CreateSession(ctx, admin.ID, "10.0.0.2", "agent-b"); err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}
	if _, err := svc.CreateSession(ctx, other.ID, "10.0.0.3", "agent-c"); err != nil {
		t.Fatalf("CreateSession C: %v", err)
	}

	sessions, err := svc.ListSessions(ctx, admin.ID, tokenA)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for admin, got %d", len(sessions))
	}
	var sawCurrent bool
	for _, s := range sessions {
		if s.Current {
			sawCurrent = true
			if s.IP != "10.0.0.1" || s.UserAgent != "agent-a" {
				t.Fatalf("current session has wrong ip/agent: %+v", s)
			}
		}
	}
	if !sawCurrent {
		t.Fatal("expected exactly one session to be marked current")
	}

	otherSessions, err := svc.ListSessions(ctx, other.ID, "")
	if err != nil {
		t.Fatalf("ListSessions (other): %v", err)
	}
	if len(otherSessions) != 1 {
		t.Fatalf("expected 1 session for other user, got %d", len(otherSessions))
	}
}

func TestRevokeSessionIsScopedToOwner(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	admin, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	other, err := svc.createUser(ctx, "other", "other@example.com", "supersecret1", false)
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}

	if _, err := svc.CreateSession(ctx, admin.ID, "10.0.0.1", "agent-a"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessions, err := svc.ListSessions(ctx, admin.ID, "")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions: %v (%d)", err, len(sessions))
	}
	sessionID := sessions[0].ID

	// other user must not be able to revoke admin's session.
	if err := svc.RevokeSession(ctx, sessionID, other.ID); err != ErrSessionNotFound {
		t.Fatalf("cross-user RevokeSession: got %v, want ErrSessionNotFound", err)
	}

	// admin revoking their own session succeeds.
	if err := svc.RevokeSession(ctx, sessionID, admin.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	sessions, err = svc.ListSessions(ctx, admin.ID, "")
	if err != nil {
		t.Fatalf("ListSessions after revoke: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after revoke, got %d", len(sessions))
	}

	// Revoking an already-gone session reports not found.
	if err := svc.RevokeSession(ctx, sessionID, admin.ID); err != ErrSessionNotFound {
		t.Fatalf("re-revoking: got %v, want ErrSessionNotFound", err)
	}
}

func TestRevokeOtherSessionsKeepsCurrent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	admin, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	current, err := svc.CreateSession(ctx, admin.ID, "10.0.0.1", "agent-a")
	if err != nil {
		t.Fatalf("CreateSession current: %v", err)
	}
	if _, err := svc.CreateSession(ctx, admin.ID, "10.0.0.2", "agent-b"); err != nil {
		t.Fatalf("CreateSession other: %v", err)
	}
	if _, err := svc.CreateSession(ctx, admin.ID, "10.0.0.3", "agent-c"); err != nil {
		t.Fatalf("CreateSession other2: %v", err)
	}

	n, err := svc.RevokeOtherSessions(ctx, admin.ID, current)
	if err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 sessions revoked, got %d", n)
	}

	sessions, err := svc.ListSessions(ctx, admin.ID, current)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("expected only the current session to remain: %+v", sessions)
	}

	// The current session itself must still authenticate.
	if _, err := svc.Authenticate(ctx, current); err != nil {
		t.Fatalf("Authenticate(current) after RevokeOtherSessions: %v", err)
	}
}

func TestRevokeAllSessionsAdmin(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	admin, err := svc.Bootstrap(ctx, "admin", "admin@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	other, err := svc.createUser(ctx, "other", "other@example.com", "supersecret1", false)
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}

	if _, err := svc.CreateSession(ctx, other.ID, "10.0.0.1", "agent-a"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.CreateSession(ctx, other.ID, "10.0.0.2", "agent-b"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	n, err := svc.RevokeAllSessions(ctx, other.ID)
	if err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 sessions revoked, got %d", n)
	}

	sessions, err := svc.ListSessions(ctx, other.ID, "")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions left for other user, got %d", len(sessions))
	}

	// admin's own sessions are untouched.
	if _, err := svc.CreateSession(ctx, admin.ID, "10.0.0.9", "agent-z"); err != nil {
		t.Fatalf("CreateSession admin: %v", err)
	}
	adminSessions, err := svc.ListSessions(ctx, admin.ID, "")
	if err != nil {
		t.Fatalf("ListSessions admin: %v", err)
	}
	if len(adminSessions) != 1 {
		t.Fatalf("expected admin to keep their own session, got %d", len(adminSessions))
	}
}
