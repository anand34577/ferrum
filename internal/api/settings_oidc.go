package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"ferrum/internal/auth"
)

// oidcRow is the single (id=1) oidc_settings row — admin-editable via the
// Settings UI, so SSO no longer requires editing config.yaml and restarting.
// clientSecretEnc is encrypted at rest with the same secrets.Box as
// connection credentials.
type oidcRow struct {
	enabled         bool
	displayName     string
	issuerURL       string
	clientID        string
	clientSecretEnc string
	redirectURL     string
	// allowAutoProvision: whether a first-time SSO login may create a new
	// local account, or must be refused because no matching account already
	// exists. Defaults to true (the historical always-on behavior).
	allowAutoProvision bool
	// singleLogout: whether sign-out also ends the session at the identity
	// provider (RP-Initiated Logout). Defaults to false — see
	// auth.OIDCConfig.SingleLogout for why this is opt-in, not automatic.
	singleLogout bool
}

func (s *Server) loadOIDCRow(ctx context.Context) (oidcRow, error) {
	row := oidcRow{allowAutoProvision: true}
	var enabled, allowAutoProvision, singleLogout int
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled, display_name, issuer_url, client_id, client_secret, redirect_url, allow_auto_provision, single_logout FROM oidc_settings WHERE id = 1`).
		Scan(&enabled, &row.displayName, &row.issuerURL, &row.clientID, &row.clientSecretEnc, &row.redirectURL, &allowAutoProvision, &singleLogout)
	if err == sql.ErrNoRows {
		return oidcRow{allowAutoProvision: true}, nil // no row yet — every field at its zero value, same as a freshly disabled config
	}
	if err != nil {
		return oidcRow{}, err
	}
	row.enabled = enabled == 1
	row.allowAutoProvision = allowAutoProvision == 1
	row.singleLogout = singleLogout == 1
	return row, nil
}

func (s *Server) saveOIDCRow(ctx context.Context, row oidcRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oidc_settings (id, enabled, display_name, issuer_url, client_id, client_secret, redirect_url, allow_auto_provision, single_logout, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			enabled = excluded.enabled, display_name = excluded.display_name, issuer_url = excluded.issuer_url,
			client_id = excluded.client_id, client_secret = excluded.client_secret, redirect_url = excluded.redirect_url,
			allow_auto_provision = excluded.allow_auto_provision, single_logout = excluded.single_logout, updated_at = excluded.updated_at`,
		boolToInt(row.enabled), row.displayName, row.issuerURL, row.clientID, row.clientSecretEnc, row.redirectURL,
		boolToInt(row.allowAutoProvision), boolToInt(row.singleLogout), time.Now().UTC().Format(time.RFC3339))
	return err
}

// applyOIDCRow decrypts the stored secret and swaps in a live OIDC client
// built from row — or clears SSO entirely when disabled/incomplete. Used
// both at boot and whenever the settings UI saves a change, so the two paths
// can never drift.
func (s *Server) applyOIDCRow(row oidcRow) {
	if !row.enabled || row.issuerURL == "" || row.clientID == "" || row.redirectURL == "" || row.clientSecretEnc == "" {
		s.SetOIDC(nil)
		return
	}
	secret, err := s.secrets.Decrypt(row.clientSecretEnc)
	if err != nil {
		slog.Error("decrypting stored OIDC client secret", "error", err)
		s.SetOIDC(nil)
		return
	}
	s.SetOIDC(auth.NewOIDCClient(auth.OIDCConfig{
		DisplayName:        row.displayName,
		IssuerURL:          row.issuerURL,
		ClientID:           row.clientID,
		ClientSecret:       secret,
		RedirectURL:        row.redirectURL,
		AllowAutoProvision: row.allowAutoProvision,
		SingleLogout:       row.singleLogout,
	}))
	slog.Info("SSO enabled", "issuer", row.issuerURL)
}

type oidcSettingsResponse struct {
	Enabled            bool   `json:"enabled"`
	DisplayName        string `json:"displayName"`
	IssuerURL          string `json:"issuerUrl"`
	ClientID           string `json:"clientId"`
	RedirectURL        string `json:"redirectUrl"`
	AllowAutoProvision bool   `json:"allowAutoProvision"`
	SingleLogout       bool   `json:"singleLogout"`
	// PostLogoutRedirectURL is what Ferrum will ask the provider to send the
	// browser back to for RP-Initiated Logout — the admin needs to register
	// this exact URL with the provider (e.g. Keycloak's "Valid post logout
	// redirect URIs") before turning SingleLogout on, or the provider
	// refuses the redirect with invalid_redirect_uri. Derived from
	// RedirectURL, so it's blank until that's set.
	PostLogoutRedirectURL string `json:"postLogoutRedirectUrl,omitempty"`
	// HasSecret tells the UI a secret is already stored, without ever
	// sending the secret itself back down to the browser.
	HasSecret bool `json:"hasSecret"`
}

func toOIDCSettingsResponse(row oidcRow) oidcSettingsResponse {
	return oidcSettingsResponse{
		Enabled: row.enabled, DisplayName: row.displayName, IssuerURL: row.issuerURL,
		ClientID: row.clientID, RedirectURL: row.redirectURL, AllowAutoProvision: row.allowAutoProvision,
		SingleLogout: row.singleLogout, PostLogoutRedirectURL: postLogoutRedirectURL(row.redirectURL),
		HasSecret: row.clientSecretEnc != "",
	}
}

// postLogoutRedirectURL mirrors auth.OIDCClient's private derivation
// (callback URL's origin + "/login") so the settings UI can show the admin
// exactly what to register at the provider without needing a live client.
func postLogoutRedirectURL(redirectURL string) string {
	u, err := url.Parse(redirectURL)
	if err != nil || redirectURL == "" {
		return ""
	}
	u.Path = "/login"
	u.RawQuery = ""
	return u.String()
}

func (s *Server) getOIDCSettings(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadOIDCRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toOIDCSettingsResponse(row))
}

// oidcSettingsPatch is a partial update, same pattern as preferences.go: a
// nil field is left unchanged, and ClientSecret is only ever overwritten
// when non-empty — an omitted/blank secret means "keep what's stored",
// since the UI never has the real value to send back.
type oidcSettingsPatch struct {
	Enabled            *bool   `json:"enabled"`
	DisplayName        *string `json:"displayName"`
	IssuerURL          *string `json:"issuerUrl"`
	ClientID           *string `json:"clientId"`
	ClientSecret       *string `json:"clientSecret"`
	RedirectURL        *string `json:"redirectUrl"`
	AllowAutoProvision *bool   `json:"allowAutoProvision"`
	SingleLogout       *bool   `json:"singleLogout"`
}

func (s *Server) putOIDCSettings(w http.ResponseWriter, r *http.Request) {
	var patch oidcSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	row, err := s.loadOIDCRow(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.DisplayName != nil {
		row.displayName = *patch.DisplayName
	}
	if patch.IssuerURL != nil {
		row.issuerURL = *patch.IssuerURL
	}
	if patch.ClientID != nil {
		row.clientID = *patch.ClientID
	}
	if patch.RedirectURL != nil {
		row.redirectURL = *patch.RedirectURL
	}
	if patch.ClientSecret != nil && *patch.ClientSecret != "" {
		enc, err := s.secrets.Encrypt(*patch.ClientSecret)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		row.clientSecretEnc = enc
	}
	if patch.AllowAutoProvision != nil {
		row.allowAutoProvision = *patch.AllowAutoProvision
	}
	if patch.SingleLogout != nil {
		row.singleLogout = *patch.SingleLogout
	}
	if patch.Enabled != nil {
		row.enabled = *patch.Enabled
	}

	if row.enabled && (row.issuerURL == "" || row.clientID == "" || row.redirectURL == "" || row.clientSecretEnc == "") {
		writeErrorMsg(w, http.StatusBadRequest, "issuer URL, client ID, client secret, and redirect URL are all required to enable SSO")
		return
	}

	if err := s.saveOIDCRow(r.Context(), row); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.applyOIDCRow(row)
	s.audit(r, "settings.oidc", "settings", boolLabel(row.enabled))
	writeJSON(w, http.StatusOK, toOIDCSettingsResponse(row))
}

func boolLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
