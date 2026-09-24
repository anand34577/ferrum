package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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

func (s *Server) setBackupProtected(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Protected bool `json:"protected"`
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
	node, storage, volid := chi.URLParam(r, "node"), chi.URLParam(r, "storage"), chi.URLParam(r, "volid")
	if err := client.SetBackupProtected(r.Context(), node, storage, volid, req.Protected); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "backup.set-protected", "storage", node+"/"+storage+"/"+volid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *Server) cephMons(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	mons, err := client.CephMons(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, mons)
}

func (s *Server) createCephMon(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.CreateCephMon(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ceph.mon.create", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) deleteCephMon(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteCephMon(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "monid")); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ceph.mon.delete", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) cephMgrs(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	mgrs, err := client.CephMgrs(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, mgrs)
}

func (s *Server) createCephMgr(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.CreateCephMgr(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ceph.mgr.create", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) deleteCephMgr(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteCephMgr(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "mgrid")); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ceph.mgr.delete", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) cephFilesystems(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	fs, err := client.CephFilesystems(r.Context(), chi.URLParam(r, "node"))
	if cephNotConfigured(w, err) {
		return
	}
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *Server) createCephFilesystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.CreateCephFilesystem(r.Context(), chi.URLParam(r, "node"), req.Name)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ceph.fs.create", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// --- PBS file-level restore ---

func (s *Server) fileRestoreList(w http.ResponseWriter, r *http.Request) {
	volume := r.URL.Query().Get("volume")
	if volume == "" {
		writeErrorMsg(w, http.StatusBadRequest, "volume query parameter is required")
		return
	}
	path := r.URL.Query().Get("path")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	entries, err := client.FileRestoreList(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "storage"), volume, path)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// fileRestoreDownload proxies a single-file (or zipped directory) download
// out of a PBS backup archive — streamed straight through rather than
// buffered, since a restored file/zip can be large.
func (s *Server) fileRestoreDownload(w http.ResponseWriter, r *http.Request) {
	volume := r.URL.Query().Get("volume")
	path := r.URL.Query().Get("path")
	if volume == "" || path == "" {
		writeErrorMsg(w, http.StatusBadRequest, "volume and path query parameters are required")
		return
	}
	// A large restored file can take well past the 30s global request
	// timeout to stream; detach from that inherited deadline the same way
	// /mcp and /ai/chat do, and apply a generous one of our own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	client, err := s.clientFor(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	resp, err := client.FileRestoreDownload(ctx, chi.URLParam(r, "node"), chi.URLParam(r, "storage"), volume, path)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	filename := path[strings.LastIndex(path, "/")+1:]
	if filename == "" {
		filename = "restore"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// Lift the server's 60s WriteTimeout, or a large file is silently
	// truncated after the 200 has already gone out.
	clearWriteDeadline(w)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

// --- Node DNS / time / hosts ---

func (s *Server) nodeDNS(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	cfg, err := client.NodeDNS(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) updateNodeDNS(w http.ResponseWriter, r *http.Request) {
	var req pve.NodeDNSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateNodeDNS(r.Context(), chi.URLParam(r, "node"), req); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.dns.update", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) nodeTime(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	info, err := client.NodeTime(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) updateNodeTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Timezone == "" {
		writeErrorMsg(w, http.StatusBadRequest, "timezone is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.SetNodeTimezone(r.Context(), chi.URLParam(r, "node"), req.Timezone); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.timezone.update", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) nodeHosts(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	hosts, err := client.NodeHosts(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) updateNodeHosts(w http.ResponseWriter, r *http.Request) {
	var req pve.NodeHosts
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateNodeHosts(r.Context(), chi.URLParam(r, "node"), req.Data, req.Digest); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.hosts.update", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Node certificates ---

func (s *Server) nodeCertificates(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	certs, err := client.NodeCertificates(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, certs)
}

func (s *Server) uploadNodeCertificate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Certificate string `json:"certificate"`
		Key         string `json:"key,omitempty"`
		Force       bool   `json:"force,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Certificate == "" {
		writeErrorMsg(w, http.StatusBadRequest, "certificate is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UploadCustomCertificate(r.Context(), chi.URLParam(r, "node"), req.Certificate, req.Key, req.Force); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.certificate.upload", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteNodeCertificate(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteCustomCertificate(r.Context(), chi.URLParam(r, "node")); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.certificate.delete", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) orderAcmeCertificate(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.AcmeOrderCertificate(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.certificate.acme.order", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) revokeAcmeCertificate(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RevokeAcmeCertificate(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.certificate.acme.revoke", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) wakeOnLan(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	result, err := client.WakeOnLan(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.wakeonlan", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}

func (s *Server) startAllGuests(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.StartAllGuests(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.startall", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) stopAllGuests(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "node")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.StopAllGuests(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.stopall", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) nodeJournal(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	entries, err := client.NodeJournal(r.Context(), chi.URLParam(r, "node"), 500)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// --- Node services (systemd units PVE manages: pveproxy, pvedaemon, ...) ---

func (s *Server) nodeServices(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	services, err := client.NodeServices(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, services)
}

func (s *Server) nodeServiceState(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	state, err := client.NodeServiceState(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "service"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) nodeServiceAction(w http.ResponseWriter, r *http.Request) {
	action := chi.URLParam(r, "action")
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		writeErrorMsg(w, http.StatusBadRequest, "action must be one of start, stop, restart, reload")
		return
	}
	node, service := chi.URLParam(r, "node"), chi.URLParam(r, "service")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.NodeServiceAction(r.Context(), node, service, action); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "node.service."+action, "node", node+"/"+service)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// uploadStorageContentLimit bounds an ISO/template upload — generous enough
// for a real installer image, unlike the 2 MiB JSON body cap the rest of the
// API uses.
const uploadStorageContentLimit = 8 << 30 // 8 GiB

func (s *Server) uploadStorageContent(w http.ResponseWriter, r *http.Request) {
	// A multi-GB body outlasts the server's 60s Read/WriteTimeout (see
	// cmd/ferrum/main.go); lift both for this request or the upload dies
	// mid-stream with "i/o timeout".
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	r.Body = http.MaxBytesReader(w, r.Body, uploadStorageContentLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "invalid upload: "+err.Error())
		return
	}
	content := r.FormValue("content")
	if content != "iso" && content != "vztmpl" {
		writeErrorMsg(w, http.StatusBadRequest, `content must be "iso" or "vztmpl"`)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	// A multi-GB ISO/template upload can take well past the 30s global
	// request timeout to stream to Proxmox; detach from that inherited
	// deadline the same way /mcp and /ai/chat do, and apply a generous one
	// of our own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	client, err := s.clientFor(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node, storage := chi.URLParam(r, "node"), chi.URLParam(r, "storage")
	upid, err := client.UploadStorageContent(ctx, node, storage, content, header.Filename, file)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.upload", "storage", node+"/"+storage+"/"+header.Filename)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) downloadURLToStorage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content           string `json:"content"`
		Filename          string `json:"filename"`
		URL               string `json:"url"`
		Checksum          string `json:"checksum,omitempty"`
		ChecksumAlgorithm string `json:"checksumAlgorithm,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Content != "iso" && req.Content != "vztmpl" {
		writeErrorMsg(w, http.StatusBadRequest, `content must be "iso" or "vztmpl"`)
		return
	}
	if req.Filename == "" || req.URL == "" {
		writeErrorMsg(w, http.StatusBadRequest, "filename and url are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node, storage := chi.URLParam(r, "node"), chi.URLParam(r, "storage")
	upid, err := client.DownloadURLToStorage(r.Context(), node, storage, req.Content, req.Filename, req.URL, req.Checksum, req.ChecksumAlgorithm)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.download-url", "storage", node+"/"+storage+"/"+req.Filename)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// --- Cluster membership ---

func (s *Server) clusterConfigNodes(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	nodes, err := client.ClusterConfigNodes(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) clusterJoinInfo(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	info, err := client.ClusterJoinInfo(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) createCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterName string `json:"clusterName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ClusterName == "" {
		writeErrorMsg(w, http.StatusBadRequest, "clusterName is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateCluster(r.Context(), req.ClusterName); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "cluster.create", "connection", chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) joinCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname    string `json:"hostname"`
		Fingerprint string `json:"fingerprint"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Hostname == "" || req.Fingerprint == "" || req.Password == "" {
		writeErrorMsg(w, http.StatusBadRequest, "hostname, fingerprint and password are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.JoinCluster(r.Context(), req.Hostname, req.Fingerprint, req.Password); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "cluster.join", "connection", chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) removeClusterNode(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.RemoveClusterNode(r.Context(), chi.URLParam(r, "node")); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "cluster.node.remove", "node", chi.URLParam(r, "node"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// taskStatus is the single-task status check — cheaper than nodeTasks'
// full listing (which caps at limit=100 and can drop an older UPID off the
// end) for a client polling "is my UPID done yet?".
func (s *Server) taskStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	task, err := client.TaskStatus(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "upid"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
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
	s.audit(r, "node.task.cancel", "node", chi.URLParam(r, "node")+"/"+chi.URLParam(r, "upid"))
	w.WriteHeader(http.StatusNoContent)
}
