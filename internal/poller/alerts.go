// Package poller runs background jobs that need periodic access to every
// configured Proxmox connection — currently just threshold alert evaluation.
package poller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/connections"
	"ferrum/internal/events"
	"ferrum/internal/notify"
	"ferrum/internal/pve"
	"ferrum/internal/store"
)

type rule struct {
	ID           string
	Metric       string
	ConnectionID *string
	Threshold    float64
	Severity     string
}

// AlertEvaluator periodically polls every connection's cluster/resources and
// evaluates enabled alert rules against the live values, opening, updating,
// or resolving alert_instances rows as thresholds are crossed.
type AlertEvaluator struct {
	db       *store.DB
	conns    *connections.Resolver
	notifier *notify.Notifier // nil until SetNotifier is called — every send site is nil-checked
	bus      *events.Bus      // nil until SetBus is called — every publish site is nil-checked

	tickerMu sync.Mutex
	ticker   *time.Ticker // nil until Run starts it; guarded so SetInterval (an HTTP handler goroutine) can reset it safely

	// certCheckMu/lastCertCheck throttle the (network-heavy) per-node
	// certificate fetch independently of the alert poll interval — see
	// checkCertificateExpiry in certificates.go.
	certCheckMu   sync.Mutex
	lastCertCheck time.Time
}

func NewAlertEvaluator(db *store.DB, conns *connections.Resolver) *AlertEvaluator {
	return &AlertEvaluator{db: db, conns: conns}
}

// SetNotifier attaches the Gotify/SMTP dispatcher — mirrors Server.SetOIDC:
// call it (or don't) once at startup, and again whenever the admin settings
// UI saves new notification config, since Notifier itself is mutated in
// place rather than swapped.
func (e *AlertEvaluator) SetNotifier(n *notify.Notifier) {
	e.notifier = n
}

// SetBus attaches the in-process event bus (see internal/events) that the
// SSE endpoint and outgoing webhook dispatcher subscribe to. Mirrors
// SetNotifier: call it once at startup; every publish site below is
// nil-checked so it's optional.
func (e *AlertEvaluator) SetBus(b *events.Bus) {
	e.bus = b
}

// SetInterval changes how often Run re-evaluates rules, effective on the
// next tick — no restart needed. A no-op before Run has started; Run then
// applies whatever interval was saved by that point.
func (e *AlertEvaluator) SetInterval(d time.Duration) {
	e.tickerMu.Lock()
	defer e.tickerMu.Unlock()
	if e.ticker != nil && d > 0 {
		e.ticker.Reset(d)
	}
}

// Run blocks, evaluating rules every interval until ctx is cancelled.
func (e *AlertEvaluator) Run(ctx context.Context, interval time.Duration) {
	e.tickerMu.Lock()
	ticker := time.NewTicker(interval)
	e.ticker = ticker
	e.tickerMu.Unlock()
	defer ticker.Stop()

	e.ensureSystemAlertRules(ctx)
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

func (e *AlertEvaluator) evaluateOnce(ctx context.Context) {
	rules, err := e.loadRules(ctx)
	if err != nil {
		slog.Error("alert evaluator: loading rules failed", "error", err)
		return
	}

	conns, err := e.conns.List(ctx)
	if err != nil {
		slog.Error("alert evaluator: listing connections failed", "error", err)
		return
	}

	// seen tracks which (rule, resource) pairs were observed this tick so
	// resources that vanish (deleted VM, offline node, removed storage) can
	// have their alerts resolved instead of lingering active forever.
	seen := map[string]map[string]bool{} // ruleID -> resourceID set
	polled := map[string]bool{}          // connectionID -> successfully fetched this tick

	// The network fetch (one HTTP round trip per Proxmox host) is the slow
	// part and is safe to parallelize; the DB-writing evaluation step touches
	// the shared `seen` map and sqlite, so that part stays sequential below.
	type fetched struct {
		conn      connections.Info
		resources []pve.ClusterResource
		ok        bool
		err       error
	}
	results := make([]fetched, len(conns))
	var wg sync.WaitGroup
	// Bound concurrency so a fleet of unreachable hosts can't fan out into
	// hundreds of simultaneous dials (each already capped by the client's
	// 15s timeout).
	sem := make(chan struct{}, 8)
	for i, conn := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, conn connections.Info) {
			defer wg.Done()
			defer func() { <-sem }()
			if conn.Type == "pbs" {
				// PBS remotes don't serve /cluster/resources — without this
				// branch they'd be driven through the PVE client below and
				// persisted as permanently "down" on every tick. Probe with
				// the cheapest authenticated call instead so they get real
				// up/down health like every other connection.
				if err := probePBS(ctx, e.conns, conn.ID); err != nil {
					slog.Warn("alert evaluator: connection unreachable, skipping", "connectionId", conn.ID, "name", conn.Name, "error", err)
					results[i] = fetched{conn: conn, err: err}
					return
				}
				results[i] = fetched{conn: conn, ok: true}
				return
			}
			resources, err := fetchClusterResources(ctx, e.conns, conn.ID)
			if err != nil {
				// The inventory UI surfaces unreachable connections; here we
				// still want a trail for "why did alerts stop updating".
				slog.Warn("alert evaluator: connection unreachable, skipping", "connectionId", conn.ID, "name", conn.Name, "error", err)
				results[i] = fetched{conn: conn, err: err}
				return
			}
			results[i] = fetched{conn: conn, resources: resources, ok: true}
		}(i, conn)
	}
	wg.Wait()

	// Connectivity is tracked and notified unconditionally — whether a host
	// answers at all matters regardless of whether the admin has configured
	// any threshold rule, since a fully offline cluster is the one case
	// metric-threshold rules can never catch (there's no metric to evaluate).
	pveResources := map[string][]pve.ClusterResource{}
	for _, f := range results {
		e.recordConnectionHealth(ctx, f.conn, f.ok, f.err)
		if !f.ok {
			continue
		}
		polled[f.conn.ID] = true
		// Smallest viable "this credential still works" signal: a successful
		// authenticated call (cluster/resources for PVE, datastore list for
		// PBS) already IS a successful authenticated request, so there is no
		// separate probe to add.
		if _, err := e.db.ExecContext(ctx, `UPDATE connections SET last_verified_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339), f.conn.ID,
		); err != nil {
			slog.Error("alert evaluator: recording last_verified_at failed", "connectionId", f.conn.ID, "error", err)
		}
		if f.conn.Type == "pve" {
			pveResources[f.conn.ID] = f.resources
			e.evaluateConnection(ctx, f.conn, f.resources, rules, seen)
		}
	}

	e.reconcileMissing(ctx, rules, polled, seen)

	// Certificate expiry and connection-staleness are surfaced through the
	// same alert_rules/alert_instances tables as metric-threshold rules
	// above (see certificates.go) — no parallel alerting mechanism.
	e.checkConnectionStaleness(ctx, conns)
	e.checkCertificateExpiry(ctx, conns, pveResources)
}

// fetchClusterResources resolves a PVE client for connID and fetches
// /cluster/resources, re-running both once when upstream rejects the cached
// ticket with 401 (the retry logs in fresh through the resolver). Non-auth
// errors are returned as-is. Shared by the alert evaluator, the lifecycle
// evaluator, and the certificate check so the re-login path lives in one
// place.
func fetchClusterResources(ctx context.Context, conns *connections.Resolver, connID string) ([]pve.ClusterResource, error) {
	var resources []pve.ClusterResource
	err := conns.RetryOnUnauthorized(connID, func() error {
		client, cerr := conns.ClientFor(ctx, connID)
		if cerr != nil {
			return cerr
		}
		resources, cerr = client.ClusterResources(ctx)
		return cerr
	})
	return resources, err
}

// probePBS checks whether a PBS connection is reachable with the cheapest
// authenticated call its client offers; the result is deliberately opaque —
// only success/failure feeds connection health.
func probePBS(ctx context.Context, conns *connections.Resolver, connID string) error {
	return conns.RetryOnUnauthorized(connID, func() error {
		client, err := conns.PBSClientFor(ctx, connID)
		if err != nil {
			return err
		}
		_, err = client.ListDatastores(ctx)
		return err
	})
}

func (e *AlertEvaluator) evaluateConnection(ctx context.Context, conn connections.Info, resources []pve.ClusterResource, rules []rule, seen map[string]map[string]bool) {
	for _, res := range resources {
		values := map[string]float64{}
		switch res.Type {
		case "node":
			values["node_cpu"] = res.CPU * 100
			if res.MaxMem > 0 {
				values["node_mem"] = float64(res.Mem) / float64(res.MaxMem) * 100
			}
			if res.MaxDisk > 0 {
				values["node_disk"] = float64(res.Disk) / float64(res.MaxDisk) * 100
			}
		case "qemu", "lxc":
			values["guest_cpu"] = res.CPU * 100
			if res.MaxMem > 0 {
				values["guest_mem"] = float64(res.Mem) / float64(res.MaxMem) * 100
			}
		case "storage":
			if res.MaxDisk > 0 {
				values["storage_usage"] = float64(res.Disk) / float64(res.MaxDisk) * 100
			}
		default:
			continue
		}

		for _, ru := range rules {
			if ru.ConnectionID != nil && *ru.ConnectionID != conn.ID {
				continue
			}
			value, ok := values[ru.Metric]
			if !ok {
				continue
			}
			// Prefixed with the connection id: raw PVE resource ids (e.g.
			// "qemu/100") are only unique within one connection, but a rule
			// with ConnectionID == nil evaluates across all of them.
			resourceID := conn.ID + "|" + res.ID
			if seen[ru.ID] == nil {
				seen[ru.ID] = map[string]bool{}
			}
			seen[ru.ID][resourceID] = true

			if value >= ru.Threshold {
				name := res.Name
				if name == "" {
					name = res.Storage
				}
				if name == "" {
					name = res.Node
				}
				e.upsertActive(ctx, ru, conn, resourceID, name, value)
			} else {
				e.resolveIfActive(ctx, ru.ID, resourceID)
			}
		}
	}
}

// optedInEmails looks up every account that's opted in to per-user alert
// emails (user_preferences.notify_email) and has an email address on file.
// Best-effort: a lookup failure just means no per-user recipients get added
// this time, not a failed notification — the admin's global list still goes out.
func (e *AlertEvaluator) optedInEmails(ctx context.Context) []string {
	rows, err := e.db.QueryContext(ctx, `
		SELECT u.email FROM users u JOIN user_preferences p ON p.user_id = u.id
		WHERE p.notify_email = 1 AND u.email != ''`)
	if err != nil {
		slog.Error("alert evaluator: loading opted-in notification emails failed", "error", err)
		return nil
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			slog.Error("alert evaluator: scanning opted-in email failed", "error", err)
			continue
		}
		emails = append(emails, email)
	}
	return emails
}

func (e *AlertEvaluator) loadRules(ctx context.Context) ([]rule, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT id, metric, connection_id, threshold, severity FROM alert_rules WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []rule
	for rows.Next() {
		var ru rule
		var connID *string
		if err := rows.Scan(&ru.ID, &ru.Metric, &connID, &ru.Threshold, &ru.Severity); err != nil {
			return nil, err
		}
		ru.ConnectionID = connID
		rules = append(rules, ru)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

// reconcileMissing resolves still-active (or silenced) alerts whose resource
// no longer appears on a connection that WAS successfully polled this tick —
// without this pass, deleting a VM or unmounting a storage would leave its
// alert active forever.
func (e *AlertEvaluator) reconcileMissing(ctx context.Context, rules []rule, polled map[string]bool, seen map[string]map[string]bool) {
	for _, ru := range rules {
		rows, err := e.db.QueryContext(ctx,
			`SELECT id, resource_id FROM alert_instances WHERE rule_id = ? AND status IN ('active', 'silenced')`, ru.ID)
		if err != nil {
			slog.Error("alert evaluator: listing active instances failed", "error", err)
			continue
		}
		type instance struct{ id, resourceID string }
		var stale []instance
		for rows.Next() {
			var inst instance
			if err := rows.Scan(&inst.id, &inst.resourceID); err != nil {
				rows.Close()
				slog.Error("alert evaluator: scanning instances failed", "error", err)
				break
			}
			stale = append(stale, inst)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			slog.Error("alert enumerator: iterating instances failed", "error", err)
			continue
		}

		for _, inst := range stale {
			connID, _, found := strings.Cut(inst.resourceID, "|")
			if !found || !polled[connID] {
				continue // connection wasn't polled this tick — absence proves nothing
			}
			if seen[ru.ID][inst.resourceID] {
				continue // still present, handled by threshold evaluation
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if _, err := e.db.ExecContext(ctx, `
				UPDATE alert_instances SET status = 'resolved', resolved_at = ?, updated_at = ?
				WHERE id = ? AND status IN ('active', 'silenced')`, now, now, inst.id); err != nil {
				slog.Error("alert evaluator: resolving vanished-resource alert failed", "error", err)
				continue
			}
			slog.Info("alert resolved (resource vanished)", "ruleId", ru.ID, "resourceId", inst.resourceID)
			if e.bus != nil {
				e.bus.Publish(events.Event{
					Type:       events.TypeAlertResolved,
					ResourceID: inst.resourceID,
					Payload:    map[string]any{"ruleId": ru.ID, "reason": "resource vanished"},
				})
			}
		}
	}
}

func (e *AlertEvaluator) upsertActive(ctx context.Context, ru rule, conn connections.Info, resourceID, resourceName string, value float64) {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID string
	// isNew must be answered by ErrNoRows specifically: any other lookup
	// error is unknown state, so the whole upsert is skipped rather than
	// risking a spurious "alert triggered" notification for an existing row.
	err := e.db.QueryRowContext(ctx, `SELECT id FROM alert_instances WHERE rule_id = ? AND resource_id = ?`, ru.ID, resourceID).Scan(&existingID)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		slog.Error("alert evaluator: checking for an existing instance failed", "rule", ru.ID, "resource", resourceID, "error", err)
		return
	}

	// Atomic upsert: the unique (rule_id, resource_id) constraint decides
	// insert vs update in one statement, so a racing evaluator can't create
	// duplicates. Silence is preserved across updates.
	if _, err := e.db.ExecContext(ctx, `
		INSERT INTO alert_instances (id, rule_id, connection_id, connection_name, resource_id, resource_name, metric, value, threshold, severity, status, triggered_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
		ON CONFLICT (rule_id, resource_id) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at,
			status = CASE WHEN alert_instances.status = 'silenced' THEN 'silenced' ELSE 'active' END`,
		uuid.NewString(), ru.ID, conn.ID, conn.Name, resourceID, resourceName, ru.Metric, value, ru.Threshold, ru.Severity, now, now,
	); err != nil {
		slog.Error("alert evaluator: persisting instance failed", "error", err)
		return
	}
	if isNew {
		slog.Warn("alert triggered", "rule", ru.Metric, "connection", conn.Name, "resource", resourceName, "value", value, "threshold", ru.Threshold, "severity", ru.Severity)
		if e.bus != nil {
			e.bus.Publish(events.Event{
				Type:         events.TypeAlertTriggered,
				ConnectionID: conn.ID,
				ResourceID:   resourceID,
				Payload: map[string]any{
					"ruleId":         ru.ID,
					"metric":         ru.Metric,
					"connectionName": conn.Name,
					"resourceName":   resourceName,
					"value":          value,
					"threshold":      ru.Threshold,
					"severity":       ru.Severity,
				},
			})
		}
		if e.notifier != nil {
			title := fmt.Sprintf("[%s] %s", strings.ToUpper(ru.Severity), resourceName)
			message := fmt.Sprintf("%s on %s is at %.0f%% (threshold %.0f%%)", ru.Metric, conn.Name, value, ru.Threshold)
			emails := e.optedInEmails(context.Background())
			// Fire-and-forget on a background context, not ctx: ctx belongs
			// to this poll tick and may already be near its deadline by the
			// time an SMTP round trip would need it.
			go func() {
				for _, err := range e.notifier.Notify(context.Background(), title, message, emails...) {
					slog.Error("alert notification failed", "error", err)
				}
			}()
		}
	}
}

// ConnectionHealth is one row of connection_health — whether a configured
// Proxmox connection answered on the evaluator's last poll.
type ConnectionHealth struct {
	ConnectionID   string `json:"connectionId"`
	ConnectionName string `json:"connectionName"`
	Status         string `json:"status"` // "up" | "down"
	LastError      string `json:"lastError,omitempty"`
	Since          string `json:"since"`
	UpdatedAt      string `json:"updatedAt"`
}

// ConnectionHealthList returns every tracked connection's last-known
// reachability, most-recently-changed first — used by the API to surface
// "is Proxmox itself reachable" independently of any alert rule.
func (e *AlertEvaluator) ConnectionHealthList(ctx context.Context) ([]ConnectionHealth, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT connection_id, connection_name, status, last_error, since, updated_at
		FROM connection_health ORDER BY since DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ConnectionHealth{}
	for rows.Next() {
		var h ConnectionHealth
		var lastError *string
		if err := rows.Scan(&h.ConnectionID, &h.ConnectionName, &h.Status, &lastError, &h.Since, &h.UpdatedAt); err != nil {
			return nil, err
		}
		if lastError != nil {
			h.LastError = *lastError
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// recordConnectionHealth persists whether conn answered this tick and fires
// a notification on every up<->down transition — a connection going
// unreachable is treated as a standalone, always-on critical event, never
// silently absorbed into "skipped this tick" the way it was before: if
// Proxmox itself is down, that's exactly when a notification matters most,
// and it must not depend on the admin having set up a metric threshold rule.
func (e *AlertEvaluator) recordConnectionHealth(ctx context.Context, conn connections.Info, ok bool, fetchErr error) {
	if conn.ID == "" {
		return // zero-value fetched{} for a connection that was never dialed (shouldn't happen, but don't record garbage)
	}
	status := "up"
	var lastError string
	if !ok {
		status = "down"
		if fetchErr != nil {
			lastError = fetchErr.Error()
		}
	}

	var prevStatus string
	hasPrev := e.db.QueryRowContext(ctx, `SELECT status FROM connection_health WHERE connection_id = ?`, conn.ID).Scan(&prevStatus) == nil
	now := time.Now().UTC().Format(time.RFC3339)
	changed := !hasPrev || prevStatus != status

	if changed {
		if _, err := e.db.ExecContext(ctx, `
			INSERT INTO connection_health (connection_id, connection_name, status, last_error, since, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (connection_id) DO UPDATE SET
				connection_name = excluded.connection_name, status = excluded.status,
				last_error = excluded.last_error, since = excluded.since, updated_at = excluded.updated_at`,
			conn.ID, conn.Name, status, nullableString(lastError), now, now,
		); err != nil {
			slog.Error("alert evaluator: recording connection health failed", "connectionId", conn.ID, "error", err)
			return
		}
	} else {
		if _, err := e.db.ExecContext(ctx, `
			UPDATE connection_health SET connection_name = ?, last_error = ?, updated_at = ? WHERE connection_id = ?`,
			conn.Name, nullableString(lastError), now, conn.ID,
		); err != nil {
			slog.Error("alert evaluator: updating connection health failed", "connectionId", conn.ID, "error", err)
		}
		return
	}

	if !hasPrev && status == "up" {
		return // first-ever poll of a healthy connection isn't a "recovery" worth announcing
	}
	slog.Warn("connection health changed", "connectionId", conn.ID, "name", conn.Name, "status", status)
	if e.bus != nil {
		evtType := events.TypeConnectionUp
		if status == "down" {
			evtType = events.TypeConnectionDown
		}
		e.bus.Publish(events.Event{
			Type:         evtType,
			ConnectionID: conn.ID,
			Payload:      map[string]any{"connectionName": conn.Name, "status": status, "lastError": lastError},
		})
	}
	if e.notifier == nil {
		return
	}
	var title, message string
	if status == "down" {
		title = fmt.Sprintf("[CRITICAL] %s unreachable", conn.Name)
		message = fmt.Sprintf("Ferrum can no longer reach %q. Last error: %s", conn.Name, lastError)
	} else {
		title = fmt.Sprintf("[RESOLVED] %s reachable again", conn.Name)
		message = fmt.Sprintf("Ferrum has re-established contact with %q.", conn.Name)
	}
	emails := e.optedInEmails(context.Background())
	go func() {
		for _, err := range e.notifier.Notify(context.Background(), title, message, emails...) {
			slog.Error("connection health notification failed", "error", err)
		}
	}()
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (e *AlertEvaluator) resolveIfActive(ctx context.Context, ruleID, resourceID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := e.db.ExecContext(ctx, `
		UPDATE alert_instances SET status = 'resolved', resolved_at = ?, updated_at = ?
		WHERE rule_id = ? AND resource_id = ? AND status = 'active'`, now, now, ruleID, resourceID)
	if err != nil {
		slog.Error("alert evaluator: resolving instance failed", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("alert resolved", "ruleId", ruleID, "resourceId", resourceID)
		if e.bus != nil {
			e.bus.Publish(events.Event{
				Type:       events.TypeAlertResolved,
				ResourceID: resourceID,
				Payload:    map[string]any{"ruleId": ruleID},
			})
		}
	}
}
