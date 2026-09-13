package api

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Tags/Notes are pointers so a field absent from the body (nil — leave
	// untouched) is distinguishable from one explicitly sent empty (clear
	// it): PVE has no "set to empty" for config keys, clearing means
	// delete=<key>.
	Tags  *string `json:"tags,omitempty"`
	Notes *string `json:"notes,omitempty"`
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
	// PVE clears a config key via the comma-separated "delete" list, not an
	// empty value — so an explicitly-empty tags/notes becomes a delete.
	var deletes []string
	if req.Tags != nil {
		if *req.Tags == "" {
			deletes = append(deletes, "tags")
		} else {
			form.Set("tags", *req.Tags)
		}
	}
	if req.Notes != nil {
		if *req.Notes == "" {
			deletes = append(deletes, "description")
		} else {
			form.Set("description", *req.Notes)
		}
	}
	if len(deletes) > 0 {
		form.Set("delete", strings.Join(deletes, ","))
	}
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
	Format      string `json:"format,omitempty"`
	Pool        string `json:"pool,omitempty"`
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
		Full: req.Full, Storage: req.Storage, Format: req.Format, Pool: req.Pool, Description: req.Description,
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
	TargetNode     string `json:"targetNode"`
	Online         bool   `json:"online"`
	WithLocalDisks bool   `json:"withLocalDisks"`
	TargetStorage  string `json:"targetStorage,omitempty"`
	Bwlimit        int    `json:"bwlimit,omitempty"`
	Restart        bool   `json:"restart,omitempty"` // lxc restart-migration for a running container
	TimeoutSecs    int    `json:"timeoutSecs,omitempty"`
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
	upid, err := client.MigrateGuest(r.Context(), guestType, node, vmid, pve.MigrateOptions{
		TargetNode:     req.TargetNode,
		Online:         req.Online,
		WithLocalDisks: req.WithLocalDisks,
		TargetStorage:  req.TargetStorage,
		Bwlimit:        req.Bwlimit,
		Restart:        req.Restart,
		TimeoutSecs:    req.TimeoutSecs,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.migrate", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Info("guest migration started", "connectionId", connID, "vmid", vmid, "target", req.TargetNode, "online", req.Online)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// remoteMigrateGuestRequest configures a cross-cluster (remote-to-remote)
// live migration — PVE's native remote_migrate, addressed at a *different*
// stored connection than the one the guest currently lives on.
type remoteMigrateGuestRequest struct {
	TargetConnID  string `json:"targetConnId"`
	TargetNode    string `json:"targetNode"`
	TargetVMID    int    `json:"targetVmid,omitempty"`
	TargetStorage string `json:"targetStorage"`
	TargetBridge  string `json:"targetBridge,omitempty"`
	Online        bool   `json:"online"`
	DeleteSource  bool   `json:"deleteSource,omitempty"`
}

// remoteMigrateGuest starts a qemu VM's live migration to a node on a
// *different* PVE connection (a different cluster/standalone host), via
// PVE 8.x's native remote_migrate API — the cross-cluster counterpart to
// migrateGuest above, which only moves a guest within its own cluster.
//
// qemu only: PVE has no remote_migrate endpoint for lxc at the time of
// writing (see pve.ErrLXCRemoteMigrateUnsupported) — that's a hard
// unsupported-operation error, not an upstream call left to fail
// unpredictably.
//
// The target connection's decrypted API token is read server-side only
// (via s.connections.TargetCredentials) to build PVE's target-endpoint
// connection string; it is never echoed back in any response.
func (s *Server) remoteMigrateGuest(w http.ResponseWriter, r *http.Request) {
	srcConnID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if guestType != "qemu" {
		writeErrorMsg(w, http.StatusBadRequest, pve.ErrLXCRemoteMigrateUnsupported.Error())
		return
	}

	var req remoteMigrateGuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.TargetConnID == "" || req.TargetNode == "" || req.TargetStorage == "" {
		writeErrorMsg(w, http.StatusBadRequest, "targetConnId, targetNode, and targetStorage are required")
		return
	}
	if req.TargetConnID == srcConnID {
		writeErrorMsg(w, http.StatusBadRequest, "targetConnId must be a different connection — use /migrate for a same-cluster move")
		return
	}

	srcType, err := s.connectionType(r.Context(), srcConnID)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "source connection not found")
		return
	}
	targetType, err := s.connectionType(r.Context(), req.TargetConnID)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "target connection not found")
		return
	}
	if srcType != "pve" || targetType != "pve" {
		writeErrorMsg(w, http.StatusBadRequest, "both connections must be type=pve for remote migration")
		return
	}

	srcClient, err := s.clientFor(r.Context(), srcConnID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	targetEndpoint, err := s.buildRemoteMigrateTargetEndpoint(r.Context(), req.TargetConnID, req.TargetNode)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	upid, err := srcClient.RemoteMigrateGuest(r.Context(), node, vmid, pve.RemoteMigrateOptions{
		TargetEndpoint: targetEndpoint,
		TargetNode:     req.TargetNode,
		TargetVMID:     req.TargetVMID,
		TargetStorage:  req.TargetStorage,
		TargetBridge:   req.TargetBridge,
		Online:         req.Online,
		DeleteSource:   req.DeleteSource,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.remote_migrate", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Info("guest remote migration started", "sourceConnectionId", srcConnID, "targetConnectionId", req.TargetConnID,
		"vmid", vmid, "targetNode", req.TargetNode, "online", req.Online)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// buildRemoteMigrateTargetEndpoint assembles PVE's "proxmox-remote"
// connection string for the target cluster: its host/port/API token (read
// server-side from the stored connection, never exposed to the frontend)
// plus the target node's pveproxy TLS fingerprint, which PVE's
// remote_migrate call uses to pin the connection instead of trusting a CA.
//
// remote_migrate performs the migration by talking to the target node's own
// API directly, so host must resolve to *that* node specifically — not just
// any reachable member of the target cluster. When the stored connection's
// host isn't already the target node (e.g. it points at a different member
// or a load balancer), the target node's own IP is looked up via its
// cluster status and used instead.
func (s *Server) buildRemoteMigrateTargetEndpoint(ctx context.Context, targetConnID, targetNode string) (string, error) {
	host, port, tokenID, tokenSecret, err := s.connections.TargetCredentials(ctx, targetConnID)
	if err != nil {
		return "", fmt.Errorf("target connection credentials: %w", err)
	}

	targetClient, err := s.clientFor(ctx, targetConnID)
	if err != nil {
		return "", err
	}
	if nodeHost, err := nodeIPFromClusterStatus(ctx, targetClient, targetNode); err == nil && nodeHost != "" {
		host = nodeHost
	}

	fingerprint, err := s.targetNodeFingerprint(ctx, targetConnID, targetNode)
	if err != nil {
		return "", fmt.Errorf("target node fingerprint: %w", err)
	}

	return fmt.Sprintf("apitoken=PVEAPIToken=%s=%s,host=%s,port=%d,fingerprint=%s",
		tokenID, tokenSecret, host, port, fingerprint), nil
}

// nodeIPFromClusterStatus looks up a specific node's IP from the target
// cluster's own /cluster/status — used so a stored connection pointed at
// one cluster member can still remote-migrate to any other member by name.
// Returns "" (not an error) when the node isn't found, e.g. a standalone
// (non-clustered) target where the connection's host is already correct.
func nodeIPFromClusterStatus(ctx context.Context, client *pve.Client, node string) (string, error) {
	status, err := client.ClusterStatus(ctx)
	if err != nil {
		return "", err
	}
	for _, entry := range status {
		if entry.Type == "node" && entry.Name == node && entry.IP != "" {
			return entry.IP, nil
		}
	}
	return "", nil
}

// targetNodeFingerprint fetches the target node's pveproxy certificate
// fingerprint through the already-authenticated target client (trust is
// bootstrapped the same way PVE's own cluster-join flow does it: connect
// with the connection's configured verify-TLS setting, then hand the
// fingerprint we observe to the source cluster to pin future connections).
func (s *Server) targetNodeFingerprint(ctx context.Context, targetConnID, targetNode string) (string, error) {
	targetClient, err := s.clientFor(ctx, targetConnID)
	if err != nil {
		return "", err
	}
	certs, err := targetClient.NodeCertificates(ctx, targetNode)
	if err != nil {
		return "", err
	}
	for _, cert := range certs {
		if cert.Filename == "pveproxy.pem" && cert.Fingerprint != "" {
			return cert.Fingerprint, nil
		}
	}
	for _, cert := range certs {
		if cert.Fingerprint != "" {
			return cert.Fingerprint, nil
		}
	}
	return "", fmt.Errorf("no TLS certificate fingerprint found for node %s", targetNode)
}

// migratePrecondition surfaces PVE's pre-migration check (local disks,
// incompatible target nodes) so the UI can warn before firing a migration
// that PVE would otherwise reject outright. qemu only — PVE doesn't expose
// this check for lxc.
func (s *Server) migratePrecondition(w http.ResponseWriter, r *http.Request) {
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
	result, err := client.MigratePrecondition(r.Context(), node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
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
	// purge defaults to true (also remove the guest from backup/replication/HA
	// jobs) — the common case for "delete this guest" — but callers that want
	// to keep those job references (e.g. re-provisioning at the same vmid)
	// can opt out with ?purge=false.
	purge := r.URL.Query().Get("purge") != "false"
	upid, err := client.DeleteGuest(r.Context(), guestType, node, vmid, purge)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.delete", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	slog.Warn("guest deleted", "connectionId", connID, "vmid", vmid)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// guestAgentNetwork surfaces live IP/MAC info for the guest — for a QEMU VM
// via the in-guest qemu-guest-agent (requires it installed, running, and
// enabled in the VM's Options); for an LXC container, straight from its
// network namespace, which the host already has direct visibility into, so
// no agent is needed there at all. Same response shape either way.
func (s *Server) guestAgentNetwork(w http.ResponseWriter, r *http.Request) {
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

	var interfaces []pve.AgentNetworkInterface
	switch guestType {
	case "qemu":
		interfaces, err = client.GuestAgentNetworkInterfaces(r.Context(), node, vmid)
		if err != nil {
			writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
			return
		}
	case "lxc":
		interfaces, err = client.LXCInterfaces(r.Context(), node, vmid)
		if err != nil {
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
	default:
		writeErrorMsg(w, http.StatusBadRequest, "unknown guest type")
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
	upid, err := client.ResizeDisk(r.Context(), guestType, node, vmid, req.Disk, req.Size)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.resize", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	// upid is "" for storage backends that resize synchronously — the
	// frontend only polls task status when it gets a non-empty one back.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "upid": upid})
}

func (s *Server) moveGuestDisk(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Disk    string `json:"disk"`
		Storage string `json:"storage"`
		Format  string `json:"format,omitempty"`
		Delete  bool   `json:"delete,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Disk == "" || req.Storage == "" {
		writeErrorMsg(w, http.StatusBadRequest, "disk and storage are required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.MoveDisk(r.Context(), guestType, node, vmid, req.Disk, req.Storage, req.Format, req.Delete)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.disk.move", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// --- QEMU guest agent (exec, fsfreeze, shutdown, set-password) ---

// guestSendKey sends a key combination (e.g. "ctrl-alt-delete") to a running
// QEMU guest's virtual display — the console toolbar action for keys the
// browser would otherwise swallow itself. QEMU only: LXC has no virtual
// keyboard to inject into.
func (s *Server) guestSendKey(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if guestType != "qemu" {
		writeErrorMsg(w, http.StatusBadRequest, "sending keys is only supported for QEMU VMs")
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		writeErrorMsg(w, http.StatusBadRequest, "key is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.SendKey(r.Context(), node, vmid, req.Key); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "guest.sendkey", "guest", strconv.Itoa(vmid)+" ("+req.Key+")")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guestAgentPing(w http.ResponseWriter, r *http.Request) {
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
	if err := client.GuestAgentPing(r.Context(), node, vmid); err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guestAgentExec(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Command []string `json:"command"`
		Input   string   `json:"input,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Command) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "command is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	result, err := client.GuestAgentExec(r.Context(), node, vmid, req.Command, req.Input)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.exec", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) guestAgentExecStatus(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "pid query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.GuestAgentExecStatus(r.Context(), node, vmid, pid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) guestAgentFsfreeze(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	thaw := chi.URLParam(r, "action") == "thaw"
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.GuestAgentFsfreeze(r.Context(), node, vmid, thaw); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.fsfreeze", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guestAgentShutdown(w http.ResponseWriter, r *http.Request) {
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
	if err := client.GuestAgentShutdown(r.Context(), node, vmid); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.shutdown", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guestAgentSetPassword(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Crypted  bool   `json:"crypted,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Username == "" || req.Password == "" {
		writeErrorMsg(w, http.StatusBadRequest, "username and password are required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.GuestAgentSetUserPassword(r.Context(), node, vmid, req.Username, req.Password, req.Crypted); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.setpassword", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guestAgentOSInfo(w http.ResponseWriter, r *http.Request) {
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
	info, err := client.GuestAgentOSInfo(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) guestAgentFSInfo(w http.ResponseWriter, r *http.Request) {
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
	info, err := client.GuestAgentFSInfo(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) guestAgentVCPUs(w http.ResponseWriter, r *http.Request) {
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
	vcpus, err := client.GuestAgentVCPUs(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, vcpus)
}

func (s *Server) guestAgentHostname(w http.ResponseWriter, r *http.Request) {
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
	hostname, err := client.GuestAgentHostname(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"hostname": hostname})
}

func (s *Server) guestAgentTimezone(w http.ResponseWriter, r *http.Request) {
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
	zone, err := client.GuestAgentTimezone(r.Context(), node, vmid)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "guest agent unavailable — install qemu-guest-agent and enable it in this VM's Options")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"zone": zone})
}

// guestAgentFileRead and guestAgentFileWrite reach into the guest's
// filesystem via the agent — sensitive like exec/shutdown/set-password, so
// both are POST (gated by requireAdminForMutations on this route group)
// rather than a plain GET, even for the read side.
func (s *Server) guestAgentFileRead(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Path == "" {
		writeErrorMsg(w, http.StatusBadRequest, "path is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	content, err := client.GuestAgentFileRead(r.Context(), node, vmid, req.Path)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.file-read", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) guestAgentFileWrite(w http.ResponseWriter, r *http.Request) {
	connID, node := chi.URLParam(r, "id"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Path == "" {
		writeErrorMsg(w, http.StatusBadRequest, "path is required")
		return
	}
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.GuestAgentFileWrite(r.Context(), node, vmid, req.Path, req.Content); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.agent.file-write", "vm", node+"/qemu/"+chi.URLParam(r, "vmid"))
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

// guestFirewallOptions and updateGuestFirewallOptions expose the guest's own
// firewall master switch — separate from the cluster-wide one. Rules added
// via addGuestFirewallRule are inert until this is on.
func (s *Server) guestFirewallOptions(w http.ResponseWriter, r *http.Request) {
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
	opts, err := client.GuestFirewallOptions(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) updateGuestFirewallOptions(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Enable bool `json:"enable"`
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
	if err := client.UpdateGuestFirewallOptions(r.Context(), guestType, node, vmid, req.Enable); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "firewall.options.update", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// guestBackupsResponse wraps the flat backup list the endpoint used to
// return with the per-storage failures hit while collecting it — additive
// shape: previously a bare array, so consumers need the new object form.
type guestBackupsResponse struct {
	Backups []pve.StorageContentItem `json:"backups"`
	// Warnings lists one entry per backup-capable storage that couldn't be
	// listed; empty (omitted) means every storage answered.
	Warnings []string `json:"warnings,omitempty"`
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
	var warnings []string // per-storage failures, surfaced instead of silently dropped
	for _, st := range storages {
		if !strings.Contains(st.Content, "backup") {
			continue
		}
		items, err := client.GuestBackups(r.Context(), node, st.Storage, vmid)
		if err != nil {
			// One unreachable/misconfigured storage shouldn't blank the
			// whole list — but the failure is part of the answer, or a
			// "no backups found" would hide a storage that couldn't even
			// be asked.
			warnings = append(warnings, st.Storage+": "+err.Error())
			continue
		}
		all = append(all, items...)
	}
	writeJSON(w, http.StatusOK, guestBackupsResponse{Backups: all, Warnings: warnings})
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

	// Archive restores from a vzdump backup volume instead of creating an
	// empty VM — cores/memoryMb become optional overrides (PVE fills them
	// in from the backup's saved config when omitted).
	Archive string `json:"archive,omitempty"`
	Force   bool   `json:"force,omitempty"`

	// Extra holds additional disks/NICs/hardware as raw PVE config keys
	// (e.g. {"scsi1": "local-lvm:32", "net1": "virtio,bridge=vmbr1"}).
	Extra map[string]string `json:"extra,omitempty"`
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	var req createVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Node == "" || (req.Archive == "" && (req.Cores <= 0 || req.MemoryMB <= 0)) {
		writeErrorMsg(w, http.StatusBadRequest, "node, cores, and memoryMb are required")
		return
	}
	// The cloud-init drive (ide3) is created on the same storage as the VM
	// disk — without a storage there is nothing to place it on and PVE
	// would reject the create outright.
	if req.CIUser != "" && req.Storage == "" {
		writeErrorMsg(w, http.StatusBadRequest, "ciUser requires storage: set storage to the same target as the VM disk so the cloud-init drive can be created on it")
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
		Archive: req.Archive, Force: req.Force, Extra: req.Extra,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	// The create routes carry no {vmid} URL segment — the target is the
	// (possibly auto-assigned) id the VM was actually created with.
	s.audit(r, "vm.create", "vm", req.Node+"/qemu/"+strconv.Itoa(req.VMID))
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

	// Archive restores from a vzdump backup volume instead of unpacking
	// Template — Template is not required when Archive is set.
	Archive string `json:"archive,omitempty"`
	Force   bool   `json:"force,omitempty"`

	// Extra holds additional mount points/NICs/hardware as raw PVE config
	// keys (e.g. {"mp0": "local-lvm:8,mp=/data", "net1": "name=eth1,bridge=vmbr1"}).
	Extra map[string]string `json:"extra,omitempty"`
}

func (s *Server) createLXC(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	var req createLXCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Node == "" || req.Cores <= 0 || req.MemoryMB <= 0 || (req.Archive == "" && req.Template == "") {
		writeErrorMsg(w, http.StatusBadRequest, "node, cores, memoryMb, and template (or archive) are required")
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
		Archive: req.Archive, Force: req.Force, Extra: req.Extra,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "lxc.create", "vm", req.Node+"/lxc/"+strconv.Itoa(req.VMID))
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
