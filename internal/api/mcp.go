package api

import (
	"context"
	"net/http"
	"time"
)

// mcpAuth is the auth+authorization gate for the /mcp endpoint. It enforces,
// in order: (1) an admin has enabled MCP at all — off by default, since this
// endpoint lets external agents act on live infrastructure; (2) the caller
// presents an MCP-scoped API key (auth.ScopeMCP) — a general "api"-scoped
// key never works here, so connecting an MCP client always requires a token
// the user explicitly minted for that purpose (see apikeys.go). MCP clients
// (Claude Desktop, Claude Code) authenticate with a header, never a browser
// cookie, so this deliberately doesn't reuse requireAuth's cookie-or-key
// fallback — a bare bearer-token check with the 401/403 response shapes MCP
// clients expect.
func (s *Server) mcpAuth(next func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentSettings, err := s.loadAgentSettings(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !agentSettings.mcpEnabled {
			writeErrorCode(w, http.StatusForbidden, "mcp_disabled", "MCP is disabled for this Ferrum instance — an admin must enable it in Settings")
			return
		}

		key := bearerAPIKey(r)
		if key == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ferrum-mcp"`)
			writeErrorMsg(w, http.StatusUnauthorized, "an MCP API key is required — create one under Profile > API Keys")
			return
		}
		user, err := s.auth.AuthenticateMCPKey(r.Context(), key)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ferrum-mcp", error="invalid_token"`)
			writeErrorMsg(w, http.StatusUnauthorized, "invalid, revoked, or non-MCP-scoped API key")
			return
		}

		// A slow upstream Proxmox host (or a chain of tool calls a client
		// makes in sequence) can exceed the 30s global request timeout
		// applied in Router(); detach from that inherited deadline the same
		// way /ai/chat does, and apply a generous one of our own.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
		defer cancel()
		ctx = context.WithValue(ctx, userCtxKey, user)
		next(w, r.WithContext(ctx))
	}
}

// mcpHandler delegates the already-authenticated request to the MCP JSON-RPC
// dispatcher (internal/mcp) — see mcpAuth for how the user in context got there.
func (s *Server) mcpHandler(w http.ResponseWriter, r *http.Request) {
	s.mcp.ServeHTTP(w, r, userFromContext(r))
}
