package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

func (s *Server) nodeStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.NodeStatus(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) nodeDisks(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	disks, err := client.NodeDisks(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, disks)
}

func (s *Server) diskSMART(w http.ResponseWriter, r *http.Request) {
	devpath := r.URL.Query().Get("disk")
	if devpath == "" {
		writeErrorMsg(w, http.StatusBadRequest, "disk query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	smart, err := client.DiskSMART(r.Context(), chi.URLParam(r, "node"), devpath)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, smart)
}

func (s *Server) rebootNode(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RebootNode(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.reboot", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) shutdownNode(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.ShutdownNode(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.shutdown", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) nodeStorageList(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	storages, err := client.NodeStorage(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, storages)
}

func (s *Server) storageContent(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	items, err := client.StorageContent(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "storage"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) deleteStorageContent(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node, storage, volid := chi.URLParam(r, "node"), chi.URLParam(r, "storage"), chi.URLParam(r, "volid")
	if err := client.DeleteStorageContent(r.Context(), node, storage, volid); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.delete-content", "storage", node+"/"+storage+"/"+volid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) nodeNetwork(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	ifaces, err := client.NodeNetwork(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ifaces)
}

func (s *Server) aptUpdates(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	updates, err := client.AptUpdates(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, updates)
}

func (s *Server) aptRefresh(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RefreshAptIndex(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) aptUpgrade(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.UpgradeNode(r.Context(), node)
	if err != nil {
		if errors.Is(err, pve.ErrUpgradeNotSupported) {
			s.writeError(w, http.StatusNotImplemented, err)
			return
		}
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.upgrade", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) nodeSyslog(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	entries, err := client.NodeSyslog(r.Context(), chi.URLParam(r, "node"), 200)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) nodeFirewallRules(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rules, err := client.NodeFirewallRules(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) addNodeFirewallRule(w http.ResponseWriter, r *http.Request) {
	var req newFirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Type == "" || req.Action == "" {
		writeErrorMsg(w, http.StatusBadRequest, "type and action are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rule := pve.NewFirewallRule{
		Type: req.Type, Action: req.Action, Source: req.Source, Dest: req.Dest,
		Proto: req.Proto, Dport: req.Dport, Sport: req.Sport, Macro: req.Macro,
		Comment: req.Comment, Enable: req.Enable,
	}
	if err := client.AddNodeFirewallRule(r.Context(), chi.URLParam(r, "node"), rule); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.create", "firewall", "node:"+chi.URLParam(r, "node"))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteNodeFirewallRule(w http.ResponseWriter, r *http.Request) {
	pos, err := strconv.Atoi(chi.URLParam(r, "pos"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteNodeFirewallRule(r.Context(), chi.URLParam(r, "node"), pos); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.delete", "firewall", "node:"+chi.URLParam(r, "node"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) nodeReplicationStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.NodeReplicationStatus(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// cephNotConfigured ends a Ceph request where the target server has no
// Ceph (not installed, or the endpoint isn't implemented on its PVE
// version). 204 reads as "no data" to the frontend — which shows "Not
// configured" — instead of an error state and a 502 in the logs.
func cephNotConfigured(w http.ResponseWriter, err error) bool {
	if !pve.IsNotAvailable(err) {
		return false
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

func (s *Server) cephStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.CephClusterStatus(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) cephPools(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	pools, err := client.CephPools(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) cephOSDs(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	osds, err := client.CephOSDs(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, osds)
}

func (s *Server) taskLog(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	lines, err := client.TaskLog(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "upid"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CancelTask(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "upid")); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
