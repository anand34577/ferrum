package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// User preferences are small per-account UI settings (theme, accent, which
// dashboard opens by default) stored server-side so an account carries its
// look-and-feel across browsers and devices — the browser keeps only a
// fast-paint cache.
//
// Only whitelisted keys are accepted: storing raw JSON would let any
// authenticated user park arbitrary structures in the database on every save.

type userPreferences struct {
	Theme             string `json:"theme"`
	Accent            string `json:"accent"`
	Look              string `json:"look"`
	ActiveDashboardID string `json:"activeDashboardId"`
}

var validThemes = map[string]bool{"light": true, "dark": true, "system": true}

// Accent is a named palette, not a raw color — same whitelist reasoning as
// theme: the actual color ramps live in the frontend's index.css, so the
// server just needs to reject anything that isn't one of the known names.
var validAccents = map[string]bool{"oxide": true, "azure": true, "verdant": true, "violet": true, "slate": true}

// Look is a named whole-app visual register (typography, radius, elevation,
// surface tone) — same whitelist reasoning as accent: index.css owns the
// actual token values behind each name.
var validLooks = map[string]bool{
	"enterprise":      true,
	"proxmox":         true,
	"terminal":        true,
	"glassFlightDeck": true,
	"midnight":        true,
	"paper":           true,
	"glassmorphism":   true,
	"neumorphism":     true,
	"brutalist":       true,
}

func (s *Server) currentPreferences(r *http.Request, userID string) (userPreferences, error) {
	prefs := userPreferences{Theme: "system", Accent: "oxide", Look: "enterprise"}
	var activeDashboardID sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT theme, accent, look, active_dashboard_id FROM user_preferences WHERE user_id = ?`, userID).
		Scan(&prefs.Theme, &prefs.Accent, &prefs.Look, &activeDashboardID)
	if err != nil && err != sql.ErrNoRows {
		return userPreferences{}, err
	}
	prefs.ActiveDashboardID = activeDashboardID.String
	return prefs, nil
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	prefs, err := s.currentPreferences(r, u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// putPreferences is a partial update: only fields present in the request
// body are changed, so switching your active dashboard doesn't require
// re-sending theme and accent (and vice versa). Pointers distinguish
// "not sent" from "sent as empty".
type preferencesPatch struct {
	Theme             *string `json:"theme"`
	Accent            *string `json:"accent"`
	Look              *string `json:"look"`
	ActiveDashboardID *string `json:"activeDashboardId"`
}

func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var patch preferencesPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	current, err := s.currentPreferences(r, u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.Theme != nil {
		if !validThemes[*patch.Theme] {
			writeErrorMsg(w, http.StatusBadRequest, "theme must be one of: light, dark, system")
			return
		}
		current.Theme = *patch.Theme
	}
	if patch.Accent != nil {
		if !validAccents[*patch.Accent] {
			writeErrorMsg(w, http.StatusBadRequest, "accent must be one of: oxide, azure, verdant, violet, slate")
			return
		}
		current.Accent = *patch.Accent
	}
	if patch.Look != nil {
		if !validLooks[*patch.Look] {
			writeErrorMsg(w, http.StatusBadRequest, "look must be one of: enterprise, proxmox, terminal, glassFlightDeck, midnight, paper, glassmorphism, neumorphism, brutalist")
			return
		}
		current.Look = *patch.Look
	}
	if patch.ActiveDashboardID != nil {
		if *patch.ActiveDashboardID != "" {
			var exists int
			err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM dashboards WHERE id = ? AND user_id = ?`, *patch.ActiveDashboardID, u.ID).Scan(&exists)
			if err == sql.ErrNoRows {
				writeErrorMsg(w, http.StatusBadRequest, "dashboard not found")
				return
			}
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		current.ActiveDashboardID = *patch.ActiveDashboardID
	}

	var activeDashboardID any
	if current.ActiveDashboardID != "" {
		activeDashboardID = current.ActiveDashboardID
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO user_preferences (user_id, theme, accent, look, active_dashboard_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET theme = excluded.theme, accent = excluded.accent, look = excluded.look, active_dashboard_id = excluded.active_dashboard_id, updated_at = excluded.updated_at`,
		u.ID, current.Theme, current.Accent, current.Look, activeDashboardID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Only the fields actually sent are worth a log line — logging every field
	// on every dashboard switch (and vice versa) would drown the audit log in
	// noise for what's a routine, frequent action.
	if patch.Theme != nil {
		s.audit(r, "preferences.theme", "settings", current.Theme)
	}
	if patch.Accent != nil {
		s.audit(r, "preferences.accent", "settings", current.Accent)
	}
	if patch.Look != nil {
		s.audit(r, "preferences.look", "settings", current.Look)
	}
	writeJSON(w, http.StatusOK, current)
}
