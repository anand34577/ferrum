package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/auth"
)

// pendingLogins bridges authLogin (password verified) to authLoginTOTP (code
// verified) for accounts with 2FA enabled — mirrors the short-lived,
// single-use session-token pattern already used for console hand-off.
var (
	pendingLoginsMu sync.Mutex
	pendingLogins   = map[string]pendingLogin{}
)

type pendingLogin struct {
	userID  string
	expires time.Time
}

func newPendingLogin(userID string) string {
	token := uuid.NewString()
	now := time.Now()
	pendingLoginsMu.Lock()
	for id, p := range pendingLogins {
		if now.After(p.expires.Add(5 * time.Minute)) {
			delete(pendingLogins, id)
		}
	}
	pendingLogins[token] = pendingLogin{userID: userID, expires: now.Add(2 * time.Minute)}
	pendingLoginsMu.Unlock()
	return token
}

func consumePendingLogin(token string) (userID string, ok bool) {
	pendingLoginsMu.Lock()
	defer pendingLoginsMu.Unlock()
	p, found := pendingLogins[token]
	if found {
		delete(pendingLogins, token)
	}
	if !found || time.Now().After(p.expires) {
		return "", false
	}
	return p.userID, true
}

func (s *Server) totpStatus(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	enabled, remaining, err := s.auth.TOTPStatus(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "remainingRecoveryCodes": remaining})
}

func (s *Server) totpEnroll(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	enrollment, err := s.auth.EnrollTOTP(r.Context(), u.ID, u.Username)
	if err != nil {
		if errors.Is(err, auth.ErrTOTPAlreadyEnabled) {
			writeErrorMsg(w, http.StatusConflict, "two-factor authentication is already enabled")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":     enrollment.Secret,
		"otpauthUrl": enrollment.OTPAuthURL,
		"qrCodePng":  base64.StdEncoding.EncodeToString(enrollment.QRCodePNG),
	})
}

type confirmTOTPRequest struct {
	Code string `json:"code"`
}

func (s *Server) totpConfirm(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var req confirmTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	codes, err := s.auth.ConfirmTOTP(r.Context(), u.ID, req.Code)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidTOTPCode) {
			writeErrorMsg(w, http.StatusBadRequest, "invalid code — check your authenticator app and try again")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "auth.2fa.enable", "auth", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

type disableTOTPRequest struct {
	Password string `json:"password"`
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var req disableTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.auth.VerifyPassword(r.Context(), u.Username, req.Password); err != nil {
		writeErrorMsg(w, http.StatusUnauthorized, "incorrect password")
		return
	}
	if err := s.auth.DisableTOTP(r.Context(), u.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "auth.2fa.disable", "auth", u.Username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
