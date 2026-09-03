package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"ferrum/internal/notify"
)

// notificationRow is the single (id=1) notification_settings row. Gotify and
// SMTP are independently optional — either, both, or neither can be enabled.
// gotifyTokenEnc / smtpPasswordEnc are encrypted at rest.
type notificationRow struct {
	gotifyEnabled  bool
	gotifyURL      string
	gotifyTokenEnc string

	smtpEnabled     bool
	smtpHost        string
	smtpPort        int
	smtpUsername    string
	smtpPasswordEnc string
	smtpFrom        string
	smtpTo          string
	smtpUseTLS      bool
}

func (s *Server) loadNotificationRow(ctx context.Context) (notificationRow, error) {
	var row notificationRow
	var gotifyEnabled, smtpEnabled, smtpUseTLS int
	err := s.db.QueryRowContext(ctx, `
		SELECT gotify_enabled, gotify_url, gotify_token, smtp_enabled, smtp_host, smtp_port, smtp_username, smtp_password, smtp_from, smtp_to, smtp_use_tls
		FROM notification_settings WHERE id = 1`).
		Scan(&gotifyEnabled, &row.gotifyURL, &row.gotifyTokenEnc, &smtpEnabled, &row.smtpHost, &row.smtpPort, &row.smtpUsername, &row.smtpPasswordEnc, &row.smtpFrom, &row.smtpTo, &smtpUseTLS)
	if err == sql.ErrNoRows {
		return notificationRow{smtpPort: 587, smtpUseTLS: true}, nil // sane defaults for a never-configured row
	}
	if err != nil {
		return notificationRow{}, err
	}
	row.gotifyEnabled = gotifyEnabled == 1
	row.smtpEnabled = smtpEnabled == 1
	row.smtpUseTLS = smtpUseTLS == 1
	return row, nil
}

func (s *Server) saveNotificationRow(ctx context.Context, row notificationRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_settings (id, gotify_enabled, gotify_url, gotify_token, smtp_enabled, smtp_host, smtp_port, smtp_username, smtp_password, smtp_from, smtp_to, smtp_use_tls, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			gotify_enabled = excluded.gotify_enabled, gotify_url = excluded.gotify_url, gotify_token = excluded.gotify_token,
			smtp_enabled = excluded.smtp_enabled, smtp_host = excluded.smtp_host, smtp_port = excluded.smtp_port,
			smtp_username = excluded.smtp_username, smtp_password = excluded.smtp_password, smtp_from = excluded.smtp_from,
			smtp_to = excluded.smtp_to, smtp_use_tls = excluded.smtp_use_tls, updated_at = excluded.updated_at`,
		boolToInt(row.gotifyEnabled), row.gotifyURL, row.gotifyTokenEnc,
		boolToInt(row.smtpEnabled), row.smtpHost, row.smtpPort, row.smtpUsername, row.smtpPasswordEnc, row.smtpFrom, row.smtpTo, boolToInt(row.smtpUseTLS),
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// toNotifySettings decrypts the stored secrets into the plain notify.Settings
// the Notifier actually sends with. Used both to apply a saved row to the
// live Notifier and to build the config for a "send test" request.
func (s *Server) toNotifySettings(row notificationRow) (notify.Settings, error) {
	gotifyToken, err := s.secrets.Decrypt(row.gotifyTokenEnc)
	if err != nil {
		return notify.Settings{}, err
	}
	smtpPassword, err := s.secrets.Decrypt(row.smtpPasswordEnc)
	if err != nil {
		return notify.Settings{}, err
	}
	return notify.Settings{
		Gotify: notify.GotifyConfig{Enabled: row.gotifyEnabled, URL: row.gotifyURL, Token: gotifyToken},
		SMTP: notify.SMTPConfig{
			Enabled: row.smtpEnabled, Host: row.smtpHost, Port: row.smtpPort, Username: row.smtpUsername,
			Password: smtpPassword, From: row.smtpFrom, To: row.smtpTo, UseTLS: row.smtpUseTLS,
		},
	}, nil
}

// applyNotificationRow pushes row into the live Notifier (and the alert
// evaluator that holds it) — called at boot and whenever the settings UI
// saves a change, so both paths build the exact same Settings.
func (s *Server) applyNotificationRow(row notificationRow) error {
	settings, err := s.toNotifySettings(row)
	if err != nil {
		return err
	}
	s.notify.Update(settings)
	return nil
}

type notificationSettingsResponse struct {
	GotifyEnabled  bool   `json:"gotifyEnabled"`
	GotifyURL      string `json:"gotifyUrl"`
	HasGotifyToken bool   `json:"hasGotifyToken"`

	SMTPEnabled     bool   `json:"smtpEnabled"`
	SMTPHost        string `json:"smtpHost"`
	SMTPPort        int    `json:"smtpPort"`
	SMTPUsername    string `json:"smtpUsername"`
	HasSMTPPassword bool   `json:"hasSmtpPassword"`
	SMTPFrom        string `json:"smtpFrom"`
	SMTPTo          string `json:"smtpTo"`
	SMTPUseTLS      bool   `json:"smtpUseTls"`
}

func toNotificationSettingsResponse(row notificationRow) notificationSettingsResponse {
	return notificationSettingsResponse{
		GotifyEnabled: row.gotifyEnabled, GotifyURL: row.gotifyURL, HasGotifyToken: row.gotifyTokenEnc != "",
		SMTPEnabled: row.smtpEnabled, SMTPHost: row.smtpHost, SMTPPort: row.smtpPort, SMTPUsername: row.smtpUsername,
		HasSMTPPassword: row.smtpPasswordEnc != "", SMTPFrom: row.smtpFrom, SMTPTo: row.smtpTo, SMTPUseTLS: row.smtpUseTLS,
	}
}

func (s *Server) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadNotificationRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotificationSettingsResponse(row))
}

// notificationSettingsPatch is a partial update — same "nil means unchanged,
// blank secret means keep the stored one" pattern as OIDC settings.
type notificationSettingsPatch struct {
	GotifyEnabled *bool   `json:"gotifyEnabled"`
	GotifyURL     *string `json:"gotifyUrl"`
	GotifyToken   *string `json:"gotifyToken"`

	SMTPEnabled  *bool   `json:"smtpEnabled"`
	SMTPHost     *string `json:"smtpHost"`
	SMTPPort     *int    `json:"smtpPort"`
	SMTPUsername *string `json:"smtpUsername"`
	SMTPPassword *string `json:"smtpPassword"`
	SMTPFrom     *string `json:"smtpFrom"`
	SMTPTo       *string `json:"smtpTo"`
	SMTPUseTLS   *bool   `json:"smtpUseTls"`
}

func (s *Server) putNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var patch notificationSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadNotificationRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.GotifyURL != nil {
		row.gotifyURL = *patch.GotifyURL
	}
	if patch.GotifyToken != nil && *patch.GotifyToken != "" {
		enc, err := s.secrets.Encrypt(*patch.GotifyToken)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		row.gotifyTokenEnc = enc
	}
	if patch.GotifyEnabled != nil {
		row.gotifyEnabled = *patch.GotifyEnabled
	}
	if row.gotifyEnabled && (row.gotifyURL == "" || row.gotifyTokenEnc == "") {
		writeErrorMsg(w, http.StatusBadRequest, "gotify URL and token are required to enable Gotify notifications")
		return
	}

	if patch.SMTPHost != nil {
		row.smtpHost = *patch.SMTPHost
	}
	if patch.SMTPPort != nil {
		if *patch.SMTPPort < 1 || *patch.SMTPPort > 65535 {
			writeErrorMsg(w, http.StatusBadRequest, "smtp port must be between 1 and 65535")
			return
		}
		row.smtpPort = *patch.SMTPPort
	}
	if patch.SMTPUsername != nil {
		row.smtpUsername = *patch.SMTPUsername
	}
	if patch.SMTPPassword != nil && *patch.SMTPPassword != "" {
		enc, err := s.secrets.Encrypt(*patch.SMTPPassword)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		row.smtpPasswordEnc = enc
	}
	if patch.SMTPFrom != nil {
		row.smtpFrom = *patch.SMTPFrom
	}
	if patch.SMTPTo != nil {
		row.smtpTo = *patch.SMTPTo
	}
	if patch.SMTPUseTLS != nil {
		row.smtpUseTLS = *patch.SMTPUseTLS
	}
	if patch.SMTPEnabled != nil {
		row.smtpEnabled = *patch.SMTPEnabled
	}
	if row.smtpEnabled && (row.smtpHost == "" || row.smtpFrom == "" || row.smtpTo == "") {
		writeErrorMsg(w, http.StatusBadRequest, "smtp host, from address, and recipients are required to enable email notifications")
		return
	}

	if err := s.saveNotificationRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.applyNotificationRow(row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if s.evaluator != nil {
		s.evaluator.SetNotifier(s.notify)
	}
	s.audit(r, "settings.notifications", "settings", "updated")
	writeJSON(w, http.StatusOK, toNotificationSettingsResponse(row))
}

// testNotificationRequest lets the settings UI send a test message before
// saving — using the currently stored secret when the form field was left
// blank (so testing doesn't require re-entering a token/password that's
// already saved), or an inline value when the admin just typed a new one.
type testNotificationRequest struct {
	Channel string `json:"channel"` // "gotify" | "smtp"

	GotifyURL   *string `json:"gotifyUrl"`
	GotifyToken *string `json:"gotifyToken"`

	SMTPHost     *string `json:"smtpHost"`
	SMTPPort     *int    `json:"smtpPort"`
	SMTPUsername *string `json:"smtpUsername"`
	SMTPPassword *string `json:"smtpPassword"`
	SMTPFrom     *string `json:"smtpFrom"`
	SMTPTo       *string `json:"smtpTo"`
	SMTPUseTLS   *bool   `json:"smtpUseTls"`
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	var req testNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	stored, err := s.loadNotificationRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	switch req.Channel {
	case "gotify":
		cfg := notify.GotifyConfig{URL: stored.gotifyURL}
		if req.GotifyURL != nil {
			cfg.URL = *req.GotifyURL
		}
		if req.GotifyToken != nil && *req.GotifyToken != "" {
			cfg.Token = *req.GotifyToken
		} else {
			cfg.Token, err = s.secrets.Decrypt(stored.gotifyTokenEnc)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		if err := notify.TestGotify(r.Context(), cfg); err != nil {
			writeErrorMsg(w, http.StatusBadGateway, err.Error())
			return
		}
	case "smtp":
		cfg := notify.SMTPConfig{Host: stored.smtpHost, Port: stored.smtpPort, Username: stored.smtpUsername, From: stored.smtpFrom, To: stored.smtpTo, UseTLS: stored.smtpUseTLS}
		if req.SMTPHost != nil {
			cfg.Host = *req.SMTPHost
		}
		if req.SMTPPort != nil {
			cfg.Port = *req.SMTPPort
		}
		if req.SMTPUsername != nil {
			cfg.Username = *req.SMTPUsername
		}
		if req.SMTPFrom != nil {
			cfg.From = *req.SMTPFrom
		}
		if req.SMTPTo != nil {
			cfg.To = *req.SMTPTo
		}
		if req.SMTPUseTLS != nil {
			cfg.UseTLS = *req.SMTPUseTLS
		}
		if req.SMTPPassword != nil && *req.SMTPPassword != "" {
			cfg.Password = *req.SMTPPassword
		} else {
			cfg.Password, err = s.secrets.Decrypt(stored.smtpPasswordEnc)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		if err := notify.TestSMTP(cfg); err != nil {
			writeErrorMsg(w, http.StatusBadGateway, err.Error())
			return
		}
	default:
		writeErrorMsg(w, http.StatusBadRequest, `channel must be "gotify" or "smtp"`)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
