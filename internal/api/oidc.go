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

// oidcStateCookie binds the /oidc/callback round-trip to the browser that
// started it: the state is planted in this cookie at /oidc/login and must
// come back with the callback's query state, so one user's login redirect
// can't be replayed against another's session (CSRF on the callback). The
// server-side state map remains the nonce store — the cookie is only the
// "same browser that started this" check.
const oidcStateCookie = "ferrum_oidc_state"

// oidcStateCookiePath scopes the state cookie to the OIDC route tree it
// belongs to; it's also the Path both set and clear must use to match.
const oidcStateCookiePath = "/api/v1/auth/oidc"

func (s *Server) setOIDCStateCookie(w http.ResponseWriter, r *http.Request, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // outlives the server-side state's 5-minute TTL slightly
	})
}

func (s *Server) clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    "",
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
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
	s.setOIDCStateCookie(w, r, state)
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
	cookieState, cookieErr := r.Cookie(oidcStateCookie)
	nonce, ok := consumeOIDCState(state)
	// The callback must be the same browser that started the login: the
	// state cookie has to be present and carry exactly the query state.
	// Both sides are consumed either way so a replayed URL never validates.
	if !ok || cookieErr != nil || cookieState.Value != state {
		s.clearOIDCStateCookie(w, r)
		slog.Warn("OIDC callback with unknown, expired, or mismatched state")
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}
	// The state has served its purpose — drop the cookie before continuing,
	// so it's gone regardless of how the rest of the flow ends.
	s.clearOIDCStateCookie(w, r)

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

	token, err := s.auth.CreateOIDCSession(r.Context(), user.ID, claims.RawIDToken, r.RemoteAddr, r.UserAgent())
	if err != nil {
		slog.Error("creating session for OIDC user", "error", err)
		http.Redirect(w, r, "/login?sso_error=1", http.StatusFound)
		return
	}
	slog.Info("login succeeded (sso)", "username", user.Username)
	auth.SetSessionCookie(w, token, s.cookieSecure(r), s.auth.SessionTTL())
	http.Redirect(w, r, "/", http.StatusFound)
}
