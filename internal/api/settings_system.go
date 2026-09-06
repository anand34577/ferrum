package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// systemRow is the single (id=1) system_settings row — general operational
// knobs that used to be hardcoded Go constants (a rebuild away from
// changing), same single-row pattern as security_settings/agent_settings.
type systemRow struct {
	alertPollSeconds   int
	corsAllowedOrigins string // comma-separated; empty means CORS stays off
}

func defaultSystemRow() systemRow {
	return systemRow{alertPollSeconds: 60}
}

func (s *Server) loadSystemRow(ctx context.Context) (systemRow, error) {
	row := defaultSystemRow()
	err := s.db.QueryRowContext(ctx, `SELECT alert_poll_seconds, cors_allowed_origins FROM system_settings WHERE id = 1`).
		Scan(&row.alertPollSeconds, &row.corsAllowedOrigins)
	if err == sql.ErrNoRows {
		return defaultSystemRow(), nil
	}
	if err != nil {
		return systemRow{}, err
	}
	return row, nil
}

func (s *Server) saveSystemRow(ctx context.Context, row systemRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings (id, alert_poll_seconds, cors_allowed_origins, updated_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET alert_poll_seconds = excluded.alert_poll_seconds, cors_allowed_origins = excluded.cors_allowed_origins, updated_at = excluded.updated_at`,
		row.alertPollSeconds, row.corsAllowedOrigins, time.Now().UTC().Format(time.RFC3339))
	return err
}

// applySystemRow pushes row into the running alert poller and the live CORS
// origin allow-list. The evaluator push is a no-op if the evaluator hasn't
// started its ticker yet (e.g. mid-bootstrap) — main.go reads
// AlertPollInterval directly to seed that first Run call instead.
func (s *Server) applySystemRow(row systemRow) {
	if s.evaluator != nil {
		s.evaluator.SetInterval(time.Duration(row.alertPollSeconds) * time.Second)
	}
	s.SetCORSOrigins(parseCORSOrigins(row.corsAllowedOrigins))
}

func parseCORSOrigins(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AlertPollInterval is read once at startup (main.go) to seed the alert
// evaluator's initial ticker, before BootstrapSettings' applySystemRow has
// anything running yet to apply it to.
func (s *Server) AlertPollInterval(ctx context.Context) time.Duration {
	row, err := s.loadSystemRow(ctx)
	if err != nil {
		return time.Duration(defaultSystemRow().alertPollSeconds) * time.Second
	}
	return time.Duration(row.alertPollSeconds) * time.Second
}

type systemSettingsResponse struct {
	AlertPollSeconds   int    `json:"alertPollSeconds"`
	CorsAllowedOrigins string `json:"corsAllowedOrigins"`
}

func toSystemSettingsResponse(row systemRow) systemSettingsResponse {
	return systemSettingsResponse{AlertPollSeconds: row.alertPollSeconds, CorsAllowedOrigins: row.corsAllowedOrigins}
}

func (s *Server) getSystemSettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadSystemRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemSettingsResponse(row))
}

type systemSettingsPatch struct {
	AlertPollSeconds   *int    `json:"alertPollSeconds"`
	CorsAllowedOrigins *string `json:"corsAllowedOrigins"`
}

func (s *Server) putSystemSettings(w http.ResponseWriter, r *http.Request) {
	var patch systemSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadSystemRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.AlertPollSeconds != nil {
		if *patch.AlertPollSeconds < 10 || *patch.AlertPollSeconds > 3600 {
			writeErrorMsg(w, http.StatusBadRequest, "alert poll interval must be between 10 and 3600 seconds")
			return
		}
		row.alertPollSeconds = *patch.AlertPollSeconds
	}
	if patch.CorsAllowedOrigins != nil {
		row.corsAllowedOrigins = strings.TrimSpace(*patch.CorsAllowedOrigins)
	}

	if err := s.saveSystemRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.applySystemRow(row)
	s.audit(r, "settings.system", "settings", "updated")
	writeJSON(w, http.StatusOK, toSystemSettingsResponse(row))
}
