package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

// Capacity forecasting projects when a node's disk/memory/CPU usage will
// cross 90% ("warning") and 100% ("critical") capacity, by fitting an
// ordinary-least-squares line through its historical RRD samples (the same
// time series /rrddata already exposes — no new data collection). This is
// deliberately a simple linear trend, not a seasonal or exponential model:
// good enough to flag "this disk fills up in 12 days at the current rate"
// without pretending to more precision than a handful of daily samples
// support.

// forecastMinPoints is the fewest distinct RRD samples needed before a
// trend line means anything. Below this, two or three points can produce
// a "perfect" but meaningless fit (R²=1 from a coin flip's worth of data).
const forecastMinPoints = 7

// forecastFlatSlopeEpsilon is the minimum slope magnitude (percentage
// points of capacity per day) treated as an actual trend rather than noise
// around a flat line. Below this, small measurement jitter would otherwise
// flip a stable metric between "rising" and "falling" run to run.
const forecastFlatSlopeEpsilon = 0.02

// ForecastResult is the response shape for both the single-node forecast
// endpoint and each row of the fleet-wide capacity-warnings rollup.
type ForecastResult struct {
	Metric           string   `json:"metric"`
	CurrentPct       float64  `json:"currentPct"`
	Trend            string   `json:"trend"` // "rising" | "falling" | "flat"
	DaysToWarning    *float64 `json:"daysToWarning,omitempty"`
	DaysToCritical   *float64 `json:"daysToCritical,omitempty"`
	ProjectedDate90  *string  `json:"projectedDate90,omitempty"`
	ProjectedDate100 *string  `json:"projectedDate100,omitempty"`
	Confidence       string   `json:"confidence"` // "low" | "medium" | "high"

	// SampleSize and RSquared aren't required by the spec but are cheap to
	// return and let a caller sanity-check *why* confidence landed where it
	// did, instead of trusting an opaque label.
	SampleSize int     `json:"sampleSize"`
	RSquared   float64 `json:"rSquared"`
}

// forecastMetricPct extracts one RRD sample's used/max ratio (0..100) for
// the requested metric. ok is false when the sample doesn't carry usable
// data for that metric (an RRD gap before the node/guest existed decodes
// as zero-valued fields indistinguishable from a real zero, so mem/disk
// samples are only trusted when their max is reported > 0; cpu has no such
// gap — it's already a 0..1 fraction of allocated cores with no natural
// "missing" reading — so it should not be aggressively filtered the same way, every sample is used).
func forecastMetricPct(p pve.RRDPoint, metric string) (float64, bool) {
	switch metric {
	case "mem":
		if p.MaxMem <= 0 {
			return 0, false
		}
		return float64(p.Mem) / float64(p.MaxMem) * 100, true
	case "disk":
		if p.MaxDisk <= 0 {
			return 0, false
		}
		return float64(p.Disk) / float64(p.MaxDisk) * 100, true
	case "cpu":
		return p.CPU * 100, true
	default:
		return 0, false
	}
}

// linearRegression fits y = slope*x + intercept over the given points by
// ordinary least squares (stdlib math only), and reports R² (coefficient
// of determination) as a measure of how well that line actually explains
// the data — a rising trend fit through wildly noisy samples should not
// be reported with the same confidence as a clean one.
func linearRegression(xs, ys []float64) (slope, intercept, r2 float64, ok bool) {
	n := float64(len(xs))
	if len(xs) < 2 || len(xs) != len(ys) {
		return 0, 0, 0, false
	}
	var sumX, sumY, sumXY, sumXX float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumXX += xs[i] * xs[i]
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		// Every x identical — can't fit a line (shouldn't happen given the
		// distinct-timestamp check upstream, but guard div-by-zero anyway).
		return 0, 0, 0, false
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	meanY := sumY / n
	var ssRes, ssTot float64
	for i := range xs {
		pred := slope*xs[i] + intercept
		diffRes := ys[i] - pred
		ssRes += diffRes * diffRes
		diffTot := ys[i] - meanY
		ssTot += diffTot * diffTot
	}
	if ssTot == 0 {
		// Every y identical (a perfectly flat metric) — the "fit" is exact
		// by definition, not evidence of anything meaningful either way.
		r2 = 1
	} else {
		r2 = 1 - ssRes/ssTot
	}
	return slope, intercept, r2, true
}

// forecastConfidence turns sample size + fit quality into the low/medium/
// high label the API returns. Thresholds are deliberately conservative:
// a handful of noisy points should never read as "high confidence" just
// because the math produced a number.
func forecastConfidence(n int, r2 float64) string {
	switch {
	case n >= 30 && r2 >= 0.6:
		return "high"
	case n >= 14 && r2 >= 0.25:
		return "medium"
	default:
		return "low"
	}
}

// buildForecast is the pure math core: given a metric's RRD history, fit a
// trend and project when it crosses 90%/100%. Kept independent of any PVE
// client or HTTP plumbing so it can be unit tested with synthetic point
// series.
func buildForecast(points []pve.RRDPoint, metric string, now time.Time) (*ForecastResult, error) {
	if metric != "disk" && metric != "mem" && metric != "cpu" {
		return nil, fmt.Errorf("unsupported metric %q (want disk, mem, or cpu)", metric)
	}

	type sample struct{ t, y float64 } // t = days since the first usable sample; y = used/max pct (0..100)
	var samples []sample
	var firstTime, lastTime int64
	haveFirst := false
	distinctTimes := map[int64]bool{}

	for _, p := range points {
		if p.Time <= 0 {
			continue
		}
		y, ok := forecastMetricPct(p, metric)
		if !ok {
			continue
		}
		if !haveFirst {
			firstTime = p.Time
			haveFirst = true
		}
		lastTime = p.Time
		distinctTimes[p.Time] = true
		samples = append(samples, sample{t: float64(p.Time-firstTime) / 86400.0, y: y})
	}

	result := &ForecastResult{Metric: metric, SampleSize: len(samples)}
	if len(samples) > 0 {
		result.CurrentPct = round2(samples[len(samples)-1].y)
	}

	// Not enough history for a trend line to mean anything — report flat
	// with low confidence rather than a number built on noise.
	if len(distinctTimes) < forecastMinPoints {
		result.Trend = "flat"
		result.Confidence = "low"
		return result, nil
	}

	xs := make([]float64, len(samples))
	ys := make([]float64, len(samples))
	for i, s := range samples {
		xs[i] = s.t
		ys[i] = s.y
	}

	slope, intercept, r2, ok := linearRegression(xs, ys)
	if !ok {
		result.Trend = "flat"
		result.Confidence = "low"
		return result, nil
	}
	result.RSquared = round4(r2)
	result.Confidence = forecastConfidence(len(samples), r2)

	switch {
	case slope > forecastFlatSlopeEpsilon:
		result.Trend = "rising"
	case slope < -forecastFlatSlopeEpsilon:
		result.Trend = "falling"
	default:
		result.Trend = "flat"
	}

	// Projected crossing dates only make sense for a genuinely rising
	// trend — a flat or falling line never reaches capacity from where it
	// is now, so returning a "projected date" there would be a nonsense
	// number wearing the clothes of a real one.
	if result.Trend == "rising" {
		nowT := float64(lastTime-firstTime) / 86400.0
		if days, date, ok := daysUntilThreshold(slope, intercept, result.CurrentPct, 90, nowT, now); ok {
			result.DaysToWarning = &days
			result.ProjectedDate90 = &date
		}
		if days, date, ok := daysUntilThreshold(slope, intercept, result.CurrentPct, 100, nowT, now); ok {
			result.DaysToCritical = &days
			result.ProjectedDate100 = &date
		}
	}

	return result, nil
}

// daysUntilThreshold projects how many days from now (relative to the last
// observed sample) the fitted line reaches the given capacity percentage.
func daysUntilThreshold(slope, intercept, currentPct, threshold, nowT float64, now time.Time) (days float64, projectedDate string, ok bool) {
	if slope <= 0 {
		return 0, "", false
	}
	if currentPct >= threshold {
		// Already there (or past it) as of the latest sample.
		return 0, now.UTC().Format(time.RFC3339), true
	}
	tThreshold := (threshold - intercept) / slope
	d := tThreshold - nowT
	if d < 0 {
		d = 0
	}
	projected := now.Add(time.Duration(d * 24 * float64(time.Hour)))
	return round1(d), projected.UTC().Format(time.RFC3339), true
}

func round1(f float64) float64 { return float64(int64(f*10+sign(f)*0.5)) / 10 }
func round2(f float64) float64 { return float64(int64(f*100+sign(f)*0.5)) / 100 }
func round4(f float64) float64 { return float64(int64(f*10000+sign(f)*0.5)) / 10000 }
func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// forecastMinRRDPoints picks between PVE's "year" and "month" RRD windows:
// year gives more history (better trend confidence) but PVE can return a
// sparser step size for a freshly created node, so fall back to whichever
// window actually has more usable samples rather than assuming year always
// wins.
func (s *Server) forecastRRDPoints(ctx context.Context, client *pve.Client, node string) ([]pve.RRDPoint, error) {
	yearPoints, err := client.NodeRRDData(ctx, node, pve.RRDYear, pve.RRDAverage)
	if err != nil {
		return nil, err
	}
	if len(yearPoints) >= forecastMinPoints {
		return yearPoints, nil
	}
	monthPoints, err := client.NodeRRDData(ctx, node, pve.RRDMonth, pve.RRDAverage)
	if err != nil {
		// Year data is still usable even if sparse; don't fail the whole
		// request just because the fallback window errored.
		return yearPoints, nil
	}
	if len(monthPoints) > len(yearPoints) {
		return monthPoints, nil
	}
	return yearPoints, nil
}

// nodeForecast handles GET /connections/{id}/nodes/{node}/forecast?metric=disk|mem|cpu.
func (s *Server) nodeForecast(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "disk"
	}

	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	node := chi.URLParam(r, "node")

	points, err := s.forecastRRDPoints(r.Context(), client, node)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	result, err := buildForecast(points, metric, time.Now())
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// capacityWarning is one row of the fleet-wide rollup: a node whose
// forecast says it's actually trending toward exhaustion soon.
type capacityWarning struct {
	ConnectionID   string   `json:"connectionId"`
	ConnectionName string   `json:"connectionName"`
	Node           string   `json:"node"`
	Metric         string   `json:"metric"`
	CurrentPct     float64  `json:"currentPct"`
	Trend          string   `json:"trend"`
	DaysToWarning  *float64 `json:"daysToWarning,omitempty"`
	DaysToCritical *float64 `json:"daysToCritical,omitempty"`
	Confidence     string   `json:"confidence"`
}

// defaultForecastHorizonDays bounds the fleet-wide capacity-warnings
// rollup to nodes projected to cross 90% within this many days by default.
const defaultForecastHorizonDays = 30

// forecastMetrics are the metrics checked per node for the fleet rollup.
var forecastMetrics = []string{"disk", "mem", "cpu"}

// fleetCapacityWarnings handles GET /api/v1/forecast/capacity-warnings —
// fans out across every connection's nodes (mirroring the buildFleetEntry
// concurrency pattern in overview.go) and returns only the nodes actually
// trending toward exhaustion within the horizon, soonest first.
func (s *Server) fleetCapacityWarnings(w http.ResponseWriter, r *http.Request) {
	horizon := defaultForecastHorizonDays
	if v := r.URL.Query().Get("horizonDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			horizon = n
		}
	}

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

	var mu sync.Mutex
	var out []capacityWarning
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for _, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			warnings := s.connectionCapacityWarnings(r.Context(), c.ID, c.Name, horizon)
			if len(warnings) == 0 {
				return
			}
			mu.Lock()
			out = append(out, warnings...)
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		return forecastSortKey(out[i]) < forecastSortKey(out[j])
	})

	writeJSON(w, http.StatusOK, out)
}

// forecastSortKey is the days-to-warning used to sort the fleet rollup
// soonest-first; every row in it was filtered to have DaysToWarning set.
func forecastSortKey(c capacityWarning) float64 {
	if c.DaysToWarning == nil {
		return 1e18
	}
	return *c.DaysToWarning
}

// connectionCapacityWarnings checks every online node of one connection
// across disk/mem/cpu and returns the ones trending toward 90% within
// horizonDays. An unreachable connection contributes nothing rather than
// failing the whole fleet rollup — same posture as inventory/overview.
func (s *Server) connectionCapacityWarnings(ctx context.Context, id, name string, horizonDays int) []capacityWarning {
	client, err := s.clientFor(ctx, id)
	if err != nil {
		slog.Warn("capacity forecast: connection unreachable", "connectionId", id, "name", name, "error", err)
		return nil
	}
	nodes, err := client.Nodes(ctx)
	if err != nil {
		slog.Warn("capacity forecast: nodes list failed", "connectionId", id, "name", name, "error", err)
		return nil
	}

	var out []capacityWarning
	for _, node := range nodes {
		if node.Status != "online" {
			continue
		}
		points, err := s.forecastRRDPoints(ctx, client, node.Node)
		if err != nil {
			slog.Warn("capacity forecast: rrd fetch failed", "connectionId", id, "node", node.Node, "error", err)
			continue
		}
		for _, metric := range forecastMetrics {
			result, err := buildForecast(points, metric, time.Now())
			if err != nil || result.Trend != "rising" || result.DaysToWarning == nil {
				continue
			}
			if *result.DaysToWarning > float64(horizonDays) {
				continue
			}
			out = append(out, capacityWarning{
				ConnectionID:   id,
				ConnectionName: name,
				Node:           node.Node,
				Metric:         metric,
				CurrentPct:     result.CurrentPct,
				Trend:          result.Trend,
				DaysToWarning:  result.DaysToWarning,
				DaysToCritical: result.DaysToCritical,
				Confidence:     result.Confidence,
			})
		}
	}
	return out
}
