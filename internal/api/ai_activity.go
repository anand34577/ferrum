package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// sensitiveArgKeys are tool-argument key names whose values must never be
// persisted or echoed to the browser: tool args flow into ai_tool_calls via
// recordToolCall and into the live tool-call SSE envelope in ai_chat.go, and
// several tools (guest agent set-password, connection create/test, ...) carry
// plaintext credentials in their arguments.
var sensitiveArgKeys = map[string]bool{
	"password":   true,
	"cipassword": true,
	"secret":     true,
	"token":      true,
	"apikey":     true,
	"api_key":    true,
}

// redactSensitive returns v with every map value under a sensitive-looking
// key (case-insensitively — args come from the model, not from our own
// structs) replaced by "[redacted]". Everything else, including keys like
// sshPublicKey, is preserved.
func redactSensitive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if sensitiveArgKeys[strings.ToLower(k)] {
				out[k] = "[redacted]"
				continue
			}
			out[k] = redactSensitive(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = redactSensitive(item)
		}
		return out
	default:
		return v
	}
}

// redactToolArgs is redactSensitive for the raw JSON a tool call carries.
// Arguments that don't parse as JSON are returned unchanged — tool clients
// always send JSON objects, so there's nothing to key a redaction on.
func redactToolArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return args
	}
	var parsed any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return args
	}
	out, err := json.Marshal(redactSensitive(parsed))
	if err != nil {
		return args
	}
	return out
}

// recordToolCall implements mcp.RecordFunc — the single place every tool
// invocation (from the AI Assistant's own loop or an external MCP client)
// is logged, successful or not, read or write. This is what backs
// GET /ai/activity (a user's own trace) and GET /admin/ai/activity (every
// user's, for admins) — "what did the LLM actually do on my behalf" should
// always be answerable, not just inferable from side effects.
func (s *Server) recordToolCall(ctx context.Context, userID, source, tool string, args json.RawMessage, ok bool, errMsg string) {
	var argsCol sql.NullString
	if len(args) > 0 {
		argsCol = sql.NullString{String: string(redactToolArgs(args)), Valid: true}
	}
	var errCol sql.NullString
	if errMsg != "" {
		errCol = sql.NullString{String: errMsg, Valid: true}
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO ai_tool_calls (id, user_id, source, tool, args, ok, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), userID, source, tool, argsCol, boolToInt(ok), errCol, time.Now().UTC().Format(time.RFC3339),
	)
}

type toolCallDTO struct {
	ID        string `json:"id"`
	Username  string `json:"username,omitempty"` // populated only in the admin, cross-user view
	Source    string `json:"source"`
	Tool      string `json:"tool"`
	Args      string `json:"args,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"createdAt"`
}

const activityPageLimit = 200

// aiActivity returns the CALLING user's own tool-call trace — available to
// every authenticated user (not admin-gated), because "what has the AI done
// on my behalf" is a question about your own account, not a system-wide
// admin concern.
func (s *Server) aiActivity(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, source, tool, COALESCE(args, ''), ok, COALESCE(error, ''), created_at
		FROM ai_tool_calls WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, u.ID, activityPageLimit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []toolCallDTO{}
	for rows.Next() {
		var t toolCallDTO
		var ok int
		if err := rows.Scan(&t.ID, &t.Source, &t.Tool, &t.Args, &ok, &t.Error, &t.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		t.OK = ok == 1
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

// adminAIActivity is the system-wide counterpart — every user's tool calls,
// for admins to audit what agents have been doing across the fleet.
func (s *Server) adminAIActivity(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT c.id, COALESCE(u.username, 'deleted user'), c.source, c.tool, COALESCE(c.args, ''), c.ok, COALESCE(c.error, ''), c.created_at
		FROM ai_tool_calls c LEFT JOIN users u ON u.id = c.user_id
		ORDER BY c.created_at DESC LIMIT ?`, activityPageLimit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []toolCallDTO{}
	for rows.Next() {
		var t toolCallDTO
		var ok int
		if err := rows.Scan(&t.ID, &t.Username, &t.Source, &t.Tool, &t.Args, &ok, &t.Error, &t.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		t.OK = ok == 1
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}
