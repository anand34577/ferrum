package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Fleet health score is a single 0-100 rollup per connection, combining
// signals Ferrum already collects elsewhere — no new polling or data
// collection is added here. The formula below is the whole scoring
// contract; a reviewer should be able to sanity-check any score by reading
// it, not by re-deriving it from the code.
//
// Starting from 100, five weighted components are scored independently and
// summed (never combined multiplicatively, so one bad signal can't be
// erased by another looking fine):
//
//   - Reachability (25 pts): from connection_health (poller/alerts.go) —
//     the connection's own periodic up/down check, plus how stale that
//     check is. status="up" and refreshed within the last 5 minutes scores
//     the full 25; "up" but stale (the evaluator may be stuck) scores 15;
//     "down", or no health row at all (evaluator never ticked / connection
//     brand new) scores 0.
//   - Critical alerts (30 pts): -10 points per *active* critical
//     alert_instances row for this connection, floored at 0. Heaviest
//     single-unit penalty of any component — a firing critical alert is
//     the strongest signal something is actually wrong right now.
//   - Warning alerts (15 pts): -3 points per active warning, floored at 0.
//   - Resource health (20 pts): the fraction of this connection's nodes
//     that are not "online", plus non-template guests reporting
//     status="unknown" or an HA state of "error"/"fence" (from
//     cluster/resources, the same call inventory/overview already make),
//     scaled linearly — 0% bad resources scores 20, 100% scores 0.
//   - Backup reliability (10 pts): failure rate among recent vzdump tasks
//     from /cluster/tasks (already exposed via clusterTasks — reusing it
//     rather than adding a new call chain), scaled linearly the same way.
//     No vzdump tasks in the recent window isn't treated as a failure —
//     it scores the full 10, since "no data" and "everything failed" are
//     not the same thing.
//
// Weights: 25 + 30 + 15 + 20 + 10 = 100.
type healthComponent struct {
	Label  string  `json:"label"`
	Points float64 `json:"points"`
	Max    float64 `json:"max"`
}

type healthScoreResult struct {
	ConnectionID   string            `json:"connectionId,omitempty"`
	ConnectionName string            `json:"connectionName,omitempty"`
	Score          int               `json:"score"`
	Components     []healthComponent `json:"components"`
}

// healthScoreInputs is everything computeHealthScore needs, gathered by the
// handler from data Ferrum already has. Kept as a plain struct so the
// scoring math is trivially unit-testable with synthetic values.
type healthScoreInputs struct {
	// ReachabilityKnown is false when there's no connection_health row yet
	// (never polled) — distinct from a row that says "down".
	ReachabilityKnown bool
	ReachabilityUp    bool
	// ReachabilityAge is how long ago the health row was last refreshed.
	ReachabilityAge time.Duration

	CriticalAlerts int
	WarningAlerts  int

	// Resource health: how many of this connection's nodes/guests are
	// counted and how many of those are "bad" (see doc comment above).
	TotalResources int
	BadResources   int

	// Backup reliability: recent vzdump tasks and how many didn't finish OK.
	TotalBackupTasks  int
	FailedBackupTasks int
}

const (
	healthReachabilityMax = 25.0
	healthCriticalMax     = 30.0
	healthWarningMax      = 15.0
	healthResourceMax     = 20.0
	healthBackupMax       = 10.0

	healthReachabilityStaleAfter = 5 * time.Minute

	healthPointsPerCritical = 10.0
	healthPointsPerWarning  = 3.0
)

// computeHealthScore is the pure scoring function — no DB or HTTP here, so
// it's directly unit-testable with synthetic inputs.
func computeHealthScore(in healthScoreInputs) (int, []healthComponent) {
	components := make([]healthComponent, 0, 5)

	reach := 0.0
	switch {
	case in.ReachabilityKnown && in.ReachabilityUp && in.ReachabilityAge <= healthReachabilityStaleAfter:
		reach = healthReachabilityMax
	case in.ReachabilityKnown && in.ReachabilityUp:
		reach = healthReachabilityMax * 0.6 // up, but the check is stale enough to be suspicious
	default:
		reach = 0 // down, or never checked
	}
	components = append(components, healthComponent{"Reachability", round1(reach), healthReachabilityMax})

	critical := healthCriticalMax - float64(in.CriticalAlerts)*healthPointsPerCritical
	if critical < 0 {
		critical = 0
	}
	components = append(components, healthComponent{"Critical alerts", round1(critical), healthCriticalMax})

	warning := healthWarningMax - float64(in.WarningAlerts)*healthPointsPerWarning
	if warning < 0 {
		warning = 0
	}
	components = append(components, healthComponent{"Warning alerts", round1(warning), healthWarningMax})

	resource := healthResourceMax
	if in.TotalResources > 0 {
		badFrac := float64(in.BadResources) / float64(in.TotalResources)
		if badFrac > 1 {
			badFrac = 1
		}
		resource = healthResourceMax * (1 - badFrac)
	}
	components = append(components, healthComponent{"Node/guest status", round1(resource), healthResourceMax})

	backup := healthBackupMax
	if in.TotalBackupTasks > 0 {
		failFrac := float64(in.FailedBackupTasks) / float64(in.TotalBackupTasks)
		if failFrac > 1 {
			failFrac = 1
		}
		backup = healthBackupMax * (1 - failFrac)
	}
	components = append(components, healthComponent{"Backup reliability", round1(backup), healthBackupMax})

	total := 0.0
	for _, c := range components {
		total += c.Points
	}
	score := int(total + 0.5) // round half up; total is always >= 0
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score, components
}

// gatherHealthScoreInputs pulls every signal computeHealthScore needs for
// one connection: its stored reachability row, active alert counts, live
// cluster/resources for node/guest status, and recent vzdump tasks for the
// backup failure rate. A connection that can't currently be reached still
// gets scored — reachability just lands at 0 — rather than the whole
// request failing.
func (s *Server) gatherHealthScoreInputs(ctx context.Context, connID string) healthScoreInputs {
	var in healthScoreInputs

	var status string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT status, updated_at FROM connection_health WHERE connection_id = ?`, connID).Scan(&status, &updatedAt)
	if err == nil {
		in.ReachabilityKnown = true
		in.ReachabilityUp = status == "up"
		if t, perr := time.Parse(time.RFC3339, updatedAt); perr == nil {
			in.ReachabilityAge = time.Since(t)
		} else {
			in.ReachabilityAge = healthReachabilityStaleAfter + time.Hour // parse failure reads as stale
		}
	}

	alertRows, err := s.db.QueryContext(ctx, `
		SELECT severity, COUNT(*) FROM alert_instances
		WHERE connection_id = ? AND status = 'active' GROUP BY severity`, connID)
	if err == nil {
		for alertRows.Next() {
			var severity string
			var n int
			if alertRows.Scan(&severity, &n) == nil {
				if severity == "critical" {
					in.CriticalAlerts += n
				} else {
					in.WarningAlerts += n
				}
			}
		}
		alertRows.Close()
	} else {
		slog.Warn("health score: alert query failed", "connectionId", connID, "error", err)
	}

	client, err := s.clientFor(ctx, connID)
	if err != nil {
		// Unreachable right now: resource/backup signals stay at zero
		// totals (scored as full marks — "no data" isn't "everything
		// failed"), while reachability above already reflects the outage.
		return in
	}

	if resources, err := client.ClusterResources(ctx); err == nil {
		for _, res := range resources {
			switch res.Type {
			case "node":
				in.TotalResources++
				if res.Status != "online" {
					in.BadResources++
				}
			case "qemu", "lxc":
				if res.Template == 1 {
					continue
				}
				in.TotalResources++
				if res.Status == "unknown" || res.HAState == "error" || res.HAState == "fence" {
					in.BadResources++
				}
			}
		}
	} else {
		slog.Warn("health score: cluster/resources failed", "connectionId", connID, "error", err)
	}

	if tasks, err := client.ClusterTasks(ctx, 200); err == nil {
		for _, t := range tasks {
			if t.Type != "vzdump" {
				continue
			}
			tc := t
			tc.Normalize()
			if tc.Status == "" || tc.Status == "running" {
				continue // still in progress, no verdict yet
			}
			in.TotalBackupTasks++
			if tc.Status != "OK" {
				in.FailedBackupTasks++
			}
		}
	} else {
		slog.Warn("health score: cluster/tasks failed", "connectionId", connID, "error", err)
	}

	return in
}

// connectionHealthScore handles GET /connections/{id}/health-score.
func (s *Server) connectionHealthScore(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")

	var name string
	if err := s.db.QueryRowContext(r.Context(), `SELECT name FROM connections WHERE id = ?`, connID).Scan(&name); err != nil {
		writeErrorMsg(w, http.StatusNotFound, "connection not found")
		return
	}

	in := s.gatherHealthScoreInputs(r.Context(), connID)
	score, components := computeHealthScore(in)
	writeJSON(w, http.StatusOK, healthScoreResult{
		ConnectionID:   connID,
		ConnectionName: name,
		Score:          score,
		Components:     components,
	})
}

// fleetHealthScore handles GET /api/v1/health-score — the fleet-wide
// average and the single worst-scoring connection, alongside every
// connection's own score for a dashboard widget to render directly.
func (s *Server) fleetHealthScore(w http.ResponseWriter, r *http.Request) {
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

	if len(conns) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"score": 100, "connections": []healthScoreResult{}})
		return
	}

	out := make([]healthScoreResult, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			in := s.gatherHealthScoreInputs(r.Context(), c.ID)
			score, components := computeHealthScore(in)
			out[i] = healthScoreResult{ConnectionID: c.ID, ConnectionName: c.Name, Score: score, Components: components}
		}(i, c)
	}
	wg.Wait()

	sum := 0
	worstIdx := 0
	for i, r := range out {
		sum += r.Score
		if r.Score < out[worstIdx].Score {
			worstIdx = i
		}
	}
	fleetScore := int(float64(sum)/float64(len(out)) + 0.5)

	sorted := make([]healthScoreResult, len(out))
	copy(sorted, out)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score < sorted[j].Score })

	writeJSON(w, http.StatusOK, map[string]any{
		"score":       fleetScore,
		"worst":       out[worstIdx],
		"connections": sorted,
	})
}
