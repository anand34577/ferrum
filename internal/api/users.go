package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type userDTO struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	Email     string   `json:"email"`
	IsAdmin   bool     `json:"isAdmin"`
	CreatedAt string   `json:"createdAt"`
	Roles     []string `json:"roles"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, username, email, is_admin, created_at FROM users ORDER BY username`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	byID := map[string]*userDTO{}
	out := []userDTO{}
	for rows.Next() {
		var u userDTO
		var isAdmin int
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &isAdmin, &u.CreatedAt); err != nil {
			rows.Close()
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		u.IsAdmin = isAdmin == 1
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	rows.Close() // must release the single sqlite connection before issuing the role-assignment query below

	for i := range out {
		byID[out[i].ID] = &out[i]
	}

	roleRows, err := s.db.QueryContext(r.Context(), `
		SELECT ur.user_id, rr.name FROM rbac_user_roles ur JOIN rbac_roles rr ON rr.id = ur.role_id`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer roleRows.Close()
	for roleRows.Next() {
		var userID, roleName string
		if err := roleRows.Scan(&userID, &roleName); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if u, ok := byID[userID]; ok {
			u.Roles = append(u.Roles, roleName)
		}
	}
	if err := roleRows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"isAdmin"`
	RoleID   string `json:"roleId,omitempty"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Username == "" || req.Email == "" || len(req.Password) < 8 {
		writeErrorMsg(w, http.StatusBadRequest, "username, email, and an 8+ character password are required")
		return
	}

	var exists int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE username = ? OR email = ?`, req.Username, req.Email).Scan(&exists); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if exists > 0 {
		writeErrorMsg(w, http.StatusConflict, "a user with that username or email already exists")
		return
	}

	// Validate the role before creating anything, so a bad id can't leave a
	// half-set-up account behind.
	if req.RoleID != "" {
		var roleExists int
		if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM rbac_roles WHERE id = ?`, req.RoleID).Scan(&roleExists); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if roleExists == 0 {
			writeErrorMsg(w, http.StatusBadRequest, "unknown role")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (id, username, email, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.Username, req.Email, string(hash), boolToInt(req.IsAdmin), now, now,
	); err != nil {
		// The pre-check above is inherently racy: a concurrent create can
		// land between it and the INSERT and fail it here. Re-check and
		// report the friendly conflict instead of a 500; anything else is
		// a genuine server fault.
		var nowExists int
		if cerr := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE username = ? OR email = ?`, req.Username, req.Email).Scan(&nowExists); cerr == nil && nowExists > 0 {
			writeErrorMsg(w, http.StatusConflict, "a user with that username or email already exists")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if req.RoleID != "" {
		if _, err := s.db.ExecContext(r.Context(),
			`INSERT INTO rbac_user_roles (id, user_id, role_id, scope_type, created_at) VALUES (?, ?, ?, 'global', ?)`,
			uuid.NewString(), id, req.RoleID, now,
		); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	s.audit(r, "users.create", "admin", req.Username)
	slog.Info("user created", "id", id, "username", req.Username, "isAdmin", req.IsAdmin)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type updateUserRequest struct {
	Email    *string `json:"email,omitempty"`
	Password *string `json:"password,omitempty"`
	IsAdmin  *bool   `json:"isAdmin,omitempty"`
	RoleID   *string `json:"roleId,omitempty"`
}

// updateUser edits an existing account: email, password reset, admin flag,
// and role assignment. Only fields the caller sends are changed. Demoting
// the last remaining admin is refused so the instance can never lock
// itself out of administration.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	var username string
	var currentAdmin int
	if err := s.db.QueryRowContext(r.Context(), `SELECT username, is_admin FROM users WHERE id = ?`, id).Scan(&username, &currentAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorMsg(w, http.StatusNotFound, "user not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if req.IsAdmin != nil && !*req.IsAdmin && currentAdmin == 1 {
		var admins int
		if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&admins); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if admins <= 1 {
			writeErrorMsg(w, http.StatusConflict, "cannot demote the last admin")
			return
		}
	}

	sets := []string{}
	args := []any{}
	set := func(col string, val any) {
		sets = append(sets, col+" = ?")
		args = append(args, val)
	}

	if req.Email != nil {
		if *req.Email == "" {
			writeErrorMsg(w, http.StatusBadRequest, "email cannot be empty")
			return
		}
		var exists int
		if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE email = ? AND id != ?`, *req.Email, id).Scan(&exists); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if exists > 0 {
			writeErrorMsg(w, http.StatusConflict, "another user already uses that email")
			return
		}
		set("email", *req.Email)
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			writeErrorMsg(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		// A password reset revokes the account's existing sessions.
		if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("password_hash", string(hash))
	}
	if req.IsAdmin != nil {
		set("is_admin", boolToInt(*req.IsAdmin))
	}
	// Validate the role before touching anything, so a bad id can't clear
	// the user's existing assignment (the DELETE below runs unconditionally
	// once we get that far).
	if req.RoleID != nil && *req.RoleID != "" {
		var roleExists int
		if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM rbac_roles WHERE id = ?`, *req.RoleID).Scan(&roleExists); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if roleExists == 0 {
			writeErrorMsg(w, http.StatusBadRequest, "unknown role")
			return
		}
	}
	if len(sets) == 0 && req.RoleID == nil {
		writeErrorMsg(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if len(sets) > 0 {
		set("updated_at", time.Now().UTC().Format(time.RFC3339))
		query := "UPDATE users SET "
		for i, c := range sets {
			if i > 0 {
				query += ", "
			}
			query += c
		}
		query += " WHERE id = ?"
		args = append(args, id)
		if _, err := s.db.ExecContext(r.Context(), query, args...); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if req.RoleID != nil {
		if _, err := s.db.ExecContext(r.Context(), `DELETE FROM rbac_user_roles WHERE user_id = ?`, id); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if *req.RoleID != "" {
			if _, err := s.db.ExecContext(r.Context(),
				`INSERT INTO rbac_user_roles (id, user_id, role_id, scope_type, created_at) VALUES (?, ?, ?, 'global', ?)`,
				uuid.NewString(), id, *req.RoleID, time.Now().UTC().Format(time.RFC3339),
			); err != nil {
				s.writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
	}

	s.audit(r, "users.update", "admin", username)
	slog.Info("user updated", "id", id, "username", username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if u := userFromContext(r); u != nil && u.ID == id {
		writeErrorMsg(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}

	var isAdmin int
	if err := s.db.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id = ?`, id).Scan(&isAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorMsg(w, http.StatusNotFound, "user not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if isAdmin == 1 {
		var admins int
		if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&admins); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if admins <= 1 {
			writeErrorMsg(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
	}

	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "users.delete", "admin", id)
	slog.Warn("user deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

type roleDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	IsSystem    bool   `json:"isSystem"`
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, COALESCE(description,''), COALESCE(color,''), is_system FROM rbac_roles ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []roleDTO{}
	for rows.Next() {
		var role roleDTO
		var isSystem int
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Color, &isSystem); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		role.IsSystem = isSystem == 1
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT a.id, COALESCE(u.username, 'system'), a.action, a.category, COALESCE(a.target,''), a.ip, a.created_at
		FROM audit_log a LEFT JOIN users u ON u.id = a.user_id
		ORDER BY a.created_at DESC LIMIT 200`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type entry struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Action    string `json:"action"`
		Category  string `json:"category"`
		Target    string `json:"target,omitempty"`
		IP        string `json:"ip,omitempty"`
		CreatedAt string `json:"createdAt"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Username, &e.Action, &e.Category, &e.Target, &e.IP, &e.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
