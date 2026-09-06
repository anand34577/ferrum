package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"ferrum/internal/auth"
	"ferrum/internal/connections"
	"ferrum/internal/store"
)

// maxRequestBody bounds an MCP JSON-RPC request body — tool arguments are
// always small, so this is generous headroom, not a real limit.
const maxRequestBody = 1 << 20

// AuditFunc records a state-changing MCP tool call the same way the REST API
// audits its equivalent action — see internal/api's audit() and where New is
// called for the concrete implementation. Read-only tools (list_*, get_*)
// aren't audited there, same as GET requests never are on the REST side —
// for a complete trace of every call (read and write), see RecordFunc.
type AuditFunc func(ctx context.Context, userID, action, category, target string)

// RecordFunc logs every tool invocation — successful or not, read or write —
// to a per-user, user-visible activity trail (see internal/api's
// GET /ai/activity and /admin/ai/activity), so a user can always see exactly
// what an LLM looked at or did on their behalf, and an admin can review the
// same across every user. source distinguishes the AI Assistant's own
// tool-calling loop ("chat") from an external MCP client ("mcp").
type RecordFunc func(ctx context.Context, userID, source, tool string, args json.RawMessage, ok bool, errMsg string)

// Server dispatches MCP JSON-RPC requests over the Streamable HTTP
// transport: a single POST endpoint, one JSON-RPC request/response body per
// call (no persistent SSE stream — Ferrum's tool calls are all quick request/
// response operations, so the simpler transport variant is sufficient).
type Server struct {
	resolver *connections.Resolver
	db       *store.DB
	audit    AuditFunc
	record   RecordFunc
}

func New(resolver *connections.Resolver, db *store.DB, audit AuditFunc, record RecordFunc) *Server {
	return &Server{resolver: resolver, db: db, audit: audit, record: record}
}

// Handle processes one JSON-RPC request. user is the identity resolved from
// the caller's API key (see internal/api's mcpAuth) — passed through to
// tools so mutating ones (guest_power_action) can enforce admin-only, the
// same rule requireAdminForMutations applies to the regular REST API.
func (s *Server) Handle(ctx context.Context, user *auth.User, body []byte) response {
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		return errorResponse(nil, -32700, "parse error")
	}
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, -32600, "invalid request: jsonrpc must be \"2.0\"")
	}

	switch req.Method {
	case "initialize":
		return resultResponse(req.ID, initializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      serverInfo{Name: "ferrum", Version: "1.0.0"},
		})
	case "notifications/initialized", "ping":
		// Notifications carry no id and expect no response body; callers of
		// Handle over HTTP should treat a nil-ID response as "204 No Content".
		return response{JSONRPC: "2.0"}
	case "tools/list":
		return resultResponse(req.ID, toolsListResult{Tools: toolDefinitions})
	case "tools/call":
		return s.handleToolCall(ctx, user, req)
	default:
		return errorResponse(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleToolCall(ctx context.Context, user *auth.User, req request) response {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "invalid params")
	}
	result := s.callTool(ctx, user, "mcp", params.Name, params.Arguments)
	return resultResponse(req.ID, result)
}

// callTool is the single place every tool invocation passes through,
// regardless of caller (MCP JSON-RPC here, or the AI Assistant's
// function-calling loop via the exported CallTool) — so there is exactly
// one spot recording the full activity trail.
func (s *Server) callTool(ctx context.Context, user *auth.User, source, name string, args json.RawMessage) toolCallResult {
	fn, ok := toolHandlers[name]
	if !ok {
		result := errorResult("unknown tool: " + name)
		s.recordCall(ctx, user, source, name, args, result)
		return result
	}
	result := fn(ctx, s, user, args)
	s.recordCall(ctx, user, source, name, args, result)
	return result
}

func (s *Server) recordCall(ctx context.Context, user *auth.User, source, name string, args json.RawMessage, result toolCallResult) {
	if s.record == nil || user == nil {
		return
	}
	errMsg := ""
	if result.IsError && len(result.Content) > 0 {
		errMsg = result.Content[0].Text
	}
	s.record(ctx, user.ID, source, name, args, !result.IsError, errMsg)
}

// ServeHTTP is a convenience wrapper for callers that just want to hand off
// the raw HTTP request; internal/api wires this in directly so its own
// bearer-token auth (mcpAuth) can run first and hand Handle the resolved user.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request, user *auth.User) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}
	resp := s.Handle(r.Context(), user, body)
	w.Header().Set("Content-Type", "application/json")
	if resp.ID == nil && resp.Result == nil && resp.Error == nil {
		// A notification (e.g. notifications/initialized) — no response body.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}
