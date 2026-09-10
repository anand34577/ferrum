package api

import (
	"testing"
	"time"
)

func TestComputeHealthScoreAllHealthy(t *testing.T) {
	in := healthScoreInputs{
		ReachabilityKnown: true,
		ReachabilityUp:    true,
		ReachabilityAge:   30 * time.Second,
		CriticalAlerts:    0,
		WarningAlerts:     0,
		TotalResources:    20,
		BadResources:      0,
		TotalBackupTasks:  10,
		FailedBackupTasks: 0,
	}
	score, components := computeHealthScore(in)
	if score < 95 {
		t.Errorf("score = %d, want ~100 for an all-healthy connection", score)
	}
	if score > 100 {
		t.Fatalf("score = %d, must never exceed 100", score)
	}
	var sum float64
	for _, c := range components {
		sum += c.Max
	}
	if sum != 100 {
		t.Errorf("component maxes sum to %v, want 100", sum)
	}
}

func TestComputeHealthScoreEverythingCritical(t *testing.T) {
	in := healthScoreInputs{
		ReachabilityKnown: true,
		ReachabilityUp:    false, // down
		CriticalAlerts:    10,    // way past the floor
		WarningAlerts:     20,
		TotalResources:    10,
		BadResources:      10, // 100% bad
		TotalBackupTasks:  10,
		FailedBackupTasks: 10, // 100% failed
	}
	score, _ := computeHealthScore(in)
	if score != 0 {
		t.Errorf("score = %d, want 0 when every signal is maximally bad", score)
	}
}

func TestComputeHealthScoreMixed(t *testing.T) {
	in := healthScoreInputs{
		ReachabilityKnown: true,
		ReachabilityUp:    true,
		ReachabilityAge:   30 * time.Second,
		CriticalAlerts:    1, // -10
		WarningAlerts:     2, // -6
		TotalResources:    10,
		BadResources:      1, // 10% bad -> 18/20
		TotalBackupTasks:  10,
		FailedBackupTasks: 1, // 10% failed -> 9/10
	}
	score, _ := computeHealthScore(in)
	// 25 (reach) + 20 (30-10 critical) + 9 (15-6 warning) + 18 + 9 = 81
	if score < 60 || score > 95 {
		t.Errorf("score = %d, want a sensible mid-range value for a lightly-degraded connection, got components math suggesting ~81", score)
	}
}

func TestComputeHealthScoreUnknownReachabilityScoresZeroForThatComponent(t *testing.T) {
	in := healthScoreInputs{ReachabilityKnown: false}
	_, components := computeHealthScore(in)
	for _, c := range components {
		if c.Label == "Reachability" {
			if c.Points != 0 {
				t.Errorf("Reachability points = %v, want 0 when never checked", c.Points)
			}
			return
		}
	}
	t.Fatal("no Reachability component returned")
}

func TestComputeHealthScoreStaleReachabilityPartialCredit(t *testing.T) {
	fresh := healthScoreInputs{ReachabilityKnown: true, ReachabilityUp: true, ReachabilityAge: time.Minute}
	stale := healthScoreInputs{ReachabilityKnown: true, ReachabilityUp: true, ReachabilityAge: time.Hour}

	_, freshComponents := computeHealthScore(fresh)
	_, staleComponents := computeHealthScore(stale)

	freshPts := componentPoints(freshComponents, "Reachability")
	stalePts := componentPoints(staleComponents, "Reachability")

	if !(stalePts > 0 && stalePts < freshPts) {
		t.Errorf("stale reachability points = %v, fresh = %v — want stale strictly between 0 and fresh", stalePts, freshPts)
	}
}

func TestComputeHealthScoreNeverNegativeOrOverflowing(t *testing.T) {
	in := healthScoreInputs{
		CriticalAlerts: 1000,
		WarningAlerts:  1000,
		TotalResources: 5,
		BadResources:   50, // more "bad" than total — must clamp, not go negative
	}
	score, components := computeHealthScore(in)
	if score < 0 || score > 100 {
		t.Fatalf("score = %d, must stay within [0,100]", score)
	}
	for _, c := range components {
		if c.Points < 0 || c.Points > c.Max {
			t.Errorf("component %q points = %v out of range [0,%v]", c.Label, c.Points, c.Max)
		}
	}
}

func TestComputeHealthScoreNoBackupOrResourceDataIsNotPenalized(t *testing.T) {
	// Zero totals ("we don't know") must not be scored the same as "100%
	// bad" — that would be indistinguishable from a genuinely broken
	// connection and would tank the score for reasons unrelated to health.
	in := healthScoreInputs{
		ReachabilityKnown: true,
		ReachabilityUp:    true,
		ReachabilityAge:   time.Second,
	}
	score, _ := computeHealthScore(in)
	if score < 95 {
		t.Errorf("score = %d, want ~100 when there's simply no resource/backup data yet", score)
	}
}

func componentPoints(components []healthComponent, label string) float64 {
	for _, c := range components {
		if c.Label == label {
			return c.Points
		}
	}
	return -1
}
