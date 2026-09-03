package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"ferrum/internal/auth"
)

// oidcPending bridges the redirect to the provider and the callback: it
// proves the callback belongs to a login we actually started (state) and
// that the returned ID token was minted for this exact login (nonce),
// mirroring the pendingLogin pattern used for the TOTP step-up in totp.go.
type oidcPending struct {
	nonce   string
	expires time.Time
}

var (
	oidcPendingMu sync.Mutex
	oidcPendingM  = map[string]oidcPending{}
)

func newOIDCState() (state, nonce string) {
	state, nonce = randomHex(16), randomHex(16)
	now := time.Now()
	oidcPendingMu.Lock()
	for id, p := range oidcPendingM {
		if now.After(p.expires.Add(10 * time.Minute)) {
			delete(oidcPendingM, id)
		}
	}
	oidcPendingM[state] = oidcPending{nonce: nonce, expires: now.Add(5 * time.Minute)}
	oidcPendingMu.Unlock()
	return state, nonce
}

func consumeOIDCState(state string) (nonce string, ok bool) {
	oidcPendingMu.Lock()
	defer oidcPendingMu.Unlock()
	p, found := oidcPendingM[state]
	delete(oidcPendingM, state)
	if !found || time.Now().After(p.expires) {
		return "", false
	}
	return p.nonce, true
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// oidcConfig tells the frontend whether to show an SSO button, without
// exposing any secret — the login page needs this before the user has
// authenticated, so it's intentionally not behind requireAuth.
func (s *Server) oidcConfig(w http.ResponseWriter, r *http.Request) {
	oidc := s.getOIDC()
	if oidc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "displayName": oidc.DisplayName()})
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	oidc := s.getOIDC()
	if oidc == nil {
		writeErrorMsg(w, http.StatusNotFound, "SSO is not configured")
		return
	}
	state, nonce := newOIDCState()
	authURL, err := oidc.AuthURL(state, nonce)
	if err != nil {
		slog.Error("building OIDC auth URL", "error", err)
		writeErrorMsg(w, http.StatusBadGateway, "could not reach the SSO provider")
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// oidcCallback completes the flow: verify state/nonce, exchange the code,
// verify the ID token, provision/link the local user, and start a normal
// Ferrum session exactly as password login would.
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	oidc := s.getOIDC()
	if oidc == nil {
		writeErrorMsg(w, http.StatusNotFound, "SSO is not configured")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		slog.Warn("OIDC provider returned an error", "error", errParam, "description", r.URL.Query().Get("error_description"))
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}

	state := r.URL.Query().Get("state")
	nonce, ok := consumeOIDCState(state)
	if !ok {
		slog.Warn("OIDC callback with unknown or expired state")
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	claims, err := oidc.Exchange(code, nonce)
	if err != nil {
		slog.Error("OIDC token exchange/verification failed", "error", err)
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}

	user, err := s.auth.FindOrCreateOIDCUser(r.Context(), claims.Subject, claims.Email, claims.PreferredUsername, claims.EmailVerified, oidc.AllowAutoProvision())
	if errors.Is(err, auth.ErrOIDCUserNotProvisioned) {
		slog.Warn("OIDC login refused: no matching local account and auto-provisioning is disabled", "subject", claims.Subject, "email", claims.Email)
		http.Redirect(w, r, "/login?sso_error=no_account", http.StatusFound)
		return
	}
	if err != nil {
		slog.Error("provisioning OIDC user", "error", err)
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}

	token, err := s.auth.CreateOIDCSession(r.Context(), user.ID, claims.RawIDToken)
	if err != nil {
		slog.Error("creating session for OIDC user", "error", err)
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}
	slog.Info("login succeeded (sso)", "username", user.Username)
	auth.SetSessionCookie(w, token, s.cookieSecure(r), s.auth.SessionTTL())
	http.Redirect(w, r, "/", http.StatusFound)
}
