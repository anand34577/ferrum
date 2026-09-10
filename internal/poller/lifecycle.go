package poller

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/connections"
	"ferrum/internal/pve"
	"ferrum/internal/store"
)

// lifecycleFanoutLimit bounds how many connections the sweep touches at
// once — same reasoning as the alert evaluator's own limit.
const lifecycleFanoutLimit = 8

// orphanDiskRuleID is a fixed, well-known alert_rules row the orphan-disk
// check upserts its findings against, the same way a metric threshold rule
// does for the regular alert evaluator — except this rule is system-managed
// (created by the sweep itself, not the admin) so its id is a constant
// rather than a generated uuid.
const orphanDiskRuleID = "system.orphan-disk"

// retainTagPattern matches a guest tag of the form "retain:<N>d" (e.g.
// "retain:7d") — the one tag convention this sweep understands. PVE tags on
// a guest are semicolon-separated in current versions, comma-separated in
// older ones; both are accepted here.
var retainTagPattern = regexp.MustCompile(`^retain:(\d+)d$`)

// LifecycleEvaluator periodically (a) sweeps guest snapshots past their
// retention window (per-guest "retain:<N>d" tag, or a fleet-wide default —
// dry-run until the admin explicitly enables enforcement) and (b) flags
// storage volumes that no guest's config references any more as low-severity
// alerts. Both passes are read-heavy and only the retention sweep ever
// deletes anything, and only once enforcement is turned on.
type LifecycleEvaluator struct {
	db    *store.DB
	conns *connections.Resolver
}

func NewLifecycleEvaluator(db *store.DB, conns *connections.Resolver) *LifecycleEvaluator {
	return &LifecycleEvaluator{db: db, conns: conns}
}

// Run blocks, evaluating both policies every interval until ctx is canceled.
func (e *LifecycleEvaluator) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.evaluateOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.evaluateOnce(ctx)
		}
	}
}

type lifecycleSettings struct {
	retentionDays int
	enforce       bool
}

func (e *LifecycleEvaluator) loadSettings(ctx context.Context) lifecycleSettings {
	var s lifecycleSettings
	var enforce int
	err := e.db.QueryRowContext(ctx, `SELECT retention_days, enforce FROM lifecycle_settings WHERE id = 1`).
		Scan(&s.retentionDays, &enforce)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("lifecycle evaluator: loading settings failed", "error", err)
	}
	s.enforce = enforce == 1
	return s
}

func (e *LifecycleEvaluator) evaluateOnce(ctx context.Context) {
	settings := e.loadSettings(ctx)

	conns, err := e.conns.List(ctx)
	if err != nil {
		slog.Error("lifecycle evaluator: listing connections failed", "error", err)
		return
	}

	// The orphan-disk check accumulates seen resource ids across every
	// connection so the reconciliation pass (resolving alerts for volumes
	// that are no longer orphaned) can run once at the end.
	seen := map[string]bool{}
	var seenMu sync.Mutex

	var wg sync.WaitGroup
	sem := make(chan struct{}, lifecycleFanoutLimit)
	for _, conn := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(conn connections.Info) {
			defer wg.Done()
			defer func() { <-sem }()

			client, err := e.conns.ClientFor(ctx, conn.ID)
			if err != nil {
				slog.Warn("lifecycle evaluator: connection unreachable, skipping", "connectionId", conn.ID, "name", conn.Name, "error", err)
				return
			}
			resources, err := client.ClusterResources(ctx)
			if err != nil {
				slog.Warn("lifecycle evaluator: fetching cluster resources failed, skipping", "connectionId", conn.ID, "name", conn.Name, "error", err)
				return
			}

			e.sweepRetention(ctx, client, conn, resources, settings)

			found := e.checkOrphanDisks(ctx, client, conn, resources)
			seenMu.Lock()
			for id := range found {
				seen[id] = true
			}
			seenMu.Unlock()
		}(conn)
	}
	wg.Wait()

	e.reconcileOrphanDisks(ctx, seen)
}

// --- Snapshot retention sweep ---

func tagRetentionDays(tags string) (int, bool) {
	for _, tag := range strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' }) {
		tag = strings.TrimSpace(tag)
		m := retainTagPattern.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		if days, err := strconv.Atoi(m[1]); err == nil && days > 0 {
			return days, true
		}
	}
	return 0, false
}

func (e *LifecycleEvaluator) sweepRetention(ctx context.Context, client *pve.Client, conn connections.Info, resources []pve.ClusterResource, settings lifecycleSettings) {
	for _, res := range resources {
		if res.Type != "qemu" && res.Type != "lxc" {
			continue
		}
		if res.Template == 1 {
			continue // templates aren't running guests with a snapshot lifecycle to manage
		}

		days, tagged := tagRetentionDays(res.Tags)
		if !tagged {
			days = settings.retentionDays
		}
		if days <= 0 {
			continue // no policy applies to this guest
		}

		snaps, err := client.ListSnapshots(ctx, res.Type, res.Node, res.VMID)
		if err != nil {
			slog.Warn("lifecycle evaluator: listing snapshots failed", "connectionId", conn.ID, "node", res.Node, "vmid", res.VMID, "error", err)
			continue
		}

		for _, snap := range snapshotsToRetire(snaps, days, time.Now()) {
			e.retireSnapshot(ctx, client, conn, res, snap, days, settings.enforce)
		}
	}
}

// snapshotsToRetire is the pure comparison behind the retention sweep: given
// a guest's snapshots and its effective retention window, which ones are
// past it. "current" is a pseudo-entry PVE includes representing the
// guest's live state, not an actual snapshot — SnapTime is 0 for it, which
// also naturally excludes it from the age check, but it's skipped
// explicitly for clarity.
func snapshotsToRetire(snaps []pve.Snapshot, retentionDays int, now time.Time) []pve.Snapshot {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var out []pve.Snapshot
	for _, snap := range snaps {
		if snap.Name == "current" || snap.SnapTime == 0 {
			continue
		}
		if time.Unix(snap.SnapTime, 0).Before(cutoff) {
			out = append(out, snap)
		}
	}
	return out
}

func (e *LifecycleEvaluator) retireSnapshot(ctx context.Context, client *pve.Client, conn connections.Info, res pve.ClusterResource, snap pve.Snapshot, retentionDays int, enforce bool) {
	name := res.Name
	if name == "" {
		name = fmt.Sprintf("%s/%d", res.Type, res.VMID)
	}
	age := time.Since(time.Unix(snap.SnapTime, 0)).Round(time.Hour)
	detail := fmt.Sprintf("snapshot %q is %s old, past the %dd retention window", snap.Name, age, retentionDays)

	status := "ok"
	var actionErr *string
	if enforce {
		if _, err := client.DeleteSnapshot(ctx, res.Type, res.Node, res.VMID, snap.Name); err != nil {
			status = "error"
			msg := err.Error()
			actionErr = &msg
			slog.Error("lifecycle evaluator: deleting snapshot failed", "connectionId", conn.ID, "node", res.Node, "vmid", res.VMID, "snapshot", snap.Name, "error", err)
		} else {
			slog.Info("lifecycle evaluator: snapshot deleted (retention)", "connectionId", conn.ID, "node", res.Node, "vmid", res.VMID, "snapshot", snap.Name, "age", age)
		}
	}

	e.logAction(ctx, conn, res.Type, res.Node, res.VMID, name, "snapshot.delete", detail, !enforce, status, actionErr)
}

func (e *LifecycleEvaluator) logAction(ctx context.Context, conn connections.Info, guestType, node string, vmid int, guestName, action, detail string, dryRun bool, status string, actionErr *string) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := e.db.ExecContext(ctx, `
		INSERT INTO lifecycle_actions (id, connection_id, connection_name, guest_type, node, vmid, guest_name, action, detail, dry_run, status, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), conn.ID, conn.Name, guestType, node, vmid, guestName, action, detail, boolToInt(dryRun), status, actionErr, now,
	); err != nil {
		slog.Error("lifecycle evaluator: recording action failed", "error", err)
	}
}

// --- Orphaned disk detection ---

// checkOrphanDisks compares every storage's content listing against every
// guest's attached disks on this connection and upserts an alert_instances
// row (against the shared orphanDiskRuleID rule) for each volume that isn't
// referenced by any guest config. Returns the set of resource ids it saw
// this pass, for the caller's reconciliation step.
func (e *LifecycleEvaluator) checkOrphanDisks(ctx context.Context, client *pve.Client, conn connections.Info, resources []pve.ClusterResource) map[string]bool {
	referenced := map[string]bool{}
	type storageRef struct{ node, storage string }
	var storages []storageRef
	seenStorage := map[string]bool{}

	for _, res := range resources {
		switch res.Type {
		case "qemu", "lxc":
			cfg, err := client.GuestConfig(ctx, res.Type, res.Node, res.VMID)
			if err != nil {
				slog.Warn("lifecycle evaluator: fetching guest config failed, skipping for orphan check", "connectionId", conn.ID, "node", res.Node, "vmid", res.VMID, "error", err)
				continue
			}
			for _, d := range cfg.Disks {
				volID, _, _ := strings.Cut(d.Value, ",")
				if strings.Contains(volID, ":") {
					referenced[volID] = true
				}
			}
		case "storage":
			key := res.Node + "|" + res.Storage
			if res.Storage != "" && !seenStorage[key] {
				seenStorage[key] = true
				storages = append(storages, storageRef{node: res.Node, storage: res.Storage})
			}
		}
	}

	seen := map[string]bool{}
	for _, st := range storages {
		items, err := client.StorageContent(ctx, st.node, st.storage)
		if err != nil {
			slog.Warn("lifecycle evaluator: listing storage content failed", "connectionId", conn.ID, "node", st.node, "storage", st.storage, "error", err)
			continue
		}
		for _, item := range orphanedVolumes(referenced, items) {
			resourceID := conn.ID + "|" + item.VolID
			seen[resourceID] = true
			e.upsertOrphanAlert(ctx, conn, resourceID, item.VolID, item.Size)
		}
	}
	return seen
}

// orphanedVolumes is the pure comparison behind the orphan-disk check: which
// guest-disk-content volumes on a storage aren't referenced by any guest's
// config. Only "images" (VM disks) and "rootdir" (container rootfs/mount
// points) entries are guest-owned storage — everything else (iso, vztmpl,
// backup, snippets) is deliberately not guest-attached and out of scope here.
func orphanedVolumes(referenced map[string]bool, items []pve.StorageContentItem) []pve.StorageContentItem {
	var out []pve.StorageContentItem
	for _, item := range items {
		if item.Content != "images" && item.Content != "rootdir" {
			continue
		}
		if referenced[item.VolID] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (e *LifecycleEvaluator) ensureOrphanDiskRule(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO alert_rules (id, name, metric, connection_id, threshold, severity, enabled, created_at)
		VALUES (?, 'Orphaned storage volume', 'storage_orphan_disk', NULL, 0, 'warning', 1, ?)
		ON CONFLICT (id) DO NOTHING`, orphanDiskRuleID, now)
	if err != nil {
		slog.Error("lifecycle evaluator: ensuring orphan-disk alert rule failed", "error", err)
	}
}

func (e *LifecycleEvaluator) upsertOrphanAlert(ctx context.Context, conn connections.Info, resourceID, volID string, size int64) {
	e.ensureOrphanDiskRule(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO alert_instances (id, rule_id, connection_id, connection_name, resource_id, resource_name, metric, value, threshold, severity, status, triggered_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'storage_orphan_disk', ?, 0, 'warning', 'active', ?, ?)
		ON CONFLICT (rule_id, resource_id) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at,
			status = CASE WHEN alert_instances.status = 'silenced' THEN 'silenced' ELSE 'active' END`,
		uuid.NewString(), orphanDiskRuleID, conn.ID, conn.Name, resourceID, volID, float64(size), now, now,
	)
	if err != nil {
		slog.Error("lifecycle evaluator: persisting orphan-disk alert failed", "error", err)
	}
}

// reconcileOrphanDisks resolves previously-flagged orphan-disk alerts whose
// volume either got referenced by a guest again or was deleted — without
// this, a false positive (or a fixed one) would stay active forever.
func (e *LifecycleEvaluator) reconcileOrphanDisks(ctx context.Context, seen map[string]bool) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, resource_id FROM alert_instances WHERE rule_id = ? AND status IN ('active', 'silenced')`, orphanDiskRuleID)
	if err != nil {
		slog.Error("lifecycle evaluator: listing active orphan-disk alerts failed", "error", err)
		return
	}
	type instance struct{ id, resourceID string }
	var stale []instance
	for rows.Next() {
		var inst instance
		if err := rows.Scan(&inst.id, &inst.resourceID); err != nil {
			rows.Close()
			slog.Error("lifecycle evaluator: scanning orphan-disk alerts failed", "error", err)
			return
		}
		stale = append(stale, inst)
	}
	rows.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, inst := range stale {
		if seen[inst.resourceID] {
			continue
		}
		if _, err := e.db.ExecContext(ctx, `
			UPDATE alert_instances SET status = 'resolved', resolved_at = ?, updated_at = ?
			WHERE id = ? AND status IN ('active', 'silenced')`, now, now, inst.id); err != nil {
			slog.Error("lifecycle evaluator: resolving orphan-disk alert failed", "error", err)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
