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

	"ferrum/internal/pve"
)

type connectionDTO struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	AuthType           string `json:"authType"`
	Username           string `json:"username,omitempty"`
	TokenID            string `json:"tokenId,omitempty"`
	VerifyTLS          bool   `json:"verifyTls"`
	BehindReverseProxy bool   `json:"behindReverseProxy"`
	CreatedAt          string `json:"createdAt"`
}

type createConnectionRequest struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	AuthType           string `json:"authType"` // "token" | "password"
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	TokenID            string `json:"tokenId,omitempty"`
	TokenSecret        string `json:"tokenSecret,omitempty"`
	VerifyTLS          bool   `json:"verifyTls"`
	BehindReverseProxy bool   `json:"behindReverseProxy"`
}

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, host, port, auth_type, COALESCE(username,''), COALESCE(token_id,''), verify_tls, behind_reverse_proxy, created_at
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Host, &c.Port, &c.AuthType, &c.Username, &c.TokenID, &verify, &reverse, &c.CreatedAt); err != nil {
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
	if req.Port == 0 {
		req.Port = 8006
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

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO connections (id, name, host, port, auth_type, token_id, token_secret_enc, username, password_enc, verify_tls, behind_reverse_proxy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Host, req.Port, req.AuthType, req.TokenID, tokenSecretEnc, req.Username, passwordEnc,
		boolToInt(req.VerifyTLS), boolToInt(req.BehindReverseProxy), now, now,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "connections.create", "connections", req.Name)
	slog.Info("connection created", "id", id, "name", req.Name, "host", req.Host, "authType", req.AuthType)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// updateConnectionRequest mirrors createConnectionRequest but every field is
// optional — only fields the caller sends are changed, so editing just the
// name doesn't force re-entering a password/token.
type updateConnectionRequest struct {
	Name               *string `json:"name,omitempty"`
	Host               *string `json:"host,omitempty"`
	Port               *int    `json:"port,omitempty"`
	AuthType           *string `json:"authType,omitempty"`
	Username           *string `json:"username,omitempty"`
	Password           *string `json:"password,omitempty"`
	TokenID            *string `json:"tokenId,omitempty"`
	TokenSecret        *string `json:"tokenSecret,omitempty"`
	VerifyTLS          *bool   `json:"verifyTls,omitempty"`
	BehindReverseProxy *bool   `json:"behindReverseProxy,omitempty"`
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

	if _, err := s.db.ExecContext(r.Context(), query, args...); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
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
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM connections WHERE id = ?`, id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
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
	if req.Port == 0 {
		req.Port = 8006
	}
	if err := validateConnectionRequest(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
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

// clientFor builds an authenticated pve.Client for a stored connection,
// reusing the resolver's cached ticket when one is live.
func (s *Server) clientFor(ctx context.Context, id string) (*pve.Client, error) {
	return s.connections.ClientFor(ctx, id)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
