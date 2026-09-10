package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// lifecycleRow is the single (id=1) lifecycle_settings row — the fleet-wide
// snapshot retention window and whether the retention sweep (internal/poller)
// is allowed to actually delete anything yet. Same single-row pattern as
// system_settings/security_settings.
type lifecycleRow struct {
	retentionDays int
	enforce       bool
}

func defaultLifecycleRow() lifecycleRow {
	return lifecycleRow{} // retentionDays: 0 (no fleet-wide default), enforce: false (dry-run only)
}

func (s *Server) loadLifecycleRow(ctx context.Context) (lifecycleRow, error) {
	row := defaultLifecycleRow()
	var enforce int
	err := s.db.QueryRowContext(ctx, `SELECT retention_days, enforce FROM lifecycle_settings WHERE id = 1`).
		Scan(&row.retentionDays, &enforce)
	if err == sql.ErrNoRows {
		return defaultLifecycleRow(), nil
	}
	if err != nil {
		return lifecycleRow{}, err
	}
	row.enforce = enforce == 1
	return row, nil
}

func (s *Server) saveLifecycleRow(ctx context.Context, row lifecycleRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO lifecycle_settings (id, retention_days, enforce, updated_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET retention_days = excluded.retention_days, enforce = excluded.enforce, updated_at = excluded.updated_at`,
		row.retentionDays, boolToInt(row.enforce), time.Now().UTC().Format(time.RFC3339))
	return err
}

type lifecycleSettingsResponse struct {
	RetentionDays int  `json:"retentionDays"`
	Enforce       bool `json:"enforce"`
}

func toLifecycleSettingsResponse(row lifecycleRow) lifecycleSettingsResponse {
	return lifecycleSettingsResponse{RetentionDays: row.retentionDays, Enforce: row.enforce}
}

func (s *Server) getLifecycleSettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadLifecycleRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toLifecycleSettingsResponse(row))
}

type lifecycleSettingsPatch struct {
	RetentionDays *int  `json:"retentionDays"`
	Enforce       *bool `json:"enforce"`
}

func (s *Server) putLifecycleSettings(w http.ResponseWriter, r *http.Request) {
	var patch lifecycleSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadLifecycleRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.RetentionDays != nil {
		if *patch.RetentionDays < 0 || *patch.RetentionDays > 3650 {
			writeErrorMsg(w, http.StatusBadRequest, "retention window must be between 0 (disabled) and 3650 days")
			return
		}
		row.retentionDays = *patch.RetentionDays
	}
	if patch.Enforce != nil {
		row.enforce = *patch.Enforce
	}

	if err := s.saveLifecycleRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "settings.lifecycle", "settings", "updated")
	writeJSON(w, http.StatusOK, toLifecycleSettingsResponse(row))
}

// --- Action log ---

type lifecycleActionDTO struct {
	ID             string  `json:"id"`
	ConnectionID   string  `json:"connectionId"`
	ConnectionName string  `json:"connectionName"`
	GuestType      string  `json:"guestType"`
	Node           string  `json:"node"`
	VMID           int     `json:"vmid"`
	GuestName      string  `json:"guestName"`
	Action         string  `json:"action"`
	Detail         string  `json:"detail"`
	DryRun         bool    `json:"dryRun"`
	Status         string  `json:"status"`
	Error          *string `json:"error,omitempty"`
	CreatedAt      string  `json:"createdAt"`
}

// lifecycleActions lists the retention sweep's action log, most recent
// first — simple limit/offset pagination is enough here, there's no need for
// cursor pagination on an internal audit trail like this.
func (s *Server) lifecycleActions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, connection_id, connection_name, guest_type, node, vmid, guest_name, action, detail, dry_run, status, error, created_at
		FROM lifecycle_actions ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []lifecycleActionDTO{}
	for rows.Next() {
		var a lifecycleActionDTO
		var dryRun int
		if err := rows.Scan(&a.ID, &a.ConnectionID, &a.ConnectionName, &a.GuestType, &a.Node, &a.VMID, &a.GuestName,
			&a.Action, &a.Detail, &dryRun, &a.Status, &a.Error, &a.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		a.DryRun = dryRun == 1
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
