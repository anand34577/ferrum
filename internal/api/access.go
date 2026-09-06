package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Read-only visibility into Proxmox's own user/permission system — realms,
// accounts, roles, and ACLs — distinct from this app's local user/role
// tables (see users.go), which only govern access to Ferrum itself. No
// mutation here: creating/editing PVE users, tokens, or ACLs stays in
// Proxmox's own UI.

func (s *Server) pveAccessUsers(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	users, err := client.AccessUsers(r.Context(), true)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) pveAccessRoles(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	roles, err := client.AccessRoles(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) pveAccessACL(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	acl, err := client.AccessACL(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, acl)
}

func (s *Server) pveAccessDomains(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	domains, err := client.AccessDomains(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, domains)
}
