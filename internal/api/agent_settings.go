package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// agentSettingsRow is the single (id=1) agent_settings row governing the AI
// Assistant's tool-calling loop and whether the MCP endpoint is reachable at
// all. Both are admin-controlled and safe-by-default: MCP starts disabled,
// and the tool-call ceiling has a generous default rather than being a
// hardcoded Go constant that would need a code change to raise.
type agentSettingsRow struct {
	mcpEnabled        bool
	apiEnabled        bool
	maxToolIterations int
}

const defaultMaxToolIterations = 8

func (s *Server) loadAgentSettings(ctx context.Context) (agentSettingsRow, error) {
	var row agentSettingsRow
	var mcpEnabled, apiEnabled int
	err := s.db.QueryRowContext(ctx, `SELECT mcp_enabled, api_enabled, max_tool_iterations FROM agent_settings WHERE id = 1`).
		Scan(&mcpEnabled, &apiEnabled, &row.maxToolIterations)
	if err == sql.ErrNoRows {
		return agentSettingsRow{mcpEnabled: false, apiEnabled: true, maxToolIterations: defaultMaxToolIterations}, nil
	}
	if err != nil {
		return agentSettingsRow{}, err
	}
	row.mcpEnabled = mcpEnabled == 1
	row.apiEnabled = apiEnabled == 1
	return row, nil
}

func (s *Server) saveAgentSettings(ctx context.Context, row agentSettingsRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_settings (id, mcp_enabled, api_enabled, max_tool_iterations, updated_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET mcp_enabled = excluded.mcp_enabled, api_enabled = excluded.api_enabled, max_tool_iterations = excluded.max_tool_iterations, updated_at = excluded.updated_at`,
		boolToInt(row.mcpEnabled), boolToInt(row.apiEnabled), row.maxToolIterations, time.Now().UTC().Format(time.RFC3339))
	return err
}

type agentSettingsResponse struct {
	McpEnabled        bool `json:"mcpEnabled"`
	ApiEnabled        bool `json:"apiEnabled"`
	MaxToolIterations int  `json:"maxToolIterations"`
}

// getAgentStatus is the trimmed, non-admin-gated counterpart to
// getAgentSettings — every user needs to know whether MCP/API are enabled
// before they can decide what kind of key to create, but only admins can
// see/change the full settings (including the tool-iteration ceiling).
func (s *Server) getAgentStatus(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadAgentSettings(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"mcpEnabled": row.mcpEnabled, "apiEnabled": row.apiEnabled})
}

func (s *Server) getAgentSettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadAgentSettings(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, agentSettingsResponse{McpEnabled: row.mcpEnabled, ApiEnabled: row.apiEnabled, MaxToolIterations: row.maxToolIterations})
}

type agentSettingsPatch struct {
	McpEnabled        *bool `json:"mcpEnabled"`
	ApiEnabled        *bool `json:"apiEnabled"`
	MaxToolIterations *int  `json:"maxToolIterations"`
}

func (s *Server) putAgentSettings(w http.ResponseWriter, r *http.Request) {
	var patch agentSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadAgentSettings(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.McpEnabled != nil {
		row.mcpEnabled = *patch.McpEnabled
	}
	if patch.ApiEnabled != nil {
		row.apiEnabled = *patch.ApiEnabled
	}
	if patch.MaxToolIterations != nil {
		if *patch.MaxToolIterations < 1 || *patch.MaxToolIterations > 50 {
			writeErrorMsg(w, http.StatusBadRequest, "maxToolIterations must be between 1 and 50")
			return
		}
		row.maxToolIterations = *patch.MaxToolIterations
	}

	if err := s.saveAgentSettings(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "settings.agent", "settings", "updated")
	writeJSON(w, http.StatusOK, agentSettingsResponse{McpEnabled: row.mcpEnabled, ApiEnabled: row.apiEnabled, MaxToolIterations: row.maxToolIterations})
}
