package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// HA groups (PVE 8) — create/update/delete. Listing is haGroups in cluster.go.

type haGroupRequest struct {
	Group      string `json:"group"`
	Nodes      string `json:"nodes"`
	Restricted bool   `json:"restricted,omitempty"`
	Nofailback bool   `json:"nofailback,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

func (s *Server) createHAGroup(w http.ResponseWriter, r *http.Request) {
	var req haGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Group == "" || req.Nodes == "" {
		writeErrorMsg(w, http.StatusBadRequest, "group and nodes are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateHAGroup(r.Context(), req.Group, req.Nodes, req.Restricted, req.Nofailback, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.group.create", "ha", req.Group)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateHAGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	group := chi.URLParam(r, "group")
	if err := client.UpdateHAGroup(r.Context(), group, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.group.update", "ha", group)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteHAGroup(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	group := chi.URLParam(r, "group")
	if err := client.DeleteHAGroup(r.Context(), group); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.group.delete", "ha", group)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HA rules (PVE 9) — full CRUD, /cluster/ha/rules.

func (s *Server) listHARules(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rules, err := client.HARules(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

type haRuleRequest struct {
	Rule    string            `json:"rule"`
	Options map[string]string `json:"options"`
}

func (s *Server) createHARule(w http.ResponseWriter, r *http.Request) {
	var req haRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Rule == "" || req.Options["type"] == "" {
		writeErrorMsg(w, http.StatusBadRequest, "rule and options.type are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateHARule(r.Context(), req.Rule, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.rule.create", "ha", req.Rule)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateHARule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	id := chi.URLParam(r, "ruleId")
	if err := client.UpdateHARule(r.Context(), id, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.rule.update", "ha", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteHARule(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	id := chi.URLParam(r, "ruleId")
	if err := client.DeleteHARule(r.Context(), id); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.rule.delete", "ha", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
