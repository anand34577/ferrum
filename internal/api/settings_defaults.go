package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// defaultPreferencesRow is the single (id=1) default_preferences row — the
// org-wide theme/accent/look/landing-page a brand-new user starts with,
// before they've ever saved a preference of their own. Admin-editable from
// Settings; existing users with a saved preference are unaffected.
type defaultPreferencesRow struct {
	theme       string
	accent      string
	look        string
	landingPage string
	density     string
}

func defaultDefaultPreferencesRow() defaultPreferencesRow {
	return defaultPreferencesRow{theme: "system", accent: "oxide", look: "enterprise", landingPage: "/", density: "comfortable"}
}

func (s *Server) loadDefaultPreferencesRow(ctx context.Context) (defaultPreferencesRow, error) {
	row := defaultDefaultPreferencesRow()
	err := s.db.QueryRowContext(ctx, `SELECT theme, accent, look, landing_page, density FROM default_preferences WHERE id = 1`).
		Scan(&row.theme, &row.accent, &row.look, &row.landingPage, &row.density)
	if err == sql.ErrNoRows {
		return defaultDefaultPreferencesRow(), nil
	}
	if err != nil {
		return defaultPreferencesRow{}, err
	}
	return row, nil
}

func (s *Server) saveDefaultPreferencesRow(ctx context.Context, row defaultPreferencesRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO default_preferences (id, theme, accent, look, landing_page, density, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			theme = excluded.theme, accent = excluded.accent, look = excluded.look, landing_page = excluded.landing_page,
			density = excluded.density, updated_at = excluded.updated_at`,
		row.theme, row.accent, row.look, row.landingPage, row.density, time.Now().UTC().Format(time.RFC3339))
	return err
}

type defaultPreferencesResponse struct {
	Theme       string `json:"theme"`
	Accent      string `json:"accent"`
	Look        string `json:"look"`
	LandingPage string `json:"landingPage"`
	Density     string `json:"density"`
}

func toDefaultPreferencesResponse(row defaultPreferencesRow) defaultPreferencesResponse {
	return defaultPreferencesResponse{Theme: row.theme, Accent: row.accent, Look: row.look, LandingPage: row.landingPage, Density: row.density}
}

func (s *Server) getDefaultPreferences(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadDefaultPreferencesRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toDefaultPreferencesResponse(row))
}

type defaultPreferencesPatch struct {
	Theme       *string `json:"theme"`
	Accent      *string `json:"accent"`
	Look        *string `json:"look"`
	LandingPage *string `json:"landingPage"`
	Density     *string `json:"density"`
}

func (s *Server) putDefaultPreferences(w http.ResponseWriter, r *http.Request) {
	var patch defaultPreferencesPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadDefaultPreferencesRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.Theme != nil {
		if !validThemes[*patch.Theme] {
			writeErrorMsg(w, http.StatusBadRequest, "theme must be one of: light, dark, system")
			return
		}
		row.theme = *patch.Theme
	}
	if patch.Accent != nil {
		if !validAccents[*patch.Accent] {
			writeErrorMsg(w, http.StatusBadRequest, "accent must be one of: oxide, azure, verdant, violet, slate")
			return
		}
		row.accent = *patch.Accent
	}
	if patch.Look != nil {
		if !validLooks[*patch.Look] {
			writeErrorMsg(w, http.StatusBadRequest, "unrecognized look")
			return
		}
		row.look = *patch.Look
	}
	if patch.LandingPage != nil {
		if !validLandingPages[*patch.LandingPage] {
			writeErrorMsg(w, http.StatusBadRequest, "unrecognized landing page")
			return
		}
		row.landingPage = *patch.LandingPage
	}
	if patch.Density != nil {
		if !validDensities[*patch.Density] {
			writeErrorMsg(w, http.StatusBadRequest, "density must be one of: comfortable, compact")
			return
		}
		row.density = *patch.Density
	}

	if err := s.saveDefaultPreferencesRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "settings.defaults", "settings", "updated")
	writeJSON(w, http.StatusOK, toDefaultPreferencesResponse(row))
}
