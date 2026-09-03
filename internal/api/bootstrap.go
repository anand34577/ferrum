package api

import (
	"context"
	"database/sql"

	"ferrum/internal/auth"
)

// BootstrapSettings loads OIDC and notification settings from the database
// and applies them (live OIDC client, live Notifier) at startup.
//
// seedOIDC is the config.yaml/env OIDC config (internal/config.OIDCConfig,
// converted by the caller) — config.yaml was the only way to set this up
// before the Settings UI existed. The very first boot after upgrading copies
// it into the database once; every boot after that (and every save from the
// UI) uses the database exclusively, so config.yaml stops being read for
// OIDC at all once a row exists.
func (s *Server) BootstrapSettings(ctx context.Context, seedEnabled bool, seedOIDC auth.OIDCConfig) error {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM oidc_settings WHERE id = 1`).Scan(&exists)
	if err == sql.ErrNoRows {
		enc, encErr := s.secrets.Encrypt(seedOIDC.ClientSecret)
		if encErr != nil {
			return encErr
		}
		if err := s.saveOIDCRow(ctx, oidcRow{
			enabled: seedEnabled, displayName: seedOIDC.DisplayName, issuerURL: seedOIDC.IssuerURL,
			clientID: seedOIDC.ClientID, clientSecretEnc: enc, redirectURL: seedOIDC.RedirectURL,
			allowAutoProvision: true, // matches the pre-existing always-on behavior config.yaml-only deployments already had
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	row, err := s.loadOIDCRow(ctx)
	if err != nil {
		return err
	}
	s.applyOIDCRow(row)

	notifRow, err := s.loadNotificationRow(ctx)
	if err != nil {
		return err
	}
	if err := s.applyNotificationRow(notifRow); err != nil {
		return err
	}

	secRow, err := s.loadSecurityRow(ctx)
	if err != nil {
		return err
	}
	s.applySecurityRow(secRow)
	return nil
}
