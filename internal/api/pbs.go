// PBS (Proxmox Backup Server) remote endpoints — mounted under
// /api/v1/connections/{id}/pbs/... for a stored connection with type=pbs.
// Reads are open to any authenticated user, same as the PVE connection
// tree; mutations (prune, gc, running a sync/verify job, protect toggle)
// require admin, enforced by requireAdminForMutations on the parent route
// group in server.go.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pbs"
)

func (s *Server) pbsListDatastores(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	stores, err := client.ListDatastores(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, stores)
}

func (s *Server) pbsListNamespaces(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	ns, err := client.ListNamespaces(r.Context(), chi.URLParam(r, "store"), r.URL.Query().Get("parent"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ns)
}

func (s *Server) pbsListGroups(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	groups, err := client.ListGroups(r.Context(), chi.URLParam(r, "store"), r.URL.Query().Get("ns"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) pbsListSnapshots(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	q := r.URL.Query()
	snaps, err := client.ListSnapshots(r.Context(), chi.URLParam(r, "store"), pbs.ListSnapshotsOptions{
		Namespace:  q.Get("ns"),
		BackupType: q.Get("backupType"),
		BackupID:   q.Get("backupId"),
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) pbsSetSnapshotProtected(w http.ResponseWriter, r *http.Request) {
	connID, store := chi.URLParam(r, "id"), chi.URLParam(r, "store")
	var req struct {
		Namespace  string `json:"namespace,omitempty"`
		BackupType string `json:"backupType"`
		BackupID   string `json:"backupId"`
		BackupTime int64  `json:"backupTime"`
		Protected  bool   `json:"protected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.BackupType == "" || req.BackupID == "" || req.BackupTime == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "backupType, backupId, and backupTime are required")
		return
	}
	client, err := s.pbsClientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	opts := pbs.ListSnapshotsOptions{Namespace: req.Namespace, BackupType: req.BackupType, BackupID: req.BackupID}
	if err := client.SetSnapshotProtected(r.Context(), store, opts, req.BackupTime, req.Protected); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.snapshot.protect", "pbs", connID+"/"+store+"/"+req.BackupType+"/"+req.BackupID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type pbsPruneRequest struct {
	Namespace   string `json:"namespace,omitempty"`
	BackupType  string `json:"backupType"`
	BackupID    string `json:"backupId"`
	KeepLast    int    `json:"keepLast,omitempty"`
	KeepHourly  int    `json:"keepHourly,omitempty"`
	KeepDaily   int    `json:"keepDaily,omitempty"`
	KeepWeekly  int    `json:"keepWeekly,omitempty"`
	KeepMonthly int    `json:"keepMonthly,omitempty"`
	KeepYearly  int    `json:"keepYearly,omitempty"`
	DryRun      bool   `json:"dryRun,omitempty"`
}

func (s *Server) pbsPrune(w http.ResponseWriter, r *http.Request) {
	connID, store := chi.URLParam(r, "id"), chi.URLParam(r, "store")
	var req pbsPruneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.BackupType == "" || req.BackupID == "" {
		writeErrorMsg(w, http.StatusBadRequest, "backupType and backupId are required")
		return
	}
	client, err := s.pbsClientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	results, err := client.Prune(r.Context(), store, pbs.PruneOptions{
		Namespace: req.Namespace, BackupType: req.BackupType, BackupID: req.BackupID,
		KeepLast: req.KeepLast, KeepHourly: req.KeepHourly, KeepDaily: req.KeepDaily,
		KeepWeekly: req.KeepWeekly, KeepMonthly: req.KeepMonthly, KeepYearly: req.KeepYearly,
		DryRun: req.DryRun,
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if !req.DryRun {
		s.audit(r, "pbs.prune", "pbs", connID+"/"+store+"/"+req.BackupType+"/"+req.BackupID)
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) pbsStartGC(w http.ResponseWriter, r *http.Request) {
	connID, store := chi.URLParam(r, "id"), chi.URLParam(r, "store")
	client, err := s.pbsClientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.StartGC(r.Context(), store)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.gc.start", "pbs", connID+"/"+store)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsGCStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := client.GCStatusFor(r.Context(), chi.URLParam(r, "store"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) pbsListSyncJobs(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	jobs, err := client.ListSyncJobs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) pbsGetSyncJob(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	job, err := client.GetSyncJob(r.Context(), chi.URLParam(r, "jobId"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) pbsRunSyncJob(w http.ResponseWriter, r *http.Request) {
	connID, jobID := chi.URLParam(r, "id"), chi.URLParam(r, "jobId")
	client, err := s.pbsClientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RunSyncJob(r.Context(), jobID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.sync.run", "pbs", connID+"/"+jobID)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsListVerifyJobs(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	jobs, err := client.ListVerifyJobs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) pbsGetVerifyJob(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	job, err := client.GetVerifyJob(r.Context(), chi.URLParam(r, "jobId"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) pbsRunVerifyJob(w http.ResponseWriter, r *http.Request) {
	connID, jobID := chi.URLParam(r, "id"), chi.URLParam(r, "jobId")
	client, err := s.pbsClientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.RunVerifyJob(r.Context(), jobID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.verify.run", "pbs", connID+"/"+jobID)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsTaskStatus(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	task, err := client.TaskStatus(r.Context(), chi.URLParam(r, "upid"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) pbsTaskLog(w http.ResponseWriter, r *http.Request) {
	client, err := s.pbsClientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	lines, err := client.TaskLog(r.Context(), chi.URLParam(r, "upid"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}
