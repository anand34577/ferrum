// PBS (Proxmox Backup Server) remote endpoints — mounted under
// /api/v1/connections/{id}/pbs/... for a stored connection with type=pbs.
// Reads are open to any authenticated user, same as the PVE connection
// tree; mutations (prune, gc, running a sync/verify job, protect toggle)
// require admin, enforced by requireAdminForMutations on the parent route
// group in server.go.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pbs"
)

// pbsCall resolves the PBS client for connID and runs fn against it. When
// upstream answers 401 (pbs.ErrUnauthorized — the cached ticket PBS just
// rejected), RetryOnUnauthorized drops the cached login and runs fn exactly
// once more with a freshly-resolved client; every other error — including a
// second 401 — is returned as-is. fn must use the client it's handed, not
// one cached across calls, so the retry actually picks up the fresh login.
func pbsCall[T any](s *Server, ctx context.Context, connID string, fn func(*pbs.Client) (T, error)) (T, error) {
	var out T
	err := s.connections.RetryOnUnauthorized(connID, func() error {
		client, err := s.pbsClientFor(ctx, connID)
		if err != nil {
			return err
		}
		out, err = fn(client)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// pbsCallErr is pbsCall for calls that return nothing but an error.
func pbsCallErr(s *Server, ctx context.Context, connID string, fn func(*pbs.Client) error) error {
	_, err := pbsCall(s, ctx, connID, func(c *pbs.Client) (struct{}, error) {
		return struct{}{}, fn(c)
	})
	return err
}

func (s *Server) pbsListDatastores(w http.ResponseWriter, r *http.Request) {
	stores, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.Datastore, error) {
		return c.ListDatastoresWithUsage(r.Context())
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, stores)
}

func (s *Server) pbsListNamespaces(w http.ResponseWriter, r *http.Request) {
	ns, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.Namespace, error) {
		return c.ListNamespaces(r.Context(), chi.URLParam(r, "store"), r.URL.Query().Get("parent"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ns)
}

func (s *Server) pbsListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.BackupGroup, error) {
		return c.ListGroups(r.Context(), chi.URLParam(r, "store"), r.URL.Query().Get("ns"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) pbsListSnapshots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	snaps, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.Snapshot, error) {
		return c.ListSnapshots(r.Context(), chi.URLParam(r, "store"), pbs.ListSnapshotsOptions{
			Namespace:  q.Get("ns"),
			BackupType: q.Get("backupType"),
			BackupID:   q.Get("backupId"),
		})
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
	err := pbsCallErr(s, r.Context(), connID, func(c *pbs.Client) error {
		return c.SetSnapshotProtected(r.Context(), store, pbs.ListSnapshotsOptions{
			Namespace: req.Namespace, BackupType: req.BackupType, BackupID: req.BackupID,
		}, req.BackupTime, req.Protected)
	})
	if err != nil {
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
	results, err := pbsCall(s, r.Context(), connID, func(c *pbs.Client) ([]pbs.PruneResult, error) {
		return c.Prune(r.Context(), store, pbs.PruneOptions{
			Namespace: req.Namespace, BackupType: req.BackupType, BackupID: req.BackupID,
			KeepLast: req.KeepLast, KeepHourly: req.KeepHourly, KeepDaily: req.KeepDaily,
			KeepWeekly: req.KeepWeekly, KeepMonthly: req.KeepMonthly, KeepYearly: req.KeepYearly,
			DryRun: req.DryRun,
		})
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
	upid, err := pbsCall(s, r.Context(), connID, func(c *pbs.Client) (string, error) {
		return c.StartGC(r.Context(), store)
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.gc.start", "pbs", connID+"/"+store)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsGCStatus(w http.ResponseWriter, r *http.Request) {
	status, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) (*pbs.GCStatus, error) {
		return c.GCStatusFor(r.Context(), chi.URLParam(r, "store"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) pbsListSyncJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.SyncJob, error) {
		return c.ListSyncJobs(r.Context())
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) pbsGetSyncJob(w http.ResponseWriter, r *http.Request) {
	job, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) (*pbs.SyncJob, error) {
		return c.GetSyncJob(r.Context(), chi.URLParam(r, "jobId"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) pbsRunSyncJob(w http.ResponseWriter, r *http.Request) {
	connID, jobID := chi.URLParam(r, "id"), chi.URLParam(r, "jobId")
	upid, err := pbsCall(s, r.Context(), connID, func(c *pbs.Client) (string, error) {
		return c.RunSyncJob(r.Context(), jobID)
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.sync.run", "pbs", connID+"/"+jobID)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsListVerifyJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]pbs.VerifyJob, error) {
		return c.ListVerifyJobs(r.Context())
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) pbsGetVerifyJob(w http.ResponseWriter, r *http.Request) {
	job, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) (*pbs.VerifyJob, error) {
		return c.GetVerifyJob(r.Context(), chi.URLParam(r, "jobId"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) pbsRunVerifyJob(w http.ResponseWriter, r *http.Request) {
	connID, jobID := chi.URLParam(r, "id"), chi.URLParam(r, "jobId")
	upid, err := pbsCall(s, r.Context(), connID, func(c *pbs.Client) (string, error) {
		return c.RunVerifyJob(r.Context(), jobID)
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "pbs.verify.run", "pbs", connID+"/"+jobID)
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

func (s *Server) pbsTaskStatus(w http.ResponseWriter, r *http.Request) {
	task, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) (*pbs.Task, error) {
		return c.TaskStatus(r.Context(), chi.URLParam(r, "upid"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) pbsTaskLog(w http.ResponseWriter, r *http.Request) {
	lines, err := pbsCall(s, r.Context(), chi.URLParam(r, "id"), func(c *pbs.Client) ([]string, error) {
		return c.TaskLog(r.Context(), chi.URLParam(r, "upid"))
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}
