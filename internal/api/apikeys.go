package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/auth"
)

// createAPIKeyRequest is the body for POST /auth/apikeys. ExpiresInDays of 0
// (or omitted) means the key never expires. Scope defaults to "api" — pass
// "mcp" only when the key is meant for an MCP client (Claude, etc.); it will
// then work ONLY against /mcp, never the general REST API, and only when an
// admin has enabled MCP (see agent_settings / GET /admin/settings/agent).
type createAPIKeyRequest struct {
	Name          string `json:"name"`
	Scope         string `json:"scope,omitempty"`
	ExpiresInDays int    `json:"expiresInDays,omitempty"`
}

type createAPIKeyResponse struct {
	auth.APIKey
	Key string `json:"key"` // plaintext — shown once, never retrievable again
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	keys, err := s.auth.ListAPIKeys(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.ExpiresInDays < 0 {
		writeErrorMsg(w, http.StatusBadRequest, "expiresInDays must not be negative")
		return
	}
	scope := req.Scope
	if scope == "" {
		scope = auth.ScopeAPI
	}
	if scope != auth.ScopeAPI && scope != auth.ScopeMCP {
		writeErrorMsg(w, http.StatusBadRequest, `scope must be "api" or "mcp"`)
		return
	}
	if scope == auth.ScopeMCP || scope == auth.ScopeAPI {
		agentSettings, err := s.loadAgentSettings(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if scope == auth.ScopeMCP && !agentSettings.mcpEnabled {
			writeErrorCode(w, http.StatusForbidden, "mcp_disabled", "MCP is disabled for this Ferrum instance — ask an admin to enable it in Settings before creating an MCP token")
			return
		}
		if scope == auth.ScopeAPI && !agentSettings.apiEnabled {
			writeErrorCode(w, http.StatusForbidden, "api_disabled", "The REST API is disabled for this Ferrum instance — ask an admin to enable it in Settings before creating an API token")
			return
		}
	}

	u := userFromContext(r)
	plainKey, key, err := s.auth.CreateAPIKey(r.Context(), u.ID, req.Name, scope, req.ExpiresInDays)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "apikey.create", "security", key.Name+" ("+scope+")")
	writeJSON(w, http.StatusCreated, createAPIKeyResponse{APIKey: *key, Key: plainKey})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := chi.URLParam(r, "id")
	if err := s.auth.RevokeAPIKey(r.Context(), u.ID, id); err != nil {
		if err == auth.ErrAPIKeyNotFound {
			writeErrorMsg(w, http.StatusNotFound, "api key not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "apikey.revoke", "security", id)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}
