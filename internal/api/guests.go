package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

func (s *Server) getGuestConfig(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	cfg, err := client.GuestConfig(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

type updateGuestConfigRequest struct {
	Name    string `json:"name,omitempty"`
	Cores   int    `json:"cores,omitempty"`
	Sockets int    `json:"sockets,omitempty"`
	Memory  int    `json:"memory,omitempty"`
	Boot    string `json:"boot,omitempty"`
	Tags    string `json:"tags,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

func (s *Server) updateGuestConfig(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req updateGuestConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	form := url.Values{}
	setIfNonEmpty(form, "name", req.Name)
	setIfPositive(form, "cores", req.Cores)
	setIfPositive(form, "sockets", req.Sockets)
	setIfPositive(form, "memory", req.Memory)
	setIfNonEmpty(form, "boot", req.Boot)
	setIfNonEmpty(form, "tags", req.Tags)
	setIfNonEmpty(form, "description", req.Notes)
	if len(form) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "no fields to update")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.UpdateGuestConfig(r.Context(), guestType, node, vmid, form)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.reconfigure", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

type cloneGuestRequest struct {
	NewID       int    `json:"newId"`
	Name        string `json:"name,omitempty"`
	TargetNode  string `json:"targetNode,omitempty"`
	Full        bool   `json:"full"`
	Storage     string `json:"storage,omitempty"`
	Description string `json:"description,omitempty"`
}

func (s *Server) cloneGuest(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req cloneGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.NewID == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "newId is required")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.CloneGuest(r.Context(), guestType, node, vmid, pve.CloneOptions{
		NewID: req.NewID, Name: req.Name, TargetNode: req.TargetNode,
		Full: req.Full, Storage: req.Storage, Description: req.Description,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.clone", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Info("guest clone started", "connectionId", connID, "sourceVmid", vmid, "newId", req.NewID)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

type migrateGuestRequest struct {
	TargetNode string `json:"targetNode"`
	Online     bool   `json:"online"`
}

func (s *Server) migrateGuest(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req migrateGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.TargetNode == "" {
		writeErrorMsg(w, http.StatusBadRequest, "targetNode is required")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.MigrateGuest(r.Context(), guestType, node, vmid, req.TargetNode, req.Online)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.migrate", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Info("guest migration started", "connectionId", connID, "vmid", vmid, "target", req.TargetNode, "online", req.Online)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) deleteGuest(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.DeleteGuest(r.Context(), guestType, node, vmid, true)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.delete", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Warn("guest deleted", "connectionId", connID, "vmid", vmid)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// guestAgentNetwork surfaces live IP/MAC info from the QEMU guest agent —
// only meaningful for "qemu" guests with the agent installed and running.
func (s *Server) guestAgentNetwork(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if guestType != "qemu" {
		writeErrorMsg(w, http.StatusBadRequest, "the guest agent is a QEMU-only feature")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	interfaces, err := client.GuestAgentNetworkInterfaces(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, interfaces)
}

func (s *Server) resizeGuestDisk(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Disk string `json:"disk"`
		Size string `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.ResizeDisk(r.Context(), guestType, node, vmid, req.Disk, req.Size); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.resize", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) setGuestTemplate(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.SetTemplate(r.Context(), guestType, node, vmid); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.convert-template", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) unlockGuest(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.UnlockGuest(r.Context(), guestType, node, vmid); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.unlock", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Snapshots ---

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	snaps, err := client.ListSnapshots(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) createSnapshot(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description,omitempty"`
		IncludeState bool   `json:"includeState"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.CreateSnapshot(r.Context(), guestType, node, vmid, req.Name, req.Description, req.IncludeState)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.snapshot.create", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) rollbackSnapshot(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	name := chi.URLParam(r, "snapname")
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RollbackSnapshot(r.Context(), guestType, node, vmid, name)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.snapshot.rollback", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	name := chi.URLParam(r, "snapname")
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.DeleteSnapshot(r.Context(), guestType, node, vmid, name)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.snapshot.delete", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) guestFirewallRules(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rules, err := client.GuestFirewallRules(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) addGuestFirewallRule(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req newFirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Type == "" || req.Action == "" {
		writeErrorMsg(w, http.StatusBadRequest, "type and action are required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	rule := pve.NewFirewallRule{
		Type: req.Type, Action: req.Action, Source: req.Source, Dest: req.Dest,
		Proto: req.Proto, Dport: req.Dport, Sport: req.Sport, Macro: req.Macro,
		Comment: req.Comment, Enable: req.Enable,
	}
	if err := client.AddGuestFirewallRule(r.Context(), guestType, node, vmid, rule); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.create", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteGuestFirewallRule(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	pos, err := strconv.Atoi(chi.URLParam(r, "pos"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.DeleteGuestFirewallRule(r.Context(), guestType, node, vmid, pos); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.rule.delete", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	w.WriteHeader(http.StatusNoContent)
}

// guestBackups aggregates backup archives for one guest across every
// storage on its node that's configured to hold backups — the caller
// shouldn't need to already know which storage a backup landed on.
func (s *Server) guestBackups(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	storages, err := client.NodeStorage(r.Context(), node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	var all []pve.StorageContentItem
	for _, st := range storages {
		if !strings.Contains(st.Content, "backup") {
			continue
		}
		items, err := client.GuestBackups(r.Context(), node, st.Storage, vmid)
		if err != nil {
			continue // one unreachable/misconfigured storage shouldn't blank the whole list
		}
		all = append(all, items...)
	}
	writeJSON(w, http.StatusOK, all)
}

// --- Create VM / LXC ---

type createVMRequest struct {
	Node         string `json:"node"`
	VMID         int    `json:"vmid"`
	Name         string `json:"name"`
	Cores        int    `json:"cores"`
	MemoryMB     int    `json:"memoryMb"`
	Storage      string `json:"storage"`
	DiskGB       int    `json:"diskGb"`
	ISO          string `json:"iso,omitempty"`
	Bridge       string `json:"bridge,omitempty"`
	CIUser       string `json:"ciUser,omitempty"`
	CIPassword   string `json:"ciPassword,omitempty"`
	SSHPublicKey string `json:"sshPublicKey,omitempty"`
	IPConfig     string `json:"ipConfig,omitempty"`
	Nameserver   string `json:"nameserver,omitempty"`
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	var req createVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Node == "" || req.Cores <= 0 || req.MemoryMB <= 0 {
		writeErrorMsg(w, http.StatusBadRequest, "node, cores, and memoryMb are required")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if req.VMID == 0 {
		req.VMID, err = client.NextID(r.Context())
		if err != nil {
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	upid, err := client.CreateVM(r.Context(), pve.CreateVMOptions{
		VMID: req.VMID, Name: req.Name, Node: req.Node, Cores: req.Cores, Memory: req.MemoryMB,
		Storage: req.Storage, DiskGB: req.DiskGB, ISO: req.ISO, Bridge: req.Bridge,
		CIUser: req.CIUser, CIPassword: req.CIPassword, SSHPublicKey: req.SSHPublicKey,
		IPConfig: req.IPConfig, Nameserver: req.Nameserver,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.create", "vm", req.Node+"/qemu/"+chi.URLParam(r, "id"))
	slog.Info("VM created", "connectionId", connID, "node", req.Node, "vmid", req.VMID, "name", req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"upid": upid, "vmid": req.VMID})
}

type createLXCRequest struct {
	Node         string `json:"node"`
	VMID         int    `json:"vmid"`
	Hostname     string `json:"hostname"`
	Cores        int    `json:"cores"`
	MemoryMB     int    `json:"memoryMb"`
	Storage      string `json:"storage"`
	DiskGB       int    `json:"diskGb"`
	Template     string `json:"template"`
	Bridge       string `json:"bridge,omitempty"`
	Password     string `json:"password,omitempty"`
	SSHPublicKey string `json:"sshPublicKey,omitempty"`
	IPConfig     string `json:"ipConfig,omitempty"`
	Unprivileged bool   `json:"unprivileged"`
}

func (s *Server) createLXC(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	var req createLXCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Node == "" || req.Template == "" || req.Cores <= 0 || req.MemoryMB <= 0 {
		writeErrorMsg(w, http.StatusBadRequest, "node, template, cores, and memoryMb are required")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if req.VMID == 0 {
		req.VMID, err = client.NextID(r.Context())
		if err != nil {
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	upid, err := client.CreateLXC(r.Context(), pve.CreateLXCOptions{
		VMID: req.VMID, Hostname: req.Hostname, Node: req.Node, Cores: req.Cores, Memory: req.MemoryMB,
		Storage: req.Storage, DiskGB: req.DiskGB, Template: req.Template, Bridge: req.Bridge,
		Password: req.Password, SSHPublicKey: req.SSHPublicKey, IPConfig: req.IPConfig, Unprivileged: req.Unprivileged,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "lxc.create", "vm", req.Node+"/lxc/"+chi.URLParam(r, "id"))
	slog.Info("LXC created", "connectionId", connID, "node", req.Node, "vmid", req.VMID, "hostname", req.Hostname)
	writeJSON(w, http.StatusCreated, map[string]any{"upid": upid, "vmid": req.VMID})
}

func (s *Server) nextGuestID(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	id, err := client.NextID(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"vmid": id})
}

func setIfNonEmpty(form url.Values, key, value string) {
	if value != "" {
		form.Set(key, value)
	}
}

func setIfPositive(form url.Values, key string, value int) {
	if value > 0 {
		form.Set(key, strconv.Itoa(value))
	}
}
