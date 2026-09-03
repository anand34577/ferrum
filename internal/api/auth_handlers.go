package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"ferrum/internal/auth"
)

func (s *Server) authSetupStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := s.auth.NeedsSetup(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": needsSetup})
}

type setupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Password) < 8 {
		writeErrorMsg(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Username == "" || req.Email == "" {
		writeErrorMsg(w, http.StatusBadRequest, "username and email are required")
		return
	}

	if _, err := s.auth.Bootstrap(r.Context(), req.Username, req.Email, req.Password); err != nil {
		if errors.Is(err, auth.ErrAlreadyBootstrapped) {
			writeErrorMsg(w, http.StatusConflict, "setup has already been completed")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	user, token, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	slog.Info("setup completed, first admin created", "username", user.Username)
	auth.SetSessionCookie(w, token, s.cookieSecure(r), s.auth.SessionTTL())
	writeJSON(w, http.StatusCreated, user)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	limiterKey := r.RemoteAddr + "|" + strings.ToLower(req.Username)
	if allowed, retryAfter := s.logins.Allowed(limiterKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		slog.Warn("login rate-limited", "remote", r.RemoteAddr)
		writeErrorMsg(w, http.StatusTooManyRequests, "too many failed attempts — try again later")
		return
	}

	user, err := s.auth.VerifyPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		s.logins.RecordFailure(limiterKey)
		slog.Warn("login failed", "remote", r.RemoteAddr)
		writeErrorMsg(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.logins.RecordSuccess(limiterKey)

	if user.TOTPEnabled {
		pendingToken := newPendingLogin(user.ID)
		writeJSON(w, http.StatusOK, map[string]string{"requiresTotp": "true", "pendingToken": pendingToken})
		return
	}

	token, err := s.auth.CreateSession(r.Context(), user.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	slog.Info("login succeeded", "username", user.Username)
	auth.SetSessionCookie(w, token, s.cookieSecure(r), s.auth.SessionTTL())
	writeJSON(w, http.StatusOK, user)
}

type loginTOTPRequest struct {
	PendingToken string `json:"pendingToken"`
	Code         string `json:"code"`
}

// authLoginTOTP completes the two-step login started by authLogin once the
// caller has proven possession of the user's TOTP device (or a recovery code).
func (s *Server) authLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req loginTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := consumePendingLogin(req.PendingToken)
	if !ok {
		writeErrorMsg(w, http.StatusUnauthorized, "login session expired — sign in again")
		return
	}

	// The pending token is single-use, so each attempt already costs a full
	// password login; this limiter is defense in depth for the same key.
	totpKey := "totp|" + userID
	if allowed, retryAfter := s.logins.Allowed(totpKey); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
		writeErrorMsg(w, http.StatusTooManyRequests, "too many failed attempts — try again later")
		return
	}
	if err := s.auth.VerifyTOTPStep(r.Context(), userID, req.Code); err != nil {
		s.logins.RecordFailure(totpKey)
		slog.Warn("totp verification failed", "userId", userID, "remote", r.RemoteAddr)
		writeErrorMsg(w, http.StatusUnauthorized, "invalid authentication code")
		return
	}
	s.logins.RecordSuccess(totpKey)

	token, err := s.auth.CreateSession(r.Context(), userID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	user, err := s.auth.Authenticate(r.Context(), token)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	slog.Info("login succeeded (totp)", "username", user.Username)
	auth.SetSessionCookie(w, token, s.cookieSecure(r), s.auth.SessionTTL())
	writeJSON(w, http.StatusOK, user)
}

// authLogout revokes the Ferrum session and, only when the admin has opted
// in (Settings → SSO → "Also sign out at the identity provider") and the
// provider supports RP-Initiated Logout, tells the frontend where to send
// the browser next to also end the session at the identity provider (SLO).
// Opt-in, not automatic: the provider must have Ferrum's post-logout
// redirect URL registered first (e.g. Keycloak's "Valid post logout
// redirect URIs") or it rejects the redirect with invalid_redirect_uri —
// enabling this unconditionally for every existing SSO deployment would
// have broken sign-out for anyone who hadn't done that yet.
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	var idToken string
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		idToken, _ = s.auth.Logout(r.Context(), cookie.Value)
	}
	if u := userFromContext(r); u != nil {
		slog.Info("logout", "username", u.Username)
	}
	auth.ClearSessionCookie(w, s.cookieSecure(r))

	if idToken != "" {
		if oidc := s.getOIDC(); oidc != nil && oidc.SingleLogoutEnabled() {
			if endSessionURL, ok := oidc.EndSessionURL(idToken); ok {
				writeJSON(w, http.StatusOK, map[string]string{"logoutUrl": endSessionURL})
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFromContext(r))
}
