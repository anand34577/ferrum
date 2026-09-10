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
	// NotifyEmail opts this account into alert-trigger emails at its own
	// address (users.email), in addition to whatever the admin's global SMTP
	// "to" list already sends to — see internal/notify and poller/alerts.go.
	NotifyEmail bool `json:"notifyEmail"`
	// LandingPage is the route this user opens on after login; empty means
	// "use the org-wide default" (default_preferences.landing_page).
	LandingPage string `json:"landingPage"`
	// Density controls row/list padding across tables and lists app-wide.
	Density string `json:"density"`
}

var validThemes = map[string]bool{"light": true, "dark": true, "system": true}

// Accent is a named palette, not a raw color — same whitelist reasoning as
// theme: the actual color ramps live in the frontend's index.css, so the
// server just needs to reject anything that isn't one of the known names.
var validAccents = map[string]bool{"oxide": true, "azure": true, "verdant": true, "violet": true, "slate": true, "amber": true, "rose": true, "teal": true}

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
	"solarized":       true,
	"highContrast":    true,
	"aurora":          true,
}

// validLandingPages whitelists the routes a user (or the org-wide default)
// may land on after login — every top-level nav destination the sidebar
// itself links to (see web/src/components/layout/AppShell.tsx's navGroups).
// validDensities whitelists the row/list padding modes — see index.css's
// [data-density] rules for what each one actually repaints.
var validDensities = map[string]bool{"comfortable": true, "compact": true}

var validLandingPages = map[string]bool{
	"/": true, "/dashboard": true, "/inventory": true, "/topology": true,
	"/storage": true, "/pools": true, "/ha": true,
	"/backups": true, "/firewall": true, "/alerts": true, "/tasks": true,
	"/ai-assistant": true,
}

// currentPreferences reads this user's saved row, falling back to the
// admin-configured org-wide defaults (default_preferences) for any field
// they've never set — a brand-new account starts on the org's chosen
// look/theme/accent/landing page instead of a hardcoded one.
func (s *Server) currentPreferences(r *http.Request, userID string) (userPreferences, error) {
	defaults, err := s.loadDefaultPreferencesRow(r.Context())
	if err != nil {
		return userPreferences{}, err
	}
	prefs := userPreferences{Theme: defaults.theme, Accent: defaults.accent, Look: defaults.look, LandingPage: defaults.landingPage, Density: defaults.density}

	var activeDashboardID sql.NullString
	var landingPage sql.NullString
	var density sql.NullString
	var notifyEmail int
	err = s.db.QueryRowContext(r.Context(),
		`SELECT theme, accent, look, active_dashboard_id, notify_email, landing_page, density FROM user_preferences WHERE user_id = ?`, userID).
		Scan(&prefs.Theme, &prefs.Accent, &prefs.Look, &activeDashboardID, &notifyEmail, &landingPage, &density)
	if err != nil && err != sql.ErrNoRows {
		return userPreferences{}, err
	}
	if err == sql.ErrNoRows {
		return prefs, nil // no saved row yet — org defaults (set above) stand as-is
	}
	prefs.ActiveDashboardID = activeDashboardID.String
	prefs.NotifyEmail = notifyEmail == 1
	if landingPage.Valid && landingPage.String != "" {
		prefs.LandingPage = landingPage.String
	}
	if density.Valid && density.String != "" {
		prefs.Density = density.String
	}
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
	NotifyEmail       *bool   `json:"notifyEmail"`
	// LandingPage: "" explicitly clears the override (falls back to the org
	// default), same convention as ActiveDashboardID.
	LandingPage *string `json:"landingPage"`
	Density     *string `json:"density"`
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
			writeErrorMsg(w, http.StatusBadRequest, "accent must be one of: oxide, azure, verdant, violet, slate, amber, rose, teal")
			return
		}
		current.Accent = *patch.Accent
	}
	if patch.Look != nil {
		if !validLooks[*patch.Look] {
			writeErrorMsg(w, http.StatusBadRequest, "look must be one of: enterprise, proxmox, terminal, glassFlightDeck, midnight, paper, glassmorphism, neumorphism, brutalist, solarized, highContrast, aurora")
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
	if patch.NotifyEmail != nil {
		current.NotifyEmail = *patch.NotifyEmail
	}
	if patch.LandingPage != nil {
		if *patch.LandingPage != "" && !validLandingPages[*patch.LandingPage] {
			writeErrorMsg(w, http.StatusBadRequest, "unrecognized landing page")
			return
		}
		current.LandingPage = *patch.LandingPage
	}
	if patch.Density != nil {
		if !validDensities[*patch.Density] {
			writeErrorMsg(w, http.StatusBadRequest, "density must be one of: comfortable, compact")
			return
		}
		current.Density = *patch.Density
	}

	var activeDashboardID any
	if current.ActiveDashboardID != "" {
		activeDashboardID = current.ActiveDashboardID
	}
	var landingPage any
	if current.LandingPage != "" {
		landingPage = current.LandingPage
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO user_preferences (user_id, theme, accent, look, active_dashboard_id, notify_email, landing_page, density, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET theme = excluded.theme, accent = excluded.accent, look = excluded.look, active_dashboard_id = excluded.active_dashboard_id,
			notify_email = excluded.notify_email, landing_page = excluded.landing_page, density = excluded.density, updated_at = excluded.updated_at`,
		u.ID, current.Theme, current.Accent, current.Look, activeDashboardID, boolToInt(current.NotifyEmail), landingPage, current.Density, time.Now().UTC().Format(time.RFC3339)); err != nil {
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
