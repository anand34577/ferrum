// Package auth implements local username/password auth with bcrypt hashing
// and server-side sessions backed by an HTTP-only cookie.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

const (
	SessionCookieName = "ferrum_session"
	// defaultSessionTTL seeds Service.sessionTTL until (and unless) an admin
	// overrides it from the Settings UI — see SetSessionTTL.
	defaultSessionTTL = 30 * 24 * time.Hour
)

var (
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrAlreadyBootstrapped = errors.New("an admin user already exists")
	// ErrOIDCUserNotProvisioned is returned by FindOrCreateOIDCUser when no
	// local account matches the SSO identity and auto-provisioning is
	// disabled (OIDCConfig.AllowAutoProvision) — the admin wants SSO
	// restricted to accounts that already exist.
	ErrOIDCUserNotProvisioned = errors.New("no local account exists for this SSO identity, and auto-provisioning new accounts is disabled")
)

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	IsAdmin     bool   `json:"isAdmin"`
	TOTPEnabled bool   `json:"totpEnabled"`
}

type Service struct {
	db *store.DB
	// secrets encrypts TOTP secrets at rest; nil means legacy plaintext
	// storage (readers fall back to the raw column value either way — see
	// decryptStoredSecret in totp.go).
	secrets *secrets.Box

	sessionTTLMu sync.RWMutex
	sessionTTL   time.Duration

	// bootstrapMu serializes Bootstrap so two concurrent first-run
	// /auth/setup requests can't both pass the "no user yet" check and
	// both create an admin account.
	bootstrapMu sync.Mutex
}

func NewService(db *store.DB, box *secrets.Box) *Service {
	return &Service{db: db, secrets: box, sessionTTL: defaultSessionTTL}
}

// SetSessionTTL changes how long newly-created sessions live — applies to
// logins from this point on; sessions already issued keep whatever TTL was
// in effect when they were created (their expiry is stored, not recomputed).
func (s *Service) SetSessionTTL(d time.Duration) {
	s.sessionTTLMu.Lock()
	s.sessionTTL = d
	s.sessionTTLMu.Unlock()
}

func (s *Service) getSessionTTL() time.Duration {
	s.sessionTTLMu.RLock()
	defer s.sessionTTLMu.RUnlock()
	return s.sessionTTL
}

// SessionTTL exposes the current TTL for SetSessionCookie callers, which
// need to hand the browser a matching cookie lifetime.
func (s *Service) SessionTTL() time.Duration {
	return s.getSessionTTL()
}

// NeedsSetup reports whether no user exists yet, meaning the frontend should
// show the first-run setup wizard instead of the login page.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

// Bootstrap creates the first admin user. It refuses to run if any user already exists.
func (s *Service) Bootstrap(ctx context.Context, username, email, password string) (*User, error) {
	// Serialize check-then-create: without this, two concurrent requests
	// could both observe an empty users table and both create an admin.
	s.bootstrapMu.Lock()
	defer s.bootstrapMu.Unlock()

	needsSetup, err := s.NeedsSetup(ctx)
	if err != nil {
		return nil, err
	}
	if !needsSetup {
		return nil, ErrAlreadyBootstrapped
	}
	return s.createUser(ctx, username, email, password, true)
}

func (s *Service) createUser(ctx context.Context, username, email, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, username, email, string(hash), boolToInt(isAdmin), now, now,
	); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, Email: email, IsAdmin: isAdmin}, nil
}

// VerifyPassword checks username/password and returns the matching user
// without creating a session — the caller decides whether a second TOTP
// step is required (User.TOTPEnabled) before calling CreateSession.
// dummyBcryptHash is compared against when the username doesn't exist so
// "unknown user" and "wrong password" take the same time — otherwise the
// missing bcrypt pass leaks valid usernames through response timing.
var dummyBcryptHash = func() []byte {
	// bcrypt hash of a random never-used password
	return []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
}()

func (s *Service) VerifyPassword(ctx context.Context, username, password string) (*User, error) {
	var (
		id, uname, email, hash string
		isAdmin, totpEnabled   int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, is_admin, totp_enabled FROM users WHERE username = ?`,
		username,
	).Scan(&id, &uname, &email, &hash, &isAdmin, &totpEnabled)
	if err != nil {
		// Spend the same bcrypt work a real user lookup would, then fail.
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &User{ID: id, Username: uname, Email: email, IsAdmin: isAdmin == 1, TOTPEnabled: totpEnabled == 1}, nil
}

// FindOrCreateOIDCUser resolves a verified OIDC identity to a local user:
// an existing user already linked to this subject, an existing local user
// with the same email (linked on first SSO login), or — failing both — a
// newly provisioned account. New OIDC accounts are never admins; promote
// them from the Users page like any other account. They get a random,
// never-disclosed password hash so the (unused) password login path stays
// closed for them, without needing a schema change to make password_hash
// nullable.
//
// emailVerified gates the risky paths: an unverified email address is never
// used to link into (or name-collide with) an existing account, because a
// hostile IdP account claiming someone else's address would otherwise take
// that account over. Unverified users get a placeholder email instead.
func (s *Service) FindOrCreateOIDCUser(ctx context.Context, subject, email, preferredUsername string, emailVerified, allowAutoProvision bool) (*User, error) {
	var (
		id, uname, mail      string
		isAdmin, totpEnabled int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, is_admin, totp_enabled FROM users WHERE oidc_subject = ?`, subject,
	).Scan(&id, &uname, &mail, &isAdmin, &totpEnabled)
	if err == nil {
		return &User{ID: id, Username: uname, Email: mail, IsAdmin: isAdmin == 1, TOTPEnabled: totpEnabled == 1}, nil
	}

	if email != "" && emailVerified {
		err = s.db.QueryRowContext(ctx,
			`SELECT id, username, email, is_admin, totp_enabled FROM users WHERE email = ?`, email,
		).Scan(&id, &uname, &mail, &isAdmin, &totpEnabled)
		if err == nil {
			if _, err := s.db.ExecContext(ctx, `UPDATE users SET oidc_subject = ? WHERE id = ?`, subject, id); err != nil {
				return nil, err
			}
			return &User{ID: id, Username: uname, Email: mail, IsAdmin: isAdmin == 1, TOTPEnabled: totpEnabled == 1}, nil
		}
	} else if email != "" {
		// A local account already holds this (unverified) address: refuse
		// rather than silently creating a second user for the same email.
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&exists); err == nil && exists > 0 {
			return nil, fmt.Errorf("SSO account email is not verified by the provider and collides with an existing local account")
		}
	}

	if !allowAutoProvision {
		return nil, ErrOIDCUserNotProvisioned
	}

	username := preferredUsername
	if username == "" {
		username = email
	}
	if username == "" {
		username = "sso-" + subject
	}
	username = s.uniqueUsername(ctx, username)
	if email == "" || !emailVerified {
		// users.email is NOT NULL UNIQUE; synthesize a placeholder that can't collide.
		email = username + "@sso.local"
	}

	randomPassword := make([]byte, 32)
	if _, err := rand.Read(randomPassword); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword(randomPassword, bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	id = uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password_hash, is_admin, oidc_subject, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
		id, username, email, string(hash), subject, now, now,
	); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, Email: email, IsAdmin: false}, nil
}

func (s *Service) uniqueUsername(ctx context.Context, base string) string {
	candidate := base
	for i := 2; i < 100; i++ {
		var exists int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, candidate).Scan(&exists)
		if exists == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return base + "-" + uuid.NewString()[:8]
}

// Login verifies credentials and, on success, creates a new session and
// returns the session token to be set as a cookie. It is a convenience
// wrapper for the common case (no TOTP) — see VerifyPassword + CreateSession
// for the two-step flow used when TOTP is enabled. ip/userAgent are stored
// on the session row so the account owner can later recognize (and revoke)
// it from the Sessions panel — see ListSessions.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (*User, string, error) {
	user, err := s.VerifyPassword(ctx, username, password)
	if err != nil {
		return nil, "", err
	}
	token, err := s.CreateSession(ctx, user.ID, ip, userAgent)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// CreateSession mints a new session for an already-authenticated user.
// Opportunistically purges expired rows so the table doesn't grow forever.
func (s *Service) CreateSession(ctx context.Context, userID, ip, userAgent string) (string, error) {
	return s.createSession(ctx, userID, "", ip, userAgent)
}

// CreateOIDCSession is CreateSession plus the verified ID token from the SSO
// login, kept so Logout can hand it back to the provider as id_token_hint
// for RP-Initiated Logout (see OIDCClient.EndSessionURL).
func (s *Service) CreateOIDCSession(ctx context.Context, userID, idToken, ip, userAgent string) (string, error) {
	return s.createSession(ctx, userID, idToken, ip, userAgent)
}

func (s *Service) createSession(ctx context.Context, userID, oidcIDToken, ip, userAgent string) (string, error) {
	token := randomToken()
	now := time.Now().UTC()
	var idTokenCol any
	if oidcIDToken != "" {
		idTokenCol = oidcIDToken
	}
	nowStr := now.Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, ip, user_agent, created_at, expires_at, oidc_id_token, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), userID, hashToken(token), nullableString(ip), nullableString(userAgent), nowStr, now.Add(s.getSessionTTL()).Format(time.RFC3339), idTokenCol, nowStr,
	); err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, nowStr)
	return token, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Authenticate resolves a session cookie value into the user that owns it.
// On success it opportunistically bumps the session's last_seen_at so the
// Sessions panel (ListSessions) reflects recent activity — best-effort,
// fire-and-forget like AuthenticateAPIKey's last_used_at bump, since a
// failed timestamp write must never fail the request riding along with it.
func (s *Service) Authenticate(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	var (
		id, username, email  string
		isAdmin, totpEnabled int
		expiresAt            string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.is_admin, u.totp_enabled, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, hashToken(token),
	).Scan(&id, &username, &email, &isAdmin, &totpEnabled, &expiresAt)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().UTC().After(expiry) {
		// Expired (or corrupt) session — drop it so the row can't accumulate.
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
		return nil, ErrInvalidCredentials
	}
	go func() {
		_, _ = s.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, time.Now().UTC().Format(time.RFC3339), hashToken(token))
	}()
	return &User{ID: id, Username: username, Email: email, IsAdmin: isAdmin == 1, TOTPEnabled: totpEnabled == 1}, nil
}

// Logout revokes the session and reports the OIDC ID token it was created
// with, if any — the caller (the /auth/logout handler) uses that as
// id_token_hint to also end the session at the identity provider.
func (s *Service) Logout(ctx context.Context, token string) (oidcIDToken string, err error) {
	var idToken sql.NullString
	// Read-then-delete rather than DELETE...RETURNING: sqlite added RETURNING
	// only in 3.35+, and this isn't hot-path enough to matter.
	_ = s.db.QueryRowContext(ctx, `SELECT oidc_id_token FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&idToken)
	_, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return idToken.String, err
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SetSessionCookie writes the session cookie on the response. ttl should
// match whatever TTL the session was actually created with (Service.SessionTTL()).
func SetSessionCookie(w http.ResponseWriter, token string, secure bool, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(ttl),
	})
}

// ClearSessionCookie expires the session cookie on the response.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
