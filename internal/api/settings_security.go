package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// securityRow is the single (id=1) security_settings row — session TTL and
// login-lockout policy, admin-editable from Settings, applied to the running
// server (auth.Service, loginLimiter) immediately on save.
type securityRow struct {
	sessionTTLHours     int
	loginMaxFailures    int
	loginLockoutMinutes int
	require2FAAdmins    bool
}

func defaultSecurityRow() securityRow {
	return securityRow{sessionTTLHours: 720, loginMaxFailures: 5, loginLockoutMinutes: 15}
}

func (s *Server) loadSecurityRow(ctx context.Context) (securityRow, error) {
	row := defaultSecurityRow()
	var require2FA int
	err := s.db.QueryRowContext(ctx,
		`SELECT session_ttl_hours, login_max_failures, login_lockout_minutes, require_2fa_admins FROM security_settings WHERE id = 1`).
		Scan(&row.sessionTTLHours, &row.loginMaxFailures, &row.loginLockoutMinutes, &require2FA)
	if err == sql.ErrNoRows {
		return defaultSecurityRow(), nil
	}
	if err != nil {
		return securityRow{}, err
	}
	row.require2FAAdmins = require2FA == 1
	return row, nil
}

func (s *Server) saveSecurityRow(ctx context.Context, row securityRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO security_settings (id, session_ttl_hours, login_max_failures, login_lockout_minutes, require_2fa_admins, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			session_ttl_hours = excluded.session_ttl_hours, login_max_failures = excluded.login_max_failures,
			login_lockout_minutes = excluded.login_lockout_minutes, require_2fa_admins = excluded.require_2fa_admins,
			updated_at = excluded.updated_at`,
		row.sessionTTLHours, row.loginMaxFailures, row.loginLockoutMinutes, boolToInt(row.require2FAAdmins),
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// applySecurityRow pushes row into the live auth.Service, loginLimiter, and
// the requireTOTPEnrolled gate — called at boot and on every save.
func (s *Server) applySecurityRow(row securityRow) {
	s.auth.SetSessionTTL(time.Duration(row.sessionTTLHours) * time.Hour)
	s.logins.SetPolicy(row.loginMaxFailures, time.Duration(row.loginLockoutMinutes)*time.Minute)
	s.SetRequire2FAAdmins(row.require2FAAdmins)
}

type securitySettingsResponse struct {
	SessionTTLHours     int  `json:"sessionTtlHours"`
	LoginMaxFailures    int  `json:"loginMaxFailures"`
	LoginLockoutMinutes int  `json:"loginLockoutMinutes"`
	Require2FAAdmins    bool `json:"require2faAdmins"`
}

func toSecuritySettingsResponse(row securityRow) securitySettingsResponse {
	return securitySettingsResponse{
		SessionTTLHours: row.sessionTTLHours, LoginMaxFailures: row.loginMaxFailures,
		LoginLockoutMinutes: row.loginLockoutMinutes, Require2FAAdmins: row.require2FAAdmins,
	}
}

func (s *Server) getSecuritySettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadSecurityRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toSecuritySettingsResponse(row))
}

type securitySettingsPatch struct {
	SessionTTLHours     *int  `json:"sessionTtlHours"`
	LoginMaxFailures    *int  `json:"loginMaxFailures"`
	LoginLockoutMinutes *int  `json:"loginLockoutMinutes"`
	Require2FAAdmins    *bool `json:"require2faAdmins"`
}

func (s *Server) putSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var patch securitySettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadSecurityRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.SessionTTLHours != nil {
		if *patch.SessionTTLHours < 1 || *patch.SessionTTLHours > 8760 {
			writeErrorMsg(w, http.StatusBadRequest, "session TTL must be between 1 and 8760 hours (1 year)")
			return
		}
		row.sessionTTLHours = *patch.SessionTTLHours
	}
	if patch.LoginMaxFailures != nil {
		if *patch.LoginMaxFailures < 1 || *patch.LoginMaxFailures > 100 {
			writeErrorMsg(w, http.StatusBadRequest, "login max failures must be between 1 and 100")
			return
		}
		row.loginMaxFailures = *patch.LoginMaxFailures
	}
	if patch.LoginLockoutMinutes != nil {
		if *patch.LoginLockoutMinutes < 1 || *patch.LoginLockoutMinutes > 1440 {
			writeErrorMsg(w, http.StatusBadRequest, "login lockout window must be between 1 and 1440 minutes (24h)")
			return
		}
		row.loginLockoutMinutes = *patch.LoginLockoutMinutes
	}
	if patch.Require2FAAdmins != nil {
		row.require2FAAdmins = *patch.Require2FAAdmins
	}

	if err := s.saveSecurityRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.applySecurityRow(row)
	s.audit(r, "settings.security", "settings", "updated")
	writeJSON(w, http.StatusOK, toSecuritySettingsResponse(row))
}
