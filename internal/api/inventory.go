package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

type connectionInventory struct {
	ConnectionID string                `json:"connectionId"`
	Name         string                `json:"name"`
	Online       bool                  `json:"online"`
	Error        string                `json:"error,omitempty"`
	Resources    []pve.ClusterResource `json:"resources,omitempty"`
}

// inventoryOverview fetches /cluster/resources for every configured
// connection and returns them grouped by connection. A single unreachable
// connection does not fail the whole request — it's reported inline so the
// UI can show it as offline instead of blanking the page.
func (s *Server) inventoryOverview(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name FROM connections ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	type conn struct{ ID, Name string }
	var conns []conn
	for rows.Next() {
		var c conn
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			rows.Close()
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		conns = append(conns, c)
	}
	rows.Close()

	// Fan out concurrently — see buildFleetEntry in overview.go for why a
	// sequential per-connection loop doesn't scale to a real fleet.
	out := make([]connectionInventory, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = s.buildInventoryEntry(r.Context(), c.ID, c.Name)
		}(i, c)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) buildInventoryEntry(ctx context.Context, id, name string) connectionInventory {
	entry := connectionInventory{ConnectionID: id, Name: name}
	client, err := s.clientFor(ctx, id)
	if err != nil {
		slog.Warn("connection unreachable", "connectionId", id, "name", name, "error", err)
		entry.Error = err.Error()
		return entry
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		slog.Warn("cluster/resources call failed", "connectionId", id, "name", name, "error", err)
		entry.Error = err.Error()
		return entry
	}
	entry.Online = true
	entry.Resources = resources
	return entry
}

func (s *Server) guestPowerAction(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	guestType := chi.URLParam(r, "type")
	node := chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	action := chi.URLParam(r, "action")
	if !allowedPowerActions[action] {
		writeErrorMsg(w, http.StatusBadRequest, "unsupported action")
		return
	}
	// LXC suspend/resume (criu checkpoint) is best-effort and frequently
	// unsupported by the container's kernel/rootfs — PVE reports a generic
	// 5xx for it, not a helpful message. Qemu supports both reliably.
	if guestType == "lxc" && (action == "suspend" || action == "resume") {
		writeErrorMsg(w, http.StatusBadRequest, "suspend/resume is not supported for containers")
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.GuestPowerAction(r.Context(), guestType, node, vmid, action)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	s.audit(r, "vm."+action, "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}

var allowedPowerActions = map[string]bool{
	"start": true, "stop": true, "shutdown": true, "reboot": true, "reset": true, "suspend": true, "resume": true,
}

func (s *Server) nodeTasks(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	node := chi.URLParam(r, "node")

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	tasks, err := client.NodeTasks(r.Context(), node, 100)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}
