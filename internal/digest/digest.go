// Package digest builds the periodic fleet health summary (uptime, backup
// success rate, active alerts, capacity) and delivers it through the
// existing notify.Notifier — it doesn't add a new delivery mechanism, only a
// new message.
//
// This package deliberately does NOT import internal/api: internal/api's
// settings handlers (internal/api/digest_settings.go) import this package to
// drive the HTTP layer, so the reverse import would be a cycle. Where that
// means duplicating a shape internal/api/overview.go already has (the
// per-connection fleet rollup), it's duplicated here in a smaller form
// rather than shared.
package digest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ferrum/internal/connections"
	"ferrum/internal/pbs"
	"ferrum/internal/pve"
	"ferrum/internal/store"
)

// fanoutLimit bounds how many connections are polled concurrently while
// building a digest, mirroring internal/api's fleetFanoutLimit.
const fanoutLimit = 8

// backupLookback is how far back "backup success rate" looks for vzdump
// tasks, regardless of how often the digest itself is sent — a digest fired
// daily still reports the trailing week, since a single day's backup count
// is too small a sample to mean much.
const backupLookback = 7 * 24 * time.Hour

// maxTasksScanned bounds how many recent cluster tasks are inspected per
// connection when tallying backups — /cluster/tasks has no server-side time
// filter (and takes no query parameters at all: pve-manager registers the
// endpoint with an empty property list, and pmxcfs only broadcasts the
// newest ~32 KiB of tasks per node anyway), so this is applied client-side
// after the fetch. It's set high enough to keep everything the endpoint can
// return, leaving the backupLookback window below as the real filter.
const maxTasksScanned = 5000

// ConnectionSummary is one connection's rollup within a FleetSummary.
type ConnectionSummary struct {
	Name      string
	Type      string // "pve" | "pbs" — a pbs row reports Datastores, not nodes/guests
	Error     string // non-empty when Reachable is false
	Reachable bool

	// PBS only: how many backup datastores the server answered with.
	Datastores int

	Nodes, OnlineNodes         int
	Guests, RunningGuests      int
	CPUPct, MemPct, StoragePct float64
	BackupTotal, BackupOK      int // vzdump tasks observed in backupLookback
}

// FleetSummary is the pure data model behind the digest email/webhook —
// built once per send, then rendered to text by Render.
type FleetSummary struct {
	GeneratedAt time.Time

	Connections          []ConnectionSummary
	TotalConnections     int
	ReachableConnections int

	TotalNodes, OnlineNodes             int
	TotalGuests, RunningGuests          int
	AvgCPUPct, AvgMemPct, AvgStoragePct float64

	AlertsCritical, AlertsWarning int

	BackupTotal, BackupOK int
}

// Build queries every configured connection (like internal/api/overview.go's
// fleetOverviewHandler) plus the active-alert counts, and assembles a
// FleetSummary. One unreachable connection doesn't abort the whole digest —
// it's reported inline via ConnectionSummary.Error, same as the overview
// page.
func Build(ctx context.Context, db *store.DB, conns *connections.Resolver) (FleetSummary, error) {
	summary := FleetSummary{GeneratedAt: time.Now().UTC()}

	infos, err := conns.List(ctx)
	if err != nil {
		return summary, fmt.Errorf("listing connections: %w", err)
	}
	summary.TotalConnections = len(infos)

	crit, warn, err := activeAlertCounts(ctx, db)
	if err != nil {
		// Best-effort: a digest missing alert counts is still useful; don't
		// fail the whole send over it.
		slog.Warn("digest: loading active alert counts failed", "error", err)
	}
	summary.AlertsCritical, summary.AlertsWarning = crit, warn

	results := make([]ConnectionSummary, len(infos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fanoutLimit)
	for i, info := range infos {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, info connections.Info) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = buildConnectionSummary(ctx, conns, info)
		}(i, info)
	}
	wg.Wait()

	var cpuSum, memSum, stoSum float64
	var cpuN, memN, stoN int
	for _, cs := range results {
		summary.Connections = append(summary.Connections, cs)
		if !cs.Reachable {
			continue
		}
		summary.ReachableConnections++
		summary.TotalNodes += cs.Nodes
		summary.OnlineNodes += cs.OnlineNodes
		summary.TotalGuests += cs.Guests
		summary.RunningGuests += cs.RunningGuests
		summary.BackupTotal += cs.BackupTotal
		summary.BackupOK += cs.BackupOK
		if cs.OnlineNodes > 0 {
			cpuSum += cs.CPUPct
			memSum += cs.MemPct
			cpuN++
			memN++
		}
		if cs.StoragePct > 0 {
			stoSum += cs.StoragePct
			stoN++
		}
	}
	if cpuN > 0 {
		summary.AvgCPUPct = cpuSum / float64(cpuN)
	}
	if memN > 0 {
		summary.AvgMemPct = memSum / float64(memN)
	}
	if stoN > 0 {
		summary.AvgStoragePct = stoSum / float64(stoN)
	}

	return summary, nil
}

func buildConnectionSummary(ctx context.Context, conns *connections.Resolver, info connections.Info) ConnectionSummary {
	cs := ConnectionSummary{Name: info.Name, Type: info.Type}

	if info.Type == "pbs" {
		// PBS remotes don't serve /cluster/resources or the vzdump task list
		// — driven through the PVE path below they'd all be reported
		// UNREACHABLE. Probe with the cheapest authenticated call the PBS
		// client offers instead so a healthy server shows as reachable (its
		// usage numbers come from the PBS-specific pages, not this digest).
		var stores []pbs.Datastore
		err := conns.RetryOnUnauthorized(info.ID, func() error {
			client, cerr := conns.PBSClientFor(ctx, info.ID)
			if cerr != nil {
				return cerr
			}
			stores, cerr = client.ListDatastores(ctx)
			return cerr
		})
		if err != nil {
			cs.Error = err.Error()
			return cs
		}
		cs.Reachable = true
		cs.Datastores = len(stores)
		return cs
	}

	// Retry once on a 401: ticket-auth clients are cached for ticketTTL, and
	// a ticket that expired between two digest sends would otherwise label
	// the whole connection UNREACHABLE even though a fresh login works.
	var resources []pve.ClusterResource
	err := conns.RetryOnUnauthorized(info.ID, func() error {
		client, cerr := conns.ClientFor(ctx, info.ID)
		if cerr != nil {
			return cerr
		}
		resources, cerr = client.ClusterResources(ctx)
		return cerr
	})
	if err != nil {
		cs.Error = err.Error()
		return cs
	}
	cs.Reachable = true

	var cpuCores int
	var usedCore float64
	var memTotal, memUsed int64
	for _, res := range resources {
		switch res.Type {
		case "node":
			cs.Nodes++
			if res.Status == "online" {
				cs.OnlineNodes++
				cpuCores += res.MaxCPU
				usedCore += res.CPU * float64(res.MaxCPU)
				memTotal += res.MaxMem
				memUsed += res.Mem
			}
		case "qemu", "lxc":
			if res.Template == 1 {
				continue
			}
			cs.Guests++
			if res.Status == "running" {
				cs.RunningGuests++
			}
		}
	}
	if cpuCores > 0 {
		cs.CPUPct = usedCore / float64(cpuCores) * 100
	}
	if memTotal > 0 {
		cs.MemPct = float64(memUsed) / float64(memTotal) * 100
	}
	cs.StoragePct = storagePct(resources)

	var tasks []pve.Task
	err = conns.RetryOnUnauthorized(info.ID, func() error {
		client, cerr := conns.ClientFor(ctx, info.ID)
		if cerr != nil {
			return cerr
		}
		tasks, cerr = client.ClusterTasks(ctx, maxTasksScanned)
		return cerr
	})
	if err != nil {
		// Backup stats are a bonus, not required for the rest of the
		// summary to be useful.
		slog.Warn("digest: fetching cluster tasks failed", "connection", info.Name, "error", err)
		return cs
	}
	cutoff := time.Now().Add(-backupLookback).Unix()
	for _, t := range tasks {
		if t.Type != "vzdump" || t.StartTime < cutoff {
			continue
		}
		t.Normalize()
		if t.Status == "running" {
			continue // not finished yet, don't count either way
		}
		cs.BackupTotal++
		if t.Status == "OK" {
			cs.BackupOK++
		}
	}
	return cs
}

// storagePct gives a simple used/total percentage across every storage
// resource reported >0 capacity, without internal/api's cross-node
// dedup pass (dedupeSharedStorage) — a digest's storage figure is a rough
// capacity signal, not a billing-grade total, so a shared NFS mount counted
// once per mounting node just skews slightly high rather than needing that
// extra bookkeeping here.
func storagePct(resources []pve.ClusterResource) float64 {
	var total, used int64
	for _, res := range resources {
		if res.Type != "storage" || res.MaxDisk <= 0 {
			continue
		}
		total += res.MaxDisk
		used += res.Disk
	}
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func activeAlertCounts(ctx context.Context, db *store.DB) (critical, warning int, err error) {
	rows, err := db.QueryContext(ctx, `
		SELECT severity, COUNT(*) FROM alert_instances WHERE status = 'active' GROUP BY severity`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var severity string
		var n int
		if err := rows.Scan(&severity, &n); err != nil {
			return critical, warning, err
		}
		if severity == "critical" {
			critical += n
		} else {
			warning += n
		}
	}
	return critical, warning, rows.Err()
}

// Render turns the summary into an email subject + plain-text body. HTML
// isn't produced: notify.Notifier.Notify sends a plain-text SMTP body (see
// internal/notify/notify.go's buildMessage), so an HTML part would never
// actually be used by the one delivery path this reuses.
func (s FleetSummary) Render() (subject, plainBody string) {
	subject = fmt.Sprintf("Ferrum fleet digest — %s", s.GeneratedAt.Format("2006-01-02"))

	var b strings.Builder
	fmt.Fprintf(&b, "Ferrum Fleet Health Digest\nGenerated %s\n\n", s.GeneratedAt.Format(time.RFC3339))

	fmt.Fprintf(&b, "Connections:    %d/%d reachable\n", s.ReachableConnections, s.TotalConnections)
	fmt.Fprintf(&b, "Nodes:          %d/%d online\n", s.OnlineNodes, s.TotalNodes)
	fmt.Fprintf(&b, "Guests:         %d/%d running\n", s.RunningGuests, s.TotalGuests)
	fmt.Fprintf(&b, "Capacity:       CPU %.1f%%  Memory %.1f%%  Storage %.1f%% (fleet average)\n", s.AvgCPUPct, s.AvgMemPct, s.AvgStoragePct)
	if s.BackupTotal > 0 {
		fmt.Fprintf(&b, "Backups (7d):   %d/%d succeeded (%.0f%%)\n", s.BackupOK, s.BackupTotal, float64(s.BackupOK)/float64(s.BackupTotal)*100)
	} else {
		b.WriteString("Backups (7d):   no vzdump tasks observed\n")
	}
	fmt.Fprintf(&b, "Active alerts:  %d critical, %d warning\n", s.AlertsCritical, s.AlertsWarning)

	b.WriteString("\nPer-connection detail:\n")
	for _, c := range s.Connections {
		if !c.Reachable {
			fmt.Fprintf(&b, "  - %s: UNREACHABLE (%s)\n", c.Name, c.Error)
			continue
		}
		if c.Type == "pbs" {
			fmt.Fprintf(&b, "  - %s: PBS backup server reachable (%d datastores)\n", c.Name, c.Datastores)
			continue
		}
		fmt.Fprintf(&b, "  - %s: %d/%d nodes online, %d/%d guests running, CPU %.0f%%, Mem %.0f%%, Storage %.0f%%",
			c.Name, c.OnlineNodes, c.Nodes, c.RunningGuests, c.Guests, c.CPUPct, c.MemPct, c.StoragePct)
		if c.BackupTotal > 0 {
			fmt.Fprintf(&b, ", backups %d/%d ok", c.BackupOK, c.BackupTotal)
		}
		b.WriteString("\n")
	}

	return subject, b.String()
}
