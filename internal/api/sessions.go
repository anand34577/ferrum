package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/auth"
)

// currentSessionToken reads the raw session cookie value off the request —
// used only to mark "this device" in a Sessions listing and to exclude it
// from a "log out everywhere else" sweep. Empty when the caller
// authenticated with an API key instead of a cookie.
func currentSessionToken(r *http.Request) string {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// --- self-service: /profile/sessions ---

func (s *Server) listMySessions(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	sessions, err := s.auth.ListSessions(r.Context(), u.ID, currentSessionToken(r))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) revokeMySession(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := chi.URLParam(r, "id")
	if err := s.auth.RevokeSession(r.Context(), id, u.ID); err != nil {
		if err == auth.ErrSessionNotFound {
			writeErrorMsg(w, http.StatusNotFound, "session not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "sessions.revoke", "security", id)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// revokeMyOtherSessions signs the account out of every device except the
// one making this request — the "log out all other sessions" action.
func (s *Server) revokeMyOtherSessions(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	token := currentSessionToken(r)
	if token == "" {
		// Called via an API key rather than the browser cookie — there is
		// no "current session" to keep, so this action is ambiguous.
		writeErrorMsg(w, http.StatusBadRequest, "no active session cookie on this request")
		return
	}
	n, err := s.auth.RevokeOtherSessions(r.Context(), u.ID, token)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "sessions.revoke_others", "security", u.Username)
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}

// --- admin: /users/{id}/sessions ---

func (s *Server) listUserSessions(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	sessions, err := s.auth.ListSessions(r.Context(), userID, "")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) revokeUserSession(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	sessionID := chi.URLParam(r, "sessionId")
	// Scoped to userID even on the admin path: a session id from a
	// different account is reported not-found rather than silently
	// revoking the wrong user's session if the two path params disagree.
	if err := s.auth.RevokeSession(r.Context(), sessionID, userID); err != nil {
		if err == auth.ErrSessionNotFound {
			writeErrorMsg(w, http.StatusNotFound, "session not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "sessions.admin_revoke", "admin", userID)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	n, err := s.auth.RevokeAllSessions(r.Context(), userID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "sessions.admin_revoke_all", "admin", userID)
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}
