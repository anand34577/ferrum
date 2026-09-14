package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ferrum/internal/pbs"
	"ferrum/internal/pve"
)

type connectionDTO struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"` // "pve" | "pbs"
	Host               string `json:"host"`
	Port               int    `json:"port"`
	AuthType           string `json:"authType"`
	Username           string `json:"username,omitempty"`
	TokenID            string `json:"tokenId,omitempty"`
	VerifyTLS          bool   `json:"verifyTls"`
	TLSFingerprint     string `json:"tlsFingerprint,omitempty"` // SHA-256 pin; empty = unset
	BehindReverseProxy bool   `json:"behindReverseProxy"`
	CreatedAt          string `json:"createdAt"`

	// SSH credentials for this connection's own host, used by Inventory's
	// SSH Shell to connect without retyping a password each time. Only the
	// non-secret shape is ever returned — no password/key, same as the PVE
	// credentials above never round-trip either.
	SSHUsername string `json:"sshUsername,omitempty"`
	SSHPort     int    `json:"sshPort,omitempty"`
	SSHAuthType string `json:"sshAuthType,omitempty"` // "" | "password" | "key"
}

type createConnectionRequest struct {
	Name               string `json:"name"`
	Type               string `json:"type,omitempty"` // "pve" (default) | "pbs"
	Host               string `json:"host"`
	Port               int    `json:"port"`
	AuthType           string `json:"authType"` // "token" | "password"
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	TokenID            string `json:"tokenId,omitempty"`
	TokenSecret        string `json:"tokenSecret,omitempty"`
	VerifyTLS          bool   `json:"verifyTls"`
	TLSFingerprint     string `json:"tlsFingerprint,omitempty"`
	BehindReverseProxy bool   `json:"behindReverseProxy"`

	// SSH credentials, all optional — omit sshAuthType (or leave it "") for
	// no SSH access configured. "key" expects an unencrypted PEM private
	// key in SSHPrivateKey (passphrase-protected keys aren't supported yet).
	SSHUsername   string `json:"sshUsername,omitempty"`
	SSHPort       int    `json:"sshPort,omitempty"`
	SSHAuthType   string `json:"sshAuthType,omitempty"` // "" | "password" | "key"
	SSHPassword   string `json:"sshPassword,omitempty"`
	SSHPrivateKey string `json:"sshPrivateKey,omitempty"`
}

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, type, host, port, auth_type, COALESCE(username,''), COALESCE(token_id,''), verify_tls, COALESCE(tls_fingerprint,''), behind_reverse_proxy, created_at,
			COALESCE(ssh_username,''), COALESCE(ssh_port,22), COALESCE(ssh_auth_type,'')
		FROM connections ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []connectionDTO{}
	for rows.Next() {
		var c connectionDTO
		var verify, reverse int
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Host, &c.Port, &c.AuthType, &c.Username, &c.TokenID, &verify, &c.TLSFingerprint, &reverse, &c.CreatedAt,
			&c.SSHUsername, &c.SSHPort, &c.SSHAuthType); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		c.VerifyTLS = verify == 1
		c.BehindReverseProxy = reverse == 1
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	var req createConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Type == "" {
		req.Type = "pve"
	}
	if req.Port == 0 {
		req.Port = defaultPortFor(req.Type)
	}
	if err := validateConnectionRequest(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
		return
	}

	var tokenSecretEnc, passwordEnc string
	var err error
	if req.AuthType == "token" {
		if tokenSecretEnc, err = s.secrets.Encrypt(req.TokenSecret); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		req.AuthType = "password"
		if passwordEnc, err = s.secrets.Encrypt(req.Password); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	var sshSecretEnc string
	if req.SSHAuthType != "" {
		if err := validateSSHFields(req.SSHAuthType, req.SSHUsername, req.SSHPassword, req.SSHPrivateKey); err != nil {
			writeErrorMsg(w, http.StatusBadRequest, err.Error())
			return
		}
		secret := req.SSHPassword
		if req.SSHAuthType == "key" {
			secret = req.SSHPrivateKey
		}
		if sshSecretEnc, err = s.secrets.Encrypt(secret); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if req.SSHPort == 0 {
			req.SSHPort = 22
		}
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO connections (id, name, type, host, port, auth_type, token_id, token_secret_enc, username, password_enc, verify_tls, tls_fingerprint, behind_reverse_proxy, created_at, updated_at, ssh_username, ssh_port, ssh_auth_type, ssh_secret_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Type, req.Host, req.Port, req.AuthType, req.TokenID, tokenSecretEnc, req.Username, passwordEnc,
		boolToInt(req.VerifyTLS), req.TLSFingerprint, boolToInt(req.BehindReverseProxy), now, now,
		req.SSHUsername, req.SSHPort, req.SSHAuthType, sshSecretEnc,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "connections.create", "connections", req.Name)
	slog.Info("connection created", "id", id, "name", req.Name, "type", req.Type, "host", req.Host, "authType", req.AuthType)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// defaultPortFor returns the conventional API port for a connection type
// when the caller didn't specify one — 8006 for PVE, 8007 for PBS.
func defaultPortFor(connType string) int {
	if connType == "pbs" {
		return 8007
	}
	return 8006
}

// updateConnectionRequest mirrors createConnectionRequest but every field is
// optional — only fields the caller sends are changed, so editing just the
// name doesn't force re-entering a password/token.
type updateConnectionRequest struct {
	Name        *string `json:"name,omitempty"`
	Type        *string `json:"type,omitempty"`
	Host        *string `json:"host,omitempty"`
	Port        *int    `json:"port,omitempty"`
	AuthType    *string `json:"authType,omitempty"`
	Username    *string `json:"username,omitempty"`
	Password    *string `json:"password,omitempty"`
	TokenID     *string `json:"tokenId,omitempty"`
	TokenSecret *string `json:"tokenSecret,omitempty"`
	VerifyTLS   *bool   `json:"verifyTls,omitempty"`
	// TLSFingerprint pins the server certificate (SHA-256 hex). A pointer so
	// absent (nil — untouched) differs from explicitly sent empty (clear the
	// pin), mirroring how tags/notes clearing works on guest config.
	TLSFingerprint     *string `json:"tlsFingerprint,omitempty"`
	BehindReverseProxy *bool   `json:"behindReverseProxy,omitempty"`

	// SSHAuthType "" (explicitly sent empty, not omitted) clears SSH access
	// entirely — same "pointer means touched" shape as the fields above.
	SSHUsername   *string `json:"sshUsername,omitempty"`
	SSHPort       *int    `json:"sshPort,omitempty"`
	SSHAuthType   *string `json:"sshAuthType,omitempty"`
	SSHPassword   *string `json:"sshPassword,omitempty"`
	SSHPrivateKey *string `json:"sshPrivateKey,omitempty"`
}

func (s *Server) updateConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	sets := []string{}
	args := []any{}
	set := func(col string, val any) {
		sets = append(sets, col+" = ?")
		args = append(args, val)
	}

	if req.Name != nil {
		set("name", *req.Name)
	}
	if req.Type != nil {
		if *req.Type != "pve" && *req.Type != "pbs" {
			writeErrorMsg(w, http.StatusBadRequest, `type must be "pve" or "pbs"`)
			return
		}
		set("type", *req.Type)
	}
	if req.Host != nil {
		set("host", *req.Host)
	}
	if req.Port != nil {
		set("port", *req.Port)
	}
	if req.AuthType != nil {
		if *req.AuthType != "token" && *req.AuthType != "password" {
			writeErrorMsg(w, http.StatusBadRequest, `authType must be "token" or "password"`)
			return
		}
		set("auth_type", *req.AuthType)
	}
	if req.Port != nil && (*req.Port < 1 || *req.Port > 65535) {
		writeErrorMsg(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.Username != nil {
		set("username", *req.Username)
	}
	if req.TokenID != nil {
		set("token_id", *req.TokenID)
	}
	if req.VerifyTLS != nil {
		set("verify_tls", boolToInt(*req.VerifyTLS))
	}
	if req.TLSFingerprint != nil {
		// Stored as given; matching normalizes case/colons at compare time.
		set("tls_fingerprint", *req.TLSFingerprint)
	}
	if req.BehindReverseProxy != nil {
		set("behind_reverse_proxy", boolToInt(*req.BehindReverseProxy))
	}
	if req.Password != nil && *req.Password != "" {
		enc, err := s.secrets.Encrypt(*req.Password)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("password_enc", enc)
	}
	if req.TokenSecret != nil && *req.TokenSecret != "" {
		enc, err := s.secrets.Encrypt(*req.TokenSecret)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("token_secret_enc", enc)
	}
	if req.SSHUsername != nil {
		set("ssh_username", *req.SSHUsername)
	}
	if req.SSHPort != nil {
		set("ssh_port", *req.SSHPort)
	}
	if req.SSHAuthType != nil {
		if *req.SSHAuthType != "" && *req.SSHAuthType != "password" && *req.SSHAuthType != "key" {
			writeErrorMsg(w, http.StatusBadRequest, `sshAuthType must be "", "password", or "key"`)
			return
		}
		set("ssh_auth_type", *req.SSHAuthType)
		if *req.SSHAuthType == "" {
			// Clearing SSH access entirely — drop the stored secret too,
			// not just the type flag.
			set("ssh_secret_enc", "")
		}
	}
	if req.SSHPassword != nil && *req.SSHPassword != "" {
		enc, err := s.secrets.Encrypt(*req.SSHPassword)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("ssh_secret_enc", enc)
	}
	if req.SSHPrivateKey != nil && *req.SSHPrivateKey != "" {
		enc, err := s.secrets.Encrypt(*req.SSHPrivateKey)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("ssh_secret_enc", enc)
	}
	if len(sets) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "no fields to update")
		return
	}
	set("updated_at", time.Now().UTC().Format(time.RFC3339))

	query := "UPDATE connections SET "
	for i, s := range sets {
		if i > 0 {
			query += ", "
		}
		query += s
	}
	query += " WHERE id = ?"
	args = append(args, id)

	res, err := s.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "connection not found")
		return
	}
	// Drop any cached ticket so edited credentials (or a demoted verify-TLS
	// setting) take effect immediately instead of up to ticketTTL later.
	s.connections.Invalidate(id)
	s.audit(r, "connections.update", "connections", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM connections WHERE id = ?`, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "connection not found")
		return
	}
	// Deleting a connection is meant to revoke access to it now — drop the
	// cached client so a stale ticket can't keep authenticating afterward.
	s.connections.Invalidate(id)
	s.audit(r, "connections.delete", "connections", id)
	slog.Info("connection deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	var req createConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Type == "" {
		req.Type = "pve"
	}
	if req.Port == 0 {
		req.Port = defaultPortFor(req.Type)
	}
	if err := validateConnectionRequest(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Type == "pbs" {
		client := pbs.New(req.Host, req.Port, pbs.WithInsecureSkipVerify(!req.VerifyTLS))
		if req.AuthType == "token" {
			client.WithAPIToken(req.TokenID, req.TokenSecret)
		} else if err := client.Login(r.Context(), req.Username, req.Password); err != nil {
			writeErrorMsg(w, http.StatusBadGateway, "login failed: "+err.Error())
			return
		}
		version, err := client.Version(r.Context())
		if err != nil {
			writeErrorMsg(w, http.StatusBadGateway, "connection failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
		return
	}

	client := pve.New(req.Host, req.Port, pve.WithInsecureSkipVerify(!req.VerifyTLS))
	if req.AuthType == "token" {
		client.WithAPIToken(req.TokenID, req.TokenSecret)
	} else if err := client.Login(r.Context(), req.Username, req.Password); err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "login failed: "+err.Error())
		return
	}

	version, err := client.Version(r.Context())
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "connection failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
}

// validateConnectionRequest enforces the request shape shared by create and
// test: a name, a host, a sane port, and a known auth type with credentials.
func validateConnectionRequest(req *createConnectionRequest) error {
	if req.Name == "" || req.Host == "" {
		return fmt.Errorf("name and host are required")
	}
	if req.Type != "" && req.Type != "pve" && req.Type != "pbs" {
		return fmt.Errorf(`type must be "pve" or "pbs"`)
	}
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if req.AuthType != "token" && req.AuthType != "password" {
		return fmt.Errorf(`authType must be "token" or "password"`)
	}
	if req.AuthType == "token" && (req.TokenID == "" || req.TokenSecret == "") {
		return fmt.Errorf("tokenId and tokenSecret are required for token auth")
	}
	if req.AuthType == "password" && req.Username == "" {
		return fmt.Errorf("username is required for password auth")
	}
	return nil
}

// validateSSHFields enforces the SSH credential shape when sshAuthType is
// set at all — mirrors validateConnectionRequest's auth-type branching for
// the Proxmox credentials, one level down.
func validateSSHFields(authType, username, password, privateKey string) error {
	if authType != "password" && authType != "key" {
		return fmt.Errorf(`sshAuthType must be "password" or "key"`)
	}
	if username == "" {
		return fmt.Errorf("sshUsername is required when SSH access is configured")
	}
	if authType == "password" && password == "" {
		return fmt.Errorf("sshPassword is required for SSH password auth")
	}
	if authType == "key" && privateKey == "" {
		return fmt.Errorf("sshPrivateKey is required for SSH key auth")
	}
	return nil
}

// clientFor builds an authenticated pve.Client for a stored connection,
// reusing the resolver's cached ticket when one is live.
func (s *Server) clientFor(ctx context.Context, id string) (*pve.Client, error) {
	return s.connections.ClientFor(ctx, id)
}

// pbsClientFor builds an authenticated pbs.Client for a stored connection,
// rejecting one that isn't type=pbs so a pve-typed connection id can't be
// used to reach the PBS handlers (and vice versa via clientFor).
func (s *Server) pbsClientFor(ctx context.Context, id string) (*pbs.Client, error) {
	connType, err := s.connectionType(ctx, id)
	if err != nil {
		return nil, err
	}
	if connType != "pbs" {
		return nil, fmt.Errorf("connection %s is not a PBS connection", id)
	}
	return s.connections.PBSClientFor(ctx, id)
}

// connectionType looks up a stored connection's type ("pve" | "pbs").
func (s *Server) connectionType(ctx context.Context, id string) (string, error) {
	var connType string
	err := s.db.QueryRowContext(ctx, `SELECT type FROM connections WHERE id = ?`, id).Scan(&connType)
	return connType, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
