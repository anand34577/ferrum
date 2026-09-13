package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ferrum/internal/poller"
)

type alertRuleDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Metric       string  `json:"metric"`
	ConnectionID *string `json:"connectionId,omitempty"`
	Threshold    float64 `json:"threshold"`
	Severity     string  `json:"severity"`
	Enabled      bool    `json:"enabled"`
	CreatedAt    string  `json:"createdAt"`
}

func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	// Excludes the built-in "system-*" rules the alert evaluator seeds for
	// itself (certificate expiry, connection staleness — see
	// internal/poller/certificates.go): they back alert_instances the same
	// way a user-created rule does, but aren't meant to be edited or
	// deleted from this admin-facing list.
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, metric, connection_id, threshold, severity, enabled, created_at
		FROM alert_rules WHERE id NOT LIKE 'system-%' ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []alertRuleDTO{}
	for rows.Next() {
		var ru alertRuleDTO
		var enabled int
		if err := rows.Scan(&ru.ID, &ru.Name, &ru.Metric, &ru.ConnectionID, &ru.Threshold, &ru.Severity, &enabled, &ru.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		ru.Enabled = enabled == 1
		out = append(out, ru)
	}
	writeJSON(w, http.StatusOK, out)
}

type createAlertRuleRequest struct {
	Name         string  `json:"name"`
	Metric       string  `json:"metric"`
	ConnectionID *string `json:"connectionId,omitempty"`
	Threshold    float64 `json:"threshold"`
	Severity     string  `json:"severity"`
}

// Same shape as create — the edit form is the create form pre-filled.
type updateAlertRuleRequest = createAlertRuleRequest

var validMetrics = map[string]bool{
	"node_cpu": true, "node_mem": true, "node_disk": true,
	"guest_cpu": true, "guest_mem": true,
	"storage_usage": true,
}

func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	var req createAlertRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || !validMetrics[req.Metric] || req.Threshold <= 0 || req.Threshold > 100 {
		writeErrorMsg(w, http.StatusBadRequest, "name, a valid metric, and a threshold between 0 and 100 are required")
		return
	}
	if req.Severity != "warning" && req.Severity != "critical" {
		req.Severity = "warning"
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO alert_rules (id, name, metric, connection_id, threshold, severity, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		id, req.Name, req.Metric, req.ConnectionID, req.Threshold, req.Severity, now,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "alerts.rule.create", "alerts", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.HasPrefix(id, "system-") {
		writeErrorMsg(w, http.StatusBadRequest, "built-in alert rules can't be deleted")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "alert rule not found")
		return
	}
	s.audit(r, "alerts.rule.delete", "alerts", id)
	w.WriteHeader(http.StatusNoContent)
}

// updateAlertRule lets an admin edit a rule in place. Until this existed the
// only way to change a threshold was delete + recreate, which also resets the
// rule's history position and any alert instances pointing at it.
func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.HasPrefix(id, "system-") {
		writeErrorMsg(w, http.StatusBadRequest, "built-in alert rules can't be edited")
		return
	}
	var req updateAlertRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || !validMetrics[req.Metric] || req.Threshold <= 0 || req.Threshold > 100 {
		writeErrorMsg(w, http.StatusBadRequest, "name, a valid metric, and a threshold between 0 and 100 are required")
		return
	}
	if req.Severity != "warning" && req.Severity != "critical" {
		req.Severity = "warning"
	}
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE alert_rules SET name = ?, metric = ?, connection_id = ?, threshold = ?, severity = ? WHERE id = ?`,
		req.Name, req.Metric, req.ConnectionID, req.Threshold, req.Severity, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "alert rule not found")
		return
	}
	s.audit(r, "alerts.rule.update", "alerts", req.Name)
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

type alertInstanceDTO struct {
	ID             string  `json:"id"`
	RuleID         string  `json:"ruleId"`
	ConnectionID   string  `json:"connectionId"`
	ConnectionName string  `json:"connectionName"`
	ResourceName   string  `json:"resourceName"`
	Metric         string  `json:"metric"`
	Value          float64 `json:"value"`
	Threshold      float64 `json:"threshold"`
	Severity       string  `json:"severity"`
	Status         string  `json:"status"`
	TriggeredAt    string  `json:"triggeredAt"`
	UpdatedAt      string  `json:"updatedAt"`
	ResolvedAt     *string `json:"resolvedAt,omitempty"`
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	query := `SELECT id, rule_id, connection_id, connection_name, resource_name, metric, value, threshold, severity, status, triggered_at, updated_at, resolved_at FROM alert_instances`
	args := []any{}
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY triggered_at DESC LIMIT 500`

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []alertInstanceDTO{}
	for rows.Next() {
		var a alertInstanceDTO
		if err := rows.Scan(&a.ID, &a.RuleID, &a.ConnectionID, &a.ConnectionName, &a.ResourceName, &a.Metric, &a.Value, &a.Threshold, &a.Severity, &a.Status, &a.TriggeredAt, &a.UpdatedAt, &a.ResolvedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) alertsSummary(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT severity, COUNT(*) FROM alert_instances WHERE status = 'active' GROUP BY severity`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	summary := map[string]int{"warning": 0, "critical": 0}
	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		summary[severity] = count
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// connectionHealth reports whether each configured Proxmox connection
// answered on the alert evaluator's last poll — independent of alert_rules,
// so "is the server even reachable" always shows up regardless of whether
// the admin configured any metric threshold. Empty (not an error) when the
// evaluator hasn't ticked yet or isn't wired up.
func (s *Server) connectionHealth(w http.ResponseWriter, r *http.Request) {
	if s.evaluator == nil {
		writeJSON(w, http.StatusOK, []poller.ConnectionHealth{})
		return
	}
	health, err := s.evaluator.ConnectionHealthList(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (s *Server) silenceAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// The status guard keeps the update from "succeeding" against an
	// already-resolved (or nonexistent) alert, so zero rows reliably means
	// the client named something that can't be silenced.
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE alert_instances SET status = 'silenced', updated_at = ? WHERE id = ? AND status IN ('active', 'silenced')`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "alert not found")
		return
	}
	s.audit(r, "alerts.silence", "alerts", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// unSilenceAlert is the inverse of silenceAlert — the polling evaluator
// preserves the silenced status on re-fire (see poller/alerts.go's upsert),
// so flipping back to 'active' re-enables the alert even while its condition
// is still firing, which is exactly what "un-silence" has to mean.
func (s *Server) unSilenceAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE alert_instances SET status = 'active', updated_at = ? WHERE id = ? AND status = 'silenced'`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "silenced alert not found")
		return
	}
	s.audit(r, "alerts.unsilence", "alerts", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
