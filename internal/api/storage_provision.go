package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Provisioning raw disks into usable storage (dir/LVM/LVM-thin/ZFS) and
// discovering NFS/CIFS/iSCSI/LVM/ZFS/GlusterFS resources before registering
// them — the two gaps left by disks.go (view-only) and storage.go (register
// an already-known backend). Every mutating route here is destructive
// (wipes a disk, creates a pool) and relies on requireAdminForMutations,
// already mounted on the /connections group in server.go.

// --- Disk provisioning ---

func (s *Server) nodeZFSPools(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	pools, err := client.NodeZFSPools(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) nodeLVMGroups(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	groups, err := client.NodeLVMVolumeGroups(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) nodeLVMThinPools(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	pools, err := client.NodeLVMThinPools(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) wipeDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk string `json:"disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Disk == "" {
		writeErrorMsg(w, http.StatusBadRequest, "disk is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	if err := client.WipeDisk(r.Context(), node, req.Disk); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.wipe", "disk", node+":"+req.Disk)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) initGPT(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk string `json:"disk"`
		UUID string `json:"uuid,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Disk == "" {
		writeErrorMsg(w, http.StatusBadRequest, "disk is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	upid, err := client.InitGPT(r.Context(), node, req.Disk, req.UUID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.initgpt", "disk", node+":"+req.Disk)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

type createDiskStorageRequest struct {
	Device     string `json:"device"`
	Name       string `json:"name"`
	AddStorage bool   `json:"addStorage"`
}

func (s *Server) createDirectoryStorage(w http.ResponseWriter, r *http.Request) {
	var req createDiskStorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Device == "" || req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "device and name are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	upid, err := client.CreateDirectoryStorage(r.Context(), node, req.Device, req.Name, req.AddStorage)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.create.directory", "disk", node+":"+req.Device)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) createLVMStorage(w http.ResponseWriter, r *http.Request) {
	var req createDiskStorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Device == "" || req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "device and name are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	upid, err := client.CreateLVMStorage(r.Context(), node, req.Device, req.Name, req.AddStorage)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.create.lvm", "disk", node+":"+req.Device)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) createLVMThinStorage(w http.ResponseWriter, r *http.Request) {
	var req createDiskStorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Device == "" || req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "device and name are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	upid, err := client.CreateLVMThinStorage(r.Context(), node, req.Device, req.Name, req.AddStorage)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.create.lvmthin", "disk", node+":"+req.Device)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) createZFSPool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string   `json:"name"`
		Devices    []string `json:"devices"`
		RaidLevel  string   `json:"raidLevel,omitempty"`
		Ashift     int      `json:"ashift,omitempty"`
		AddStorage bool     `json:"addStorage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || len(req.Devices) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "name and at least one device are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")
	upid, err := client.CreateZFSPool(r.Context(), node, req.Name, req.Devices, req.RaidLevel, req.Ashift, req.AddStorage)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "storage.disk.create.zfs", "disk", node+":"+req.Name)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

// --- Storage discovery/scan ---

func (s *Server) scanNFS(w http.ResponseWriter, r *http.Request) {
	server := r.URL.Query().Get("server")
	if server == "" {
		writeErrorMsg(w, http.StatusBadRequest, "server query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	exports, err := client.ScanNFS(r.Context(), chi.URLParam(r, "node"), server)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, exports)
}

func (s *Server) scanCIFS(w http.ResponseWriter, r *http.Request) {
	// Credentials travel in a POST body rather than the query string used by
	// the other scan endpoints, since query strings are far more likely to
	// end up in access/proxy logs or browser history.
	var req struct {
		Server   string `json:"server"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
		Domain   string `json:"domain,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Server == "" {
		writeErrorMsg(w, http.StatusBadRequest, "server is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	shares, err := client.ScanCIFS(r.Context(), chi.URLParam(r, "node"), req.Server, req.Username, req.Password, req.Domain)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) scanISCSI(w http.ResponseWriter, r *http.Request) {
	portal := r.URL.Query().Get("portal")
	if portal == "" {
		writeErrorMsg(w, http.StatusBadRequest, "portal query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	targets, err := client.ScanISCSI(r.Context(), chi.URLParam(r, "node"), portal)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) scanLVM(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	groups, err := client.ScanLVM(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) scanZFS(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	pools, err := client.ScanZFS(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) scanGlusterFS(w http.ResponseWriter, r *http.Request) {
	server := r.URL.Query().Get("server")
	if server == "" {
		writeErrorMsg(w, http.StatusBadRequest, "server query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	volumes, err := client.ScanGlusterFS(r.Context(), chi.URLParam(r, "node"), server)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, volumes)
}
