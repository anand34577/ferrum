package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// apiKeyPrefix marks a Ferrum-issued key visually distinct from a session
// token, so it's recognizable when pasted into a 3rd-party app's config.
const apiKeyPrefix = "fer_"

// apiKeyPrefixLen is how many characters of the plaintext key are stored
// (unencrypted, alongside the hash) so the UI can show "fer_a1b2c3d4…"
// without ever persisting the full secret.
const apiKeyPrefixLen = 12

var ErrAPIKeyNotFound = errors.New("api key not found")

// Key scopes. A key only ever authenticates for the endpoint family it was
// scoped to at creation — a general "api" key is rejected by the MCP
// endpoint and vice versa, so connecting an MCP client always requires a
// token the user explicitly created for that purpose.
const (
	ScopeAPI = "api"
	ScopeMCP = "mcp"
)

type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	Scope      string     `json:"scope"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// CreateAPIKey mints a new bearer token for userID, scoped to either general
// REST API access (ScopeAPI) or the MCP endpoint only (ScopeMCP). The
// plaintext key is returned once and never stored — only its sha256 hash
// (same scheme as session tokens, see hashToken) is persisted.
func (s *Service) CreateAPIKey(ctx context.Context, userID, name, scope string, expiresInDays int) (plainKey string, key *APIKey, err error) {
	if scope != ScopeAPI && scope != ScopeMCP {
		scope = ScopeAPI
	}
	plainKey = apiKeyPrefix + randomToken()
	now := time.Now().UTC()

	var expiresAt sql.NullString
	var expiresAtPtr *time.Time
	if expiresInDays > 0 {
		t := now.Add(time.Duration(expiresInDays) * 24 * time.Hour)
		expiresAt = sql.NullString{String: t.Format(time.RFC3339), Valid: true}
		expiresAtPtr = &t
	}

	id := uuid.NewString()
	prefix := plainKey
	if len(prefix) > apiKeyPrefixLen {
		prefix = prefix[:apiKeyPrefixLen]
	}
	if _, err = s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, scope, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, prefix, hashToken(plainKey), scope, expiresAt, now.Format(time.RFC3339),
	); err != nil {
		return "", nil, err
	}
	return plainKey, &APIKey{ID: id, Name: name, KeyPrefix: prefix, Scope: scope, ExpiresAt: expiresAtPtr, CreatedAt: now}, nil
}

// ListAPIKeys returns every non-revoked key belonging to userID, newest first.
func (s *Service) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, key_prefix, scope, last_used_at, expires_at, created_at
		 FROM api_keys WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		var lastUsed, expires sql.NullString
		var createdAt string
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Scope, &lastUsed, &expires, &createdAt); err != nil {
			return nil, err
		}
		k.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if lastUsed.Valid {
			if t, err := time.Parse(time.RFC3339, lastUsed.String); err == nil {
				k.LastUsedAt = &t
			}
		}
		if expires.Valid {
			if t, err := time.Parse(time.RFC3339, expires.String); err == nil {
				k.ExpiresAt = &t
			}
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey revokes keyID, but only if it belongs to userID.
func (s *Service) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), keyID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// AuthenticateAPIKey resolves a general-purpose (ScopeAPI) bearer token into
// the user that owns it — used for ordinary REST API access. A key created
// with ScopeMCP is rejected here; see AuthenticateMCPKey for that path.
// Unlike sessions, an API key inherits whatever is_admin the user currently
// has — it is not a snapshot of permissions at creation time.
func (s *Service) AuthenticateAPIKey(ctx context.Context, token string) (*User, error) {
	return s.authenticateScopedKey(ctx, token, ScopeAPI)
}

// AuthenticateMCPKey is AuthenticateAPIKey's counterpart for the MCP
// endpoint — only a key explicitly created with ScopeMCP is accepted.
func (s *Service) AuthenticateMCPKey(ctx context.Context, token string) (*User, error) {
	return s.authenticateScopedKey(ctx, token, ScopeMCP)
}

func (s *Service) authenticateScopedKey(ctx context.Context, token, requiredScope string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	var (
		keyID, id, username, email, scope string
		isAdmin, totpEnabled              int
		expiresAt                         sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT k.id, u.id, u.username, u.email, u.is_admin, u.totp_enabled, k.scope, k.expires_at
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ? AND k.revoked_at IS NULL`, hashToken(token),
	).Scan(&keyID, &id, &username, &email, &isAdmin, &totpEnabled, &scope, &expiresAt)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if scope != requiredScope {
		return nil, ErrInvalidCredentials
	}
	if expiresAt.Valid {
		if t, err := time.Parse(time.RFC3339, expiresAt.String); err == nil && time.Now().UTC().After(t) {
			return nil, ErrInvalidCredentials
		}
	}
	// Best-effort — a failed timestamp bump must never fail the request it's riding along with.
	go func() {
		_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), keyID)
	}()
	return &User{ID: id, Username: username, Email: email, IsAdmin: isAdmin == 1, TOTPEnabled: totpEnabled == 1}, nil
}
