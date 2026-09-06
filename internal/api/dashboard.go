package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// A user can keep several named dashboards (e.g. "Ops overview", "Capacity
// planning") instead of one shared layout — each is a fully independent
// widget grid, all under this same per-user table. defaultDashboardLayout
// seeds a brand-new account's first one.
// layoutVersion 2 halved the grid's row unit (64px rows -> 32px) so resizing
// snaps in finer steps; every v1 height/y is exactly double in v2. The client
// migrates any layout it reads below this version.
const layoutVersion = 2

const defaultDashboardLayout = `{
  "version": 2,
  "widgets": [
    {"id": "fleet-overview", "type": "fleet-overview", "x": 0, "y": 0, "w": 12, "h": 8},
    {"id": "fleet-trend", "type": "fleet-trend", "x": 0, "y": 8, "w": 12, "h": 10},
    {"id": "cpu-by-node", "type": "cpu-by-node", "x": 0, "y": 18, "w": 6, "h": 8},
    {"id": "memory-by-node", "type": "memory-by-node", "x": 6, "y": 18, "w": 6, "h": 8},
    {"id": "top-consumers", "type": "top-consumers", "x": 0, "y": 26, "w": 6, "h": 8},
    {"id": "running-tasks", "type": "running-tasks", "x": 6, "y": 26, "w": 6, "h": 8},
    {"id": "storage-usage", "type": "storage-usage", "x": 0, "y": 34, "w": 6, "h": 8},
    {"id": "alert-activity", "type": "alert-activity", "x": 6, "y": 34, "w": 6, "h": 8}
  ]
}`

const emptyDashboardLayout = `{"version": 2, "widgets": []}`

// Bounds how many dashboards one account can hoard — this is a personal
// workspace list, not a multi-tenant resource, so a generous flat cap is
// enough to stop runaway scripting without ever bothering a real user.
const maxDashboardsPerUser = 30

const maxDashboardNameLen = 60

type dashboardSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
}

type dashboardDTO struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Layout json.RawMessage `json:"-"`
}

// MarshalJSON flattens the stored {"widgets": [...]} layout up into the
// dashboard's own object, so the client sees {id, name, widgets} instead of
// a nested layout object it would have to unwrap.
func (d dashboardDTO) MarshalJSON() ([]byte, error) {
	var widgets json.RawMessage
	var parsed storedLayout
	if err := json.Unmarshal(d.Layout, &parsed); err == nil {
		if b, err := json.Marshal(parsed.Widgets); err == nil {
			widgets = b
		}
	}
	if widgets == nil {
		widgets = json.RawMessage("[]")
	}
	return json.Marshal(struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Version int             `json:"version"`
		Widgets json.RawMessage `json:"widgets"`
	}{d.ID, d.Name, parsed.Version, widgets})
}

func (s *Server) insertDashboard(w http.ResponseWriter, r *http.Request, userID, name string, layout []byte) (dashboardSummary, bool) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO dashboards (id, user_id, name, layout, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, name, string(layout), now, now,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return dashboardSummary{}, false
	}
	return dashboardSummary{ID: id, Name: name, UpdatedAt: now}, true
}

// listDashboards returns every dashboard the user owns, oldest first — the
// oldest is the fallback a client uses when no active dashboard is set (or
// the stored one was deleted elsewhere). A brand-new account gets its "Main"
// dashboard lazily, here, rather than at signup.
func (s *Server) listDashboards(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, updated_at FROM dashboards WHERE user_id = ? ORDER BY created_at ASC`, u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := []dashboardSummary{}
	for rows.Next() {
		var d dashboardSummary
		if err := rows.Scan(&d.ID, &d.Name, &d.UpdatedAt); err != nil {
			rows.Close()
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, d)
	}
	rows.Close()

	if len(out) == 0 {
		seeded, ok := s.insertDashboard(w, r, u.ID, "Main", []byte(defaultDashboardLayout))
		if !ok {
			return
		}
		out = append(out, seeded)
	}

	writeJSON(w, http.StatusOK, out)
}

type createDashboardRequest struct {
	Name string `json:"name"`
}

func (s *Server) createDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var req createDashboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(name) > maxDashboardNameLen {
		writeErrorMsg(w, http.StatusBadRequest, fmt.Sprintf("name must be %d characters or fewer", maxDashboardNameLen))
		return
	}

	var count int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM dashboards WHERE user_id = ?`, u.ID).Scan(&count); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if count >= maxDashboardsPerUser {
		writeErrorMsg(w, http.StatusBadRequest, fmt.Sprintf("you already have %d dashboards — remove one before adding another", maxDashboardsPerUser))
		return
	}

	created, ok := s.insertDashboard(w, r, u.ID, name, []byte(emptyDashboardLayout))
	if !ok {
		return
	}
	s.audit(r, "dashboard.create", "dashboard", name)
	writeJSON(w, http.StatusCreated, dashboardDTO{ID: created.ID, Name: name, Layout: json.RawMessage(emptyDashboardLayout)})
}

// dashboardOwnedByUser loads a dashboard's name + layout, scoped to the
// requesting user — every dashboard endpoint below routes through this so a
// user can never read, rename, resize or delete another account's dashboard
// by guessing an id.
func (s *Server) dashboardOwnedByUser(w http.ResponseWriter, r *http.Request) (name string, layout []byte, ok bool) {
	u := userFromContext(r)
	id := chi.URLParam(r, "id")
	var lay string
	err := s.db.QueryRowContext(r.Context(), `SELECT name, layout FROM dashboards WHERE id = ? AND user_id = ?`, id, u.ID).Scan(&name, &lay)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorMsg(w, http.StatusNotFound, "dashboard not found")
		return "", nil, false
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return "", nil, false
	}
	return name, []byte(lay), true
}

func (s *Server) getDashboard(w http.ResponseWriter, r *http.Request) {
	name, layout, ok := s.dashboardOwnedByUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, dashboardDTO{ID: chi.URLParam(r, "id"), Name: name, Layout: layout})
}

type updateDashboardRequest struct {
	Name    *string         `json:"name"`
	Widgets json.RawMessage `json:"widgets"`
}

func (s *Server) updateDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := chi.URLParam(r, "id")
	currentName, currentLayout, ok := s.dashboardOwnedByUser(w, r)
	if !ok {
		return
	}

	var req updateDashboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	name := currentName
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			writeErrorMsg(w, http.StatusBadRequest, "name is required")
			return
		}
		if len(name) > maxDashboardNameLen {
			writeErrorMsg(w, http.StatusBadRequest, fmt.Sprintf("name must be %d characters or fewer", maxDashboardNameLen))
			return
		}
	}

	layout := currentLayout
	if req.Widgets != nil {
		clean, err := parseDashboardLayout([]byte(fmt.Sprintf(`{"widgets":%s}`, req.Widgets)))
		if err != nil {
			writeErrorMsg(w, http.StatusBadRequest, err.Error())
			return
		}
		layout = clean
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE dashboards SET name = ?, layout = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		name, string(layout), now, id, u.ID,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if req.Name != nil && name != currentName {
		s.audit(r, "dashboard.rename", "dashboard", currentName+" -> "+name)
	}
	writeJSON(w, http.StatusOK, dashboardDTO{ID: id, Name: name, Layout: layout})
}

func (s *Server) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := chi.URLParam(r, "id")
	name, _, ok := s.dashboardOwnedByUser(w, r)
	if !ok {
		return
	}

	var count int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM dashboards WHERE user_id = ?`, u.ID).Scan(&count); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if count <= 1 {
		writeErrorMsg(w, http.StatusBadRequest, "can't delete your only dashboard")
		return
	}

	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM dashboards WHERE id = ? AND user_id = ?`, id, u.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	// If this was the account's active dashboard, clear the pointer so the
	// next load falls back to the oldest remaining one instead of a 404.
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE user_preferences SET active_dashboard_id = NULL WHERE user_id = ? AND active_dashboard_id = ?`, u.ID, id,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "dashboard.delete", "dashboard", name)
	w.WriteHeader(http.StatusNoContent)
}

type storedWidget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	// Per-widget settings (connection filter, row counts, ...) round-trip as
	// an opaque string map — without this field the re-marshal below would
	// silently drop them on every save.
	Settings map[string]string `json:"settings,omitempty"`
}

type storedLayout struct {
	// Absent (0) means a pre-versioning v1 layout; see layoutVersion.
	Version int            `json:"version,omitempty"`
	Widgets []storedWidget `json:"widgets"`
}

// parseDashboardLayout validates a client-submitted layout: an object with a
// widgets array of bounded, non-negative grid entries. Storing raw JSON
// would let any authenticated user park arbitrary nested structures in the
// database on every save.
func parseDashboardLayout(body []byte) ([]byte, error) {
	var layout storedLayout
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&layout); err != nil {
		return nil, fmt.Errorf("layout must be a JSON object with a widgets array")
	}
	if len(layout.Widgets) > 100 {
		return nil, fmt.Errorf("too many widgets (max 100)")
	}
	for _, wgt := range layout.Widgets {
		if wgt.ID == "" || wgt.Type == "" {
			return nil, fmt.Errorf("every widget needs an id and a type")
		}
		if len(wgt.Settings) > 20 {
			return nil, fmt.Errorf("widget %q has too many settings", wgt.ID)
		}
		if wgt.X < 0 || wgt.Y < 0 || wgt.W <= 0 || wgt.H <= 0 || wgt.W > 12 || wgt.H > 80 || wgt.X > 12 || wgt.Y > 1000 {
			return nil, fmt.Errorf("widget %q has out-of-bounds grid coordinates", wgt.ID)
		}
	}
	clean, err := json.Marshal(layout)
	if err != nil {
		return nil, err
	}
	return clean, nil
}
