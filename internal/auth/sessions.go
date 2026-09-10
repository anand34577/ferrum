package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrSessionNotFound is returned by RevokeSession when no session matches
// the given id (already revoked, expired and purged, or — for the
// user-scoped variant — belongs to a different account).
var ErrSessionNotFound = errors.New("session not found")

// Session is one row of the sessions table, shaped for the Sessions panel
// (ProfilePage) and its admin equivalent (Users page). The plaintext token
// itself is never exposed — only sha256(token) is ever persisted, and
// nothing here lets it be recovered.
type Session struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt,omitempty"`
	ExpiresAt  string `json:"expiresAt"`
	IP         string `json:"ip,omitempty"`
	UserAgent  string `json:"userAgent,omitempty"`
	// Current marks the session tied to the cookie the caller used to make
	// this very request — set by ListSessions when currentToken is non-empty.
	Current bool `json:"current"`
}

// ListSessions returns every non-expired session belonging to userID,
// most-recently-created first. currentToken (the cookie value of the
// request making this call) is hashed and compared so the UI can mark
// "this device" and steer the user away from revoking it by accident; pass
// "" (e.g. from an admin listing another user's sessions) to skip that.
func (s *Service) ListSessions(ctx context.Context, userID, currentToken string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, token_hash, COALESCE(ip,''), COALESCE(user_agent,''), created_at, COALESCE(last_seen_at,''), expires_at
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC`,
		userID, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	currentHash := ""
	if currentToken != "" {
		currentHash = hashToken(currentToken)
	}

	out := []Session{}
	for rows.Next() {
		var sess Session
		var tokenHash string
		if err := rows.Scan(&sess.ID, &tokenHash, &sess.IP, &sess.UserAgent, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		sess.Current = currentHash != "" && tokenHash == currentHash
		out = append(out, sess)
	}
	return out, rows.Err()
}

// RevokeSession deletes one session by id. When userID is non-empty, the
// delete is scoped to sessions owned by that user — the shape used by the
// self-service "revoke this device" endpoint, so one account can never
// revoke another's session by guessing an id. Pass "" for the admin path,
// which may revoke any user's session.
func (s *Service) RevokeSession(ctx context.Context, sessionID, userID string) error {
	var (
		res sql.Result
		err error
	)
	if userID != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, sessionID, userID)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RevokeOtherSessions deletes every session belonging to userID except the
// one matching exceptToken (typically the caller's own current session) —
// backs "log out all other devices". Returns how many were revoked.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, exceptToken string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, hashToken(exceptToken))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeAllSessions deletes every session belonging to userID — the admin
// "sign this user out everywhere" action, e.g. after resetting their
// password or suspecting a compromised account.
func (s *Service) RevokeAllSessions(ctx context.Context, userID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
