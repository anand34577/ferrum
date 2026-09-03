package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"ferrum/internal/pve"
)

// fleetFanoutLimit bounds how many connections are polled concurrently per
// request, so a fleet of many hosts can't open hundreds of simultaneous
// dials at once.
const fleetFanoutLimit = 8

// The fleet overview is the normalized data model behind the multi-connection
// landing page: one call that rolls every configured Proxmox cluster/server
// up into comparable per-connection summaries — node/guest counts, weighted
// CPU utilization, memory, storage by plugin type, cluster identity, and
// active Ferrum alerts. Widgets and the Overview page render directly from
// this instead of re-aggregating raw cluster/resources client-side.
type fleetOverview struct {
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Online       bool   `json:"online"`
	Error        string `json:"error,omitempty"`
	LatencyMs    int64  `json:"latencyMs,omitempty"` // round-trip time of the cluster/resources call
	CheckedAt    string `json:"checkedAt"`           // RFC3339 timestamp of this poll, regardless of outcome

	// Nil for standalone servers (no cluster manager); quorate mirrors
	// /cluster/status.
	Cluster *fleetCluster `json:"cluster,omitempty"`

	Nodes     fleetNodeSummary  `json:"nodes"`
	VMs       fleetGuestSummary `json:"vms"`
	LXCs      fleetGuestSummary `json:"lxcs"`
	Templates int               `json:"templates"`
	HAGuests  int               `json:"haGuests"`

	CPU fleetCPUSummary     `json:"cpu"`
	Mem fleetMemorySummary  `json:"memory"`
	Sto fleetStorageSummary `json:"storage"`

	Alerts fleetAlertSummary `json:"alerts"`
}

type fleetCluster struct {
	Name    string `json:"name"`
	Quorate bool   `json:"quorate"`
	Nodes   int    `json:"nodes"`
}

type fleetNodeSummary struct {
	Total  int `json:"total"`
	Online int `json:"online"`
	Cores  int `json:"cores"`
}

type fleetGuestSummary struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type fleetCPUSummary struct {
	Cores    int     `json:"cores"`     // physical cores across online nodes
	UsedCore float64 `json:"usedCores"` // cpu fraction × cores, summed
	Pct      float64 `json:"pct"`       // weighted utilization 0..100
}

type fleetMemorySummary struct {
	Total int64   `json:"total"`
	Used  int64   `json:"used"`
	Pct   float64 `json:"pct"`
}

type fleetStorageSummary struct {
	Total  int64            `json:"total"`
	Used   int64            `json:"used"`
	Pct    float64          `json:"pct"`
	ByType map[string]int64 `json:"byType"` // used bytes per plugin type (dir, zfspool, rbd, nfs, ...)
}

type fleetAlertSummary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
}

// fleetOverviewHandler aggregates every connection. Like /inventory, one
// unreachable Proxmox host doesn't blank the fleet — it's reported inline.
func (s *Server) fleetOverviewHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, host, port FROM connections ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	type conn struct {
		ID, Name, Host string
		Port           int
	}
	var conns []conn
	for rows.Next() {
		var c conn
		if err := rows.Scan(&c.ID, &c.Name, &c.Host, &c.Port); err != nil {
			rows.Close()
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		conns = append(conns, c)
	}
	rows.Close()

	// Active alerts grouped per connection (Ferrum's own alert engine).
	alertsByConn := map[string]fleetAlertSummary{}
	alertRows, err := s.db.QueryContext(r.Context(), `
		SELECT connection_id, severity, COUNT(*) FROM alert_instances
		WHERE status = 'active' GROUP BY connection_id, severity`)
	if err == nil {
		for alertRows.Next() {
			var connID, severity string
			var n int
			if err := alertRows.Scan(&connID, &severity, &n); err == nil {
				a := alertsByConn[connID]
				if severity == "critical" {
					a.Critical += n
				} else {
					a.Warning += n
				}
				alertsByConn[connID] = a
			}
		}
		alertRows.Close()
	} else {
		slog.Warn("fleet overview: alert query failed", "error", err)
	}

	// Fan out to every connection concurrently — sequential polling means one
	// slow or unreachable host (its own 15s HTTP timeout) stalls the whole
	// fleet view on every refresh. Each host is independent, so run them in
	// parallel and let the slowest one bound the request instead of the sum.
	out := make([]fleetOverview, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = s.buildFleetEntry(r.Context(), c.ID, c.Name, c.Host, c.Port, alertsByConn[c.ID])
		}(i, c)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) buildFleetEntry(ctx context.Context, id, name, host string, port int, alerts fleetAlertSummary) fleetOverview {
	entry := fleetOverview{
		ConnectionID: id, Name: name, Host: host, Port: port,
		Alerts: alerts,
	}

	start := time.Now()
	client, err := s.clientFor(ctx, id)
	if err != nil {
		entry.Error = err.Error()
		entry.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		return entry
	}

	resources, err := client.ClusterResources(ctx)
	entry.LatencyMs = time.Since(start).Milliseconds()
	entry.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.Online = true

	if status, err := client.ClusterStatus(ctx); err == nil {
		for _, row := range status {
			if row.Type == "cluster" {
				entry.Cluster = &fleetCluster{
					Name:    row.Name,
					Quorate: row.Quorate == 1,
					Nodes:   countType(status, "node"),
				}
				break
			}
		}
	}

	entry.Sto.ByType = map[string]int64{}
	var storageRows []pve.ClusterResource
	for _, res := range resources {
		switch res.Type {
		case "node":
			entry.Nodes.Total++
			if res.Status == "online" {
				entry.Nodes.Online++
				entry.Nodes.Cores += res.MaxCPU
				entry.CPU.Cores += res.MaxCPU
				entry.CPU.UsedCore += res.CPU * float64(res.MaxCPU)
				entry.Mem.Total += res.MaxMem
				entry.Mem.Used += res.Mem
			}
		case "qemu", "lxc":
			if res.Template == 1 {
				// Templates are qemu rows in /cluster/resources.
				entry.Templates++
				continue
			}
			summary := &entry.VMs
			if res.Type == "lxc" {
				summary = &entry.LXCs
			}
			summary.Total++
			switch res.Status {
			case "running":
				summary.Running++
			case "stopped":
				summary.Stopped++
			}
			if res.HAState != "" {
				entry.HAGuests++
			}
		case "storage":
			if res.MaxDisk > 0 {
				storageRows = append(storageRows, res)
			}
		}
	}
	for _, res := range dedupeSharedStorage(storageRows) {
		entry.Sto.Total += res.MaxDisk
		entry.Sto.Used += res.Disk
		plugin := res.PluginType
		if plugin == "" {
			plugin = "unknown"
		}
		entry.Sto.ByType[plugin] += res.Disk
	}
	if entry.CPU.Cores > 0 {
		entry.CPU.Pct = entry.CPU.UsedCore / float64(entry.CPU.Cores) * 100
	}
	if entry.Mem.Total > 0 {
		entry.Mem.Pct = float64(entry.Mem.Used) / float64(entry.Mem.Total) * 100
	}
	if entry.Sto.Total > 0 {
		entry.Sto.Pct = float64(entry.Sto.Used) / float64(entry.Sto.Total) * 100
	}

	return entry
}

// localOnlyPluginTypes never represent a physical volume shared across
// nodes — every other plugin type (nfs, cifs, pbs, cephfs, rbd, iscsi, ...)
// is treated as network storage even when PVE's own Shared flag is 0, since
// plenty of real setups add the same NFS/CIFS target as a near-identical
// per-node definition without ever ticking "Shared" in the storage config.
var localOnlyPluginTypes = map[string]bool{"dir": true, "lvm": true, "lvmthin": true, "zfspool": true, "btrfs": true}

// dedupeSharedStorage collapses cluster/resources storage rows that
// represent the *same* physical volume reported once per node it's mounted
// on — summing them as-is (the naive approach) multiplied a shared pool's
// capacity by the node count, e.g. an 8-node cluster made a 2TB NFS share
// read as 16TB. Local-only storage (dir/lvm/zfspool/...) is left exactly as
// reported: it's genuinely separate capacity per node.
//
// Within the "could be shared" group, rows are deduped by an exact
// (name, total, used) match: two truly independent volumes are vanishingly
// unlikely to report byte-for-byte identical total *and* used capacity at
// the same instant, while duplicate reports of one physical volume always do.
func dedupeSharedStorage(rows []pve.ClusterResource) []pve.ClusterResource {
	out := make([]pve.ClusterResource, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Shared == 0 && localOnlyPluginTypes[r.PluginType] {
			out = append(out, r)
			continue
		}
		key := fmt.Sprintf("%s|%d|%d", r.Storage, r.MaxDisk, r.Disk)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func countType(rows []pve.ClusterStatus, typ string) int {
	n := 0
	for _, r := range rows {
		if r.Type == typ {
			n++
		}
	}
	return n
}
