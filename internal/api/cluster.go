package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

func (s *Server) clusterStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.ClusterStatus(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// clusterTasks is the cluster-wide task list. It replaces the client-side
// habit of calling /nodes/{node}/tasks once per node on every poll tick:
// PVE already aggregates this, so an N-node cluster costs one upstream call.
func (s *Server) clusterTasks(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	tasks, err := client.ClusterTasks(r.Context(), 200)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) clusterLog(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	entries, err := client.ClusterLog(r.Context(), 200)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) clusterFirewallRules(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rules, err := client.ClusterFirewallRules(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

type newFirewallRuleRequest struct {
	Type    string `json:"type"`
	Action  string `json:"action"`
	Source  string `json:"source,omitempty"`
	Dest    string `json:"dest,omitempty"`
	Proto   string `json:"proto,omitempty"`
	Dport   string `json:"dport,omitempty"`
	Sport   string `json:"sport,omitempty"`
	Macro   string `json:"macro,omitempty"`
	Comment string `json:"comment,omitempty"`
	Enable  bool   `json:"enable"`
}

func (s *Server) addClusterFirewallRule(w http.ResponseWriter, r *http.Request) {
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
	if err := client.AddClusterFirewallRule(r.Context(), rule); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.create", "firewall", req.Action+" "+req.Source+"->"+req.Dest)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteClusterFirewallRule(w http.ResponseWriter, r *http.Request) {
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
	if err := client.DeleteClusterFirewallRule(r.Context(), pos); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.delete", "firewall", strconv.Itoa(pos))
	w.WriteHeader(http.StatusNoContent)
}

type newAliasRequest struct {
	Name    string `json:"name"`
	CIDR    string `json:"cidr"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) addFirewallAlias(w http.ResponseWriter, r *http.Request) {
	var req newAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.CIDR == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name and cidr are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.AddFirewallAlias(r.Context(), req.Name, req.CIDR, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.alias.create", "firewall", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteFirewallAlias(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteFirewallAlias(r.Context(), name); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.alias.delete", "firewall", name)
	w.WriteHeader(http.StatusNoContent)
}

type newIPSetRequest struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) addFirewallIPSet(w http.ResponseWriter, r *http.Request) {
	var req newIPSetRequest
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
	if err := client.AddFirewallIPSet(r.Context(), req.Name, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.ipset.create", "firewall", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteFirewallIPSet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteFirewallIPSet(r.Context(), name); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.ipset.delete", "firewall", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) firewallIPSetEntries(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	entries, err := client.FirewallIPSetEntries(r.Context(), name)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

type newIPSetEntryRequest struct {
	CIDR    string `json:"cidr"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) addFirewallIPSetEntry(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req newIPSetEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.CIDR == "" {
		writeErrorMsg(w, http.StatusBadRequest, "cidr is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.AddFirewallIPSetEntry(r.Context(), name, req.CIDR, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.ipset.entry.create", "firewall", name+"/"+req.CIDR)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteFirewallIPSetEntry(w http.ResponseWriter, r *http.Request) {
	name, cidr := chi.URLParam(r, "name"), r.URL.Query().Get("cidr")
	if cidr == "" {
		writeErrorMsg(w, http.StatusBadRequest, "cidr query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteFirewallIPSetEntry(r.Context(), name, cidr); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.ipset.entry.delete", "firewall", name+"/"+cidr)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clusterFirewallAliases(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	aliases, err := client.ClusterFirewallAliases(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, aliases)
}

func (s *Server) clusterFirewallIPSets(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	sets, err := client.ClusterFirewallIPSets(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

// --- HA ---

func (s *Server) haResources(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	resources, err := client.HAResources(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func (s *Server) haGroups(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	groups, err := client.HAGroups(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) haStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.HAStatusCurrent(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type addHAResourceRequest struct {
	SID         string `json:"sid"`
	Group       string `json:"group,omitempty"`
	MaxRestart  int    `json:"maxRestart,omitempty"`
	MaxRelocate int    `json:"maxRelocate,omitempty"`
}

func (s *Server) addHAResource(w http.ResponseWriter, r *http.Request) {
	var req addHAResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.SID == "" {
		writeErrorMsg(w, http.StatusBadRequest, "sid is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.AddHAResource(r.Context(), req.SID, req.Group, req.MaxRestart, req.MaxRelocate); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.add-resource", "ha", req.SID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// updateHAResourceRequest uses pointers (unlike addHAResourceRequest) so an
// omitted field means "leave as-is" — 0 is a legitimate explicit value for
// MaxRestart/MaxRelocate ("don't restart/relocate"), so a plain int can't
// tell "not sent" from "sent as zero".
type updateHAResourceRequest struct {
	Group       *string `json:"group,omitempty"`
	MaxRestart  *int    `json:"maxRestart,omitempty"`
	MaxRelocate *int    `json:"maxRelocate,omitempty"`
	Comment     *string `json:"comment,omitempty"`
}

func (s *Server) updateHAResource(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	var req updateHAResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	group, maxRestart, maxRelocate, comment := "", -1, -1, ""
	if req.Group != nil {
		group = *req.Group
	}
	if req.MaxRestart != nil {
		maxRestart = *req.MaxRestart
	}
	if req.MaxRelocate != nil {
		maxRelocate = *req.MaxRelocate
	}
	if req.Comment != nil {
		comment = *req.Comment
	}
	if err := client.UpdateHAResource(r.Context(), sid, group, maxRestart, maxRelocate, comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.update-resource", "ha", sid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) removeHAResource(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.RemoveHAResource(r.Context(), sid); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "ha.remove-resource", "ha", sid)
	w.WriteHeader(http.StatusNoContent)
}

// --- Backups ---

func (s *Server) backupJobs(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	jobs, err := client.BackupJobs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

type runBackupRequest struct {
	Node    string   `json:"node"`
	Storage string   `json:"storage"`
	VMIDs   []string `json:"vmids,omitempty"`
	Mode    string   `json:"mode,omitempty"`
}

func (s *Server) runBackupNow(w http.ResponseWriter, r *http.Request) {
	var req runBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Node == "" || req.Storage == "" {
		writeErrorMsg(w, http.StatusBadRequest, "node and storage are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RunBackupNow(r.Context(), req.Node, req.Storage, req.VMIDs, req.Mode)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "backup.run", "backup", req.Node+"/"+req.Storage)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

type backupJobRequest struct {
	Schedule string `json:"schedule"`
	Storage  string `json:"storage"`
	VMIDs    string `json:"vmids,omitempty"` // comma-separated, empty = all guests
	Mode     string `json:"mode,omitempty"`
	Compress string `json:"compress,omitempty"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
	Prune    int    `json:"prune,omitempty"`

	NotificationMode   string `json:"notificationMode,omitempty"`
	MailTo             string `json:"mailTo,omitempty"`
	MailNotification   string `json:"mailNotification,omitempty"`
	BandwidthLimitKBps int    `json:"bandwidthLimitKBps,omitempty"`
	Pigz               *int   `json:"pigz,omitempty"`
}

func (req backupJobRequest) toOptions() pve.CreateBackupJobOptions {
	return pve.CreateBackupJobOptions{
		Schedule: req.Schedule, Storage: req.Storage, VMIDs: req.VMIDs,
		Mode: req.Mode, Compress: req.Compress, Enabled: req.Enabled,
		Comment: req.Comment, Prune: req.Prune,
		NotificationMode: req.NotificationMode, MailTo: req.MailTo, MailNotification: req.MailNotification,
		BandwidthLimitKBps: req.BandwidthLimitKBps, Pigz: req.Pigz,
	}
}

func (s *Server) createBackupJob(w http.ResponseWriter, r *http.Request) {
	var req backupJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Schedule == "" || req.Storage == "" {
		writeErrorMsg(w, http.StatusBadRequest, "schedule and storage are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateBackupJob(r.Context(), req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "backup-job.create", "backup", req.Storage)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) updateBackupJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	var req backupJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateBackupJob(r.Context(), jobID, req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "backup-job.update", "backup", jobID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteBackupJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteBackupJob(r.Context(), jobID); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "backup-job.delete", "backup", jobID)
	w.WriteHeader(http.StatusNoContent)
}

// --- Resource pools ---

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	pools, err := client.Pools(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) poolDetail(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	detail, err := client.PoolDetail(r.Context(), chi.URLParam(r, "poolId"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PoolID  string `json:"poolId"`
		Comment string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PoolID == "" {
		writeErrorMsg(w, http.StatusBadRequest, "poolId is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreatePool(r.Context(), req.PoolID, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pool.create", "pool", req.PoolID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deletePool(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolId")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeletePool(r.Context(), poolID); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pool.delete", "pool", poolID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setPoolMembers(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolId")
	var req struct {
		VMIDs  []int `json:"vmids"`
		Remove bool  `json:"remove"`
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
	if err := client.SetPoolMembers(r.Context(), poolID, req.VMIDs, req.Remove); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pool.members.update", "pool", poolID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Datacenter options & subscription ---

func (s *Server) datacenterOptions(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	opts, err := client.DatacenterOptions(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

type updateDatacenterOptionsRequest struct {
	pve.DatacenterOptions
	Extra map[string]string `json:"extra,omitempty"`
}

func (s *Server) updateDatacenterOptions(w http.ResponseWriter, r *http.Request) {
	var req updateDatacenterOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateDatacenterOptions(r.Context(), req.DatacenterOptions, req.Extra); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "datacenter.options.update", "datacenter", chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) nodeSubscription(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	sub, err := client.NodeSubscription(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// --- Cluster firewall options (the master enable switch) ---

func (s *Server) clusterFirewallOptions(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	opts, err := client.ClusterFirewallOptions(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) updateClusterFirewallOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enable bool `json:"enable"`
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
	if err := client.UpdateClusterFirewallOptions(r.Context(), req.Enable); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.options.update", "firewall", fmt.Sprintf("enable=%v", req.Enable))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Replication ---

func (s *Server) replicationJobs(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	jobs, err := client.ReplicationJobs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) scheduleReplicationNow(w http.ResponseWriter, r *http.Request) {
	node, id := chi.URLParam(r, "node"), chi.URLParam(r, "repId")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.ScheduleReplicationNow(r.Context(), node, id)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "replication.run", "node", node+"/"+id)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

type replicationJobRequest struct {
	ID       string `json:"id,omitempty"` // required on create only; ignored on update (the path id wins)
	Guest    int    `json:"guest,omitempty"`
	Target   string `json:"target"`
	Schedule string `json:"schedule,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Disable  bool   `json:"disable,omitempty"`
}

func (req replicationJobRequest) toOptions() pve.CreateReplicationJobOptions {
	return pve.CreateReplicationJobOptions{
		ID: req.ID, Guest: req.Guest, Target: req.Target,
		Schedule: req.Schedule, Comment: req.Comment, Disable: req.Disable,
	}
}

func (s *Server) createReplicationJob(w http.ResponseWriter, r *http.Request) {
	var req replicationJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ID == "" || req.Guest == 0 || req.Target == "" {
		writeErrorMsg(w, http.StatusBadRequest, "id, guest, and target are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateReplicationJob(r.Context(), req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "replication-job.create", "replication", req.ID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) updateReplicationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "repId")
	var req replicationJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateReplicationJob(r.Context(), jobID, req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "replication-job.update", "replication", jobID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteReplicationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "repId")
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteReplicationJob(r.Context(), jobID, force); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "replication-job.delete", "replication", jobID)
	w.WriteHeader(http.StatusNoContent)
}

// --- Firewall security groups ---

func (s *Server) firewallSecurityGroups(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	groups, err := client.FirewallSecurityGroups(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

type securityGroupRequest struct {
	Group   string `json:"group"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) createFirewallSecurityGroup(w http.ResponseWriter, r *http.Request) {
	var req securityGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Group == "" {
		writeErrorMsg(w, http.StatusBadRequest, "group is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateFirewallSecurityGroup(r.Context(), req.Group, req.Comment); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.create-group", "firewall", req.Group)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteFirewallSecurityGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteFirewallSecurityGroup(r.Context(), name); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.delete-group", "firewall", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) securityGroupRules(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rules, err := client.SecurityGroupRules(r.Context(), name)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) addSecurityGroupRule(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
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
	if err := client.AddSecurityGroupRule(r.Context(), name, rule); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.add-group-rule", "firewall", name)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteSecurityGroupRule(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	pos, err := strconv.Atoi(chi.URLParam(r, "pos"))
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "pos must be an integer")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteSecurityGroupRule(r.Context(), name, pos); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.delete-group-rule", "firewall", name)
	w.WriteHeader(http.StatusNoContent)
}

// --- Storage configuration (add/edit/remove a storage backend) ---

type storageConfigRequest struct {
	Storage string            `json:"storage"`
	Type    string            `json:"type"`
	Content string            `json:"content,omitempty"`
	Nodes   string            `json:"nodes,omitempty"`
	Shared  bool              `json:"shared,omitempty"`
	Disable bool              `json:"disable,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
}

func (req storageConfigRequest) toOptions() pve.CreateStorageOptions {
	return pve.CreateStorageOptions{
		Storage: req.Storage, Type: req.Type, Content: req.Content,
		Nodes: req.Nodes, Shared: req.Shared, Disable: req.Disable, Extra: req.Extra,
	}
}

func (s *Server) createStorage(w http.ResponseWriter, r *http.Request) {
	var req storageConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Storage == "" || req.Type == "" {
		writeErrorMsg(w, http.StatusBadRequest, "storage and type are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateStorage(r.Context(), req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.create", "storage", req.Storage)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) updateStorage(w http.ResponseWriter, r *http.Request) {
	storage := chi.URLParam(r, "storage")
	var req storageConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UpdateStorage(r.Context(), storage, req.toOptions()); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.update", "storage", storage)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteStorage(w http.ResponseWriter, r *http.Request) {
	storage := chi.URLParam(r, "storage")
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteStorage(r.Context(), storage); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.delete", "storage", storage)
	w.WriteHeader(http.StatusNoContent)
}
