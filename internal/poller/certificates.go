package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"ferrum/internal/connections"
)

// Synthetic, always-present alert_rules rows that back the built-in
// certificate-expiry and connection-staleness checks below. They live in
// the same alert_rules/alert_instances tables as user-defined metric
// threshold rules (see evaluateConnection in alerts.go) rather than a
// parallel table, so the existing /alerts UI, silence, and notification
// plumbing all work for them unmodified. Their ids are fixed so
// ensureSystemAlertRules is idempotent — INSERT ... ON CONFLICT DO NOTHING.
const (
	certExpiryRuleID      = "system-cert-expiry"
	connectionStaleRuleID = "system-connection-stale"

	// Certificate expiry thresholds, in days remaining until notAfter.
	certWarnDays     = 30.0
	certCriticalDays = 7.0

	// Connection staleness thresholds: how long since a connection last
	// answered a poll (connections.last_verified_at, bumped in
	// evaluateOnce). The evaluator polls every connection on every tick
	// (60s by default), so anything beyond a day without a successful poll
	// means sustained trouble, not a blip — connection_health already
	// notifies on the up/down transition itself; this is the persistent,
	// silenceable alert-list counterpart.
	staleWarn     = 24 * time.Hour
	staleCritical = 72 * time.Hour

	// certCheckInterval bounds how often the (network-heavy — one call per
	// node) certificate fetch runs, independent of the alert poll interval:
	// certificate lifetimes are measured in weeks, not seconds.
	certCheckInterval = 6 * time.Hour
)

// ensureSystemAlertRules seeds the two built-in rules once so they always
// exist for evaluateOnce to attach instances to — safe to call on every
// Run() start; a second call is a no-op.
func (e *AlertEvaluator) ensureSystemAlertRules(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	rules := []struct{ id, name, metric string }{
		{certExpiryRuleID, "Certificate expiring soon (built-in)", "cert_expiry"},
		{connectionStaleRuleID, "Connection not recently verified (built-in)", "connection_stale"},
	}
	for _, ru := range rules {
		if _, err := e.db.ExecContext(ctx, `
			INSERT INTO alert_rules (id, name, metric, connection_id, threshold, severity, enabled, created_at)
			VALUES (?, ?, ?, NULL, 0, 'warning', 1, ?)
			ON CONFLICT (id) DO NOTHING`,
			ru.id, ru.name, ru.metric, now,
		); err != nil {
			slog.Error("alert evaluator: seeding built-in alert rule failed", "rule", ru.id, "error", err)
		}
	}
}

// checkConnectionStaleness flags any connection whose credential hasn't
// been successfully exercised (last_verified_at) recently — cheap local-DB
// only, so it runs on every tick unlike the certificate check below.
func (e *AlertEvaluator) checkConnectionStaleness(ctx context.Context, conns []connections.Info) {
	now := time.Now().UTC()
	for _, conn := range conns {
		var lastVerified *string
		if err := e.db.QueryRowContext(ctx, `SELECT last_verified_at FROM connections WHERE id = ?`, conn.ID).Scan(&lastVerified); err != nil {
			continue
		}
		resourceID := conn.ID + "|staleness"
		if lastVerified == nil || *lastVerified == "" {
			// Never successfully verified yet — likely just created; not
			// "stale" in the sense this check cares about.
			e.resolveIfActive(ctx, connectionStaleRuleID, resourceID)
			continue
		}
		verifiedAt, err := time.Parse(time.RFC3339, *lastVerified)
		if err != nil {
			continue
		}
		age := now.Sub(verifiedAt)
		daysStale := age.Hours() / 24
		switch {
		case age >= staleCritical:
			e.upsertSystemAlert(ctx, connectionStaleRuleID, "connection_stale", conn, resourceID, conn.Name, daysStale, staleCritical.Hours()/24, "critical")
		case age >= staleWarn:
			e.upsertSystemAlert(ctx, connectionStaleRuleID, "connection_stale", conn, resourceID, conn.Name, daysStale, staleWarn.Hours()/24, "warning")
		default:
			e.resolveIfActive(ctx, connectionStaleRuleID, resourceID)
		}
	}
}

// checkCertificateExpiry fetches every node's TLS certificates (PVE's
// /nodes/{node}/certificates/info) for every connection and alerts on
// ones nearing expiry. Gated to certCheckInterval since it costs one HTTP
// round trip per node, on top of the per-connection cluster/resources call
// evaluateOnce already made this tick.
//
// PBS certificate monitoring was left out of this round: PBS cert info
// needs its own client work (see task notes) rather than reusing this PVE
// call shape, so only PVE connections are covered here.
func (e *AlertEvaluator) checkCertificateExpiry(ctx context.Context, conns []connections.Info) {
	e.certCheckMu.Lock()
	due := e.lastCertCheck.IsZero() || time.Since(e.lastCertCheck) >= certCheckInterval
	if due {
		e.lastCertCheck = time.Now()
	}
	e.certCheckMu.Unlock()
	if !due {
		return
	}

	for _, conn := range conns {
		client, err := e.conns.ClientFor(ctx, conn.ID)
		if err != nil {
			continue // unreachable connection is already covered by connection_health/staleness
		}
		resources, err := client.ClusterResources(ctx)
		if err != nil {
			continue
		}
		for _, res := range resources {
			if res.Type != "node" || res.Node == "" {
				continue
			}
			certs, err := client.NodeCertificates(ctx, res.Node)
			if err != nil {
				slog.Warn("alert evaluator: fetching node certificates failed", "connectionId", conn.ID, "node", res.Node, "error", err)
				continue
			}
			for _, cert := range certs {
				if cert.NotAfter == 0 {
					continue
				}
				notAfter := time.Unix(cert.NotAfter, 0).UTC()
				daysLeft := time.Until(notAfter).Hours() / 24
				resourceID := fmt.Sprintf("%s|cert|%s|%s", conn.ID, res.Node, cert.Filename)
				name := fmt.Sprintf("%s: %s", res.Node, cert.Filename)
				switch {
				case daysLeft <= certCriticalDays:
					e.upsertSystemAlert(ctx, certExpiryRuleID, "cert_expiry", conn, resourceID, name, daysLeft, certCriticalDays, "critical")
				case daysLeft <= certWarnDays:
					e.upsertSystemAlert(ctx, certExpiryRuleID, "cert_expiry", conn, resourceID, name, daysLeft, certWarnDays, "warning")
				default:
					e.resolveIfActive(ctx, certExpiryRuleID, resourceID)
				}
			}
		}
	}
}

// upsertSystemAlert is upsertActive's counterpart for the built-in rules
// above: same insert-or-update-preserving-silence shape, but severity is
// computed per-instance (closer to expiry = worse) rather than copied from
// a single rule-wide severity, and "value" is a day count where lower is
// worse instead of a percentage where higher is worse.
func (e *AlertEvaluator) upsertSystemAlert(ctx context.Context, ruleID, metric string, conn connections.Info, resourceID, resourceName string, value, threshold float64, severity string) {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID string
	isNew := e.db.QueryRowContext(ctx, `SELECT id FROM alert_instances WHERE rule_id = ? AND resource_id = ?`, ruleID, resourceID).Scan(&existingID) != nil

	if _, err := e.db.ExecContext(ctx, `
		INSERT INTO alert_instances (id, rule_id, connection_id, connection_name, resource_id, resource_name, metric, value, threshold, severity, status, triggered_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
		ON CONFLICT (rule_id, resource_id) DO UPDATE SET
			value = excluded.value,
			threshold = excluded.threshold,
			severity = excluded.severity,
			updated_at = excluded.updated_at,
			status = CASE WHEN alert_instances.status = 'silenced' THEN 'silenced' ELSE 'active' END`,
		uuid.NewString(), ruleID, conn.ID, conn.Name, resourceID, resourceName, metric, value, threshold, severity, now, now,
	); err != nil {
		slog.Error("alert evaluator: persisting built-in alert failed", "rule", ruleID, "error", err)
		return
	}
	if !isNew {
		return
	}
	slog.Warn("built-in alert triggered", "rule", ruleID, "connection", conn.Name, "resource", resourceName, "value", value, "severity", severity)
	if e.notifier == nil {
		return
	}
	title := fmt.Sprintf("[%s] %s", strings.ToUpper(severity), resourceName)
	var message string
	if metric == "cert_expiry" {
		message = fmt.Sprintf("Certificate %q on %s expires in %.0f day(s)", resourceName, conn.Name, value)
	} else {
		message = fmt.Sprintf("Ferrum has not successfully polled %q in %.0f day(s)", conn.Name, value)
	}
	emails := e.optedInEmails(context.Background())
	go func() {
		for _, err := range e.notifier.Notify(context.Background(), title, message, emails...) {
			slog.Error("built-in alert notification failed", "error", err)
		}
	}()
}
