package api

import (
	"math"
	"testing"
	"time"

	"ferrum/internal/pve"
)

// genMemPoints builds a synthetic node RRD series for the "mem" metric:
// one sample per day starting at startPct, moving by dailyDeltaPct% per
// day (plus optional noise), against a fixed maxMem.
func genMemPoints(days int, startPct, dailyDeltaPct float64, noise []float64) []pve.RRDPoint {
	const maxMem = int64(1_000_000_000) // 1 GB, arbitrary
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	points := make([]pve.RRDPoint, days)
	for i := 0; i < days; i++ {
		pct := startPct + dailyDeltaPct*float64(i)
		if noise != nil {
			pct += noise[i%len(noise)]
		}
		if pct < 0 {
			pct = 0
		}
		used := int64(pct / 100 * float64(maxMem))
		points[i] = pve.RRDPoint{
			Time:   base + int64(i)*86400,
			Mem:    used,
			MaxMem: maxMem,
		}
	}
	return points
}

func TestBuildForecastRisingLinearly(t *testing.T) {
	// 30 days, climbing from 50% to 50%+29*1.5% ≈ 93.5% at 1.5%/day.
	points := genMemPoints(30, 50, 1.5, nil)
	now := time.Unix(points[len(points)-1].Time, 0).UTC()

	result, err := buildForecast(points, "mem", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "rising" {
		t.Fatalf("Trend = %q, want rising", result.Trend)
	}
	if result.Confidence != "high" {
		t.Errorf("Confidence = %q, want high for a clean linear series with 30 points", result.Confidence)
	}
	if result.RSquared < 0.99 {
		t.Errorf("RSquared = %v, want ~1.0 for a perfectly linear series", result.RSquared)
	}
	if result.DaysToWarning == nil {
		t.Fatal("DaysToWarning is nil, want a projection since the series already crossed 90%")
	}
	// Current pct (last sample, day 29) is 50 + 29*1.5 = 93.5, already past
	// 90 — so days-to-warning should be ~0, not some far-future number.
	if *result.DaysToWarning != 0 {
		t.Errorf("DaysToWarning = %v, want 0 (already past 90%%)", *result.DaysToWarning)
	}
	if result.ProjectedDate90 == nil {
		t.Error("ProjectedDate90 is nil, want a timestamp")
	}
	if result.DaysToCritical == nil {
		t.Fatal("DaysToCritical is nil, want a projection")
	}
	// 100% is reached (100-50)/1.5 ≈ 33.3 days from day 0; day 29 is "now",
	// so about 4.3 more days.
	if *result.DaysToCritical < 3 || *result.DaysToCritical > 6 {
		t.Errorf("DaysToCritical = %v, want roughly 4.3", *result.DaysToCritical)
	}
}

func TestBuildForecastFlat(t *testing.T) {
	points := genMemPoints(30, 40, 0, nil) // constant 40% for 30 days
	now := time.Unix(points[len(points)-1].Time, 0).UTC()

	result, err := buildForecast(points, "mem", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "flat" {
		t.Fatalf("Trend = %q, want flat", result.Trend)
	}
	if result.DaysToWarning != nil || result.DaysToCritical != nil {
		t.Error("a flat trend must not report projected days to any threshold")
	}
	if result.ProjectedDate90 != nil || result.ProjectedDate100 != nil {
		t.Error("a flat trend must not report a projected crossing date")
	}
}

func TestBuildForecastFalling(t *testing.T) {
	points := genMemPoints(30, 80, -1.0, nil) // draining from 80% down
	now := time.Unix(points[len(points)-1].Time, 0).UTC()

	result, err := buildForecast(points, "mem", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "falling" {
		t.Fatalf("Trend = %q, want falling", result.Trend)
	}
	if result.DaysToWarning != nil || result.DaysToCritical != nil {
		t.Error("a falling trend must never report a nonsense future exhaustion date")
	}
}

func TestBuildForecastInsufficientData(t *testing.T) {
	// Only 5 points (< forecastMinPoints) even though they'd otherwise look
	// like a clean, sharply rising line — must not be trusted.
	points := genMemPoints(5, 10, 20, nil)
	now := time.Unix(points[len(points)-1].Time, 0).UTC()

	result, err := buildForecast(points, "mem", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "flat" {
		t.Errorf("Trend = %q, want flat when history is too sparse to trust", result.Trend)
	}
	if result.Confidence != "low" {
		t.Errorf("Confidence = %q, want low with only %d points", result.Confidence, len(points))
	}
	if result.DaysToWarning != nil || result.DaysToCritical != nil || result.ProjectedDate90 != nil || result.ProjectedDate100 != nil {
		t.Error("insufficient data must never produce a projected date")
	}
}

func TestBuildForecastNoisyButRising(t *testing.T) {
	// A genuine upward trend (1%/day) with meaningful day-to-day noise
	// layered on top — the fit should still detect "rising" but confidence
	// should reflect the noise, not report "high" as if it were a clean line.
	noise := []float64{18, -22, 14, -9, 25, -19, 11, -16, 21, -24}
	points := genMemPoints(40, 30, 1.0, noise)
	now := time.Unix(points[len(points)-1].Time, 0).UTC()

	result, err := buildForecast(points, "mem", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "rising" {
		t.Fatalf("Trend = %q, want rising (the underlying signal is a genuine upward trend)", result.Trend)
	}
	if result.Confidence == "high" {
		t.Errorf("Confidence = high, want low/medium — the series has meaningful noise, R²=%v", result.RSquared)
	}
	if result.RSquared >= 0.99 {
		t.Errorf("RSquared = %v suspiciously high for a noisy series", result.RSquared)
	}
}

func TestBuildForecastUnsupportedMetric(t *testing.T) {
	if _, err := buildForecast(nil, "bogus", time.Now()); err == nil {
		t.Fatal("want an error for an unsupported metric")
	}
}

func TestBuildForecastEmptySeries(t *testing.T) {
	result, err := buildForecast(nil, "disk", time.Now())
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if result.Trend != "flat" || result.Confidence != "low" {
		t.Errorf("empty series should report flat/low, got trend=%q confidence=%q", result.Trend, result.Confidence)
	}
	if result.CurrentPct != 0 {
		t.Errorf("CurrentPct = %v, want 0 for no data", result.CurrentPct)
	}
}

func TestBuildForecastCPUMetricUsesFractionDirectly(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	points := make([]pve.RRDPoint, 10)
	for i := range points {
		points[i] = pve.RRDPoint{Time: base + int64(i)*86400, CPU: 0.5} // 50% flat
	}
	now := time.Unix(points[len(points)-1].Time, 0).UTC()
	result, err := buildForecast(points, "cpu", now)
	if err != nil {
		t.Fatalf("buildForecast: %v", err)
	}
	if math.Abs(result.CurrentPct-50) > 0.001 {
		t.Errorf("CurrentPct = %v, want 50 (cpu fraction * 100)", result.CurrentPct)
	}
}

func TestLinearRegressionDivByZeroGuards(t *testing.T) {
	// All identical x values — must not panic or divide by zero.
	if _, _, _, ok := linearRegression([]float64{5, 5, 5}, []float64{1, 2, 3}); ok {
		t.Error("want ok=false when every x is identical (can't fit a line)")
	}
	// Too few points.
	if _, _, _, ok := linearRegression([]float64{1}, []float64{1}); ok {
		t.Error("want ok=false with a single point")
	}
	// Mismatched lengths.
	if _, _, _, ok := linearRegression([]float64{1, 2}, []float64{1}); ok {
		t.Error("want ok=false with mismatched slice lengths")
	}
}

func TestLinearRegressionPerfectFit(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{10, 12, 14, 16, 18} // y = 2x + 10
	slope, intercept, r2, ok := linearRegression(xs, ys)
	if !ok {
		t.Fatal("want ok=true")
	}
	if math.Abs(slope-2) > 1e-9 {
		t.Errorf("slope = %v, want 2", slope)
	}
	if math.Abs(intercept-10) > 1e-9 {
		t.Errorf("intercept = %v, want 10", intercept)
	}
	if math.Abs(r2-1) > 1e-9 {
		t.Errorf("r2 = %v, want 1 for a perfect fit", r2)
	}
}

func TestDaysUntilThresholdAlreadyPast(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	days, date, ok := daysUntilThreshold(1.0, 0, 95, 90, 10, now)
	if !ok {
		t.Fatal("want ok=true")
	}
	if days != 0 {
		t.Errorf("days = %v, want 0 (already past threshold)", days)
	}
	if date != now.UTC().Format(time.RFC3339) {
		t.Errorf("date = %q, want now formatted as RFC3339", date)
	}
}

func TestDaysUntilThresholdNonPositiveSlope(t *testing.T) {
	now := time.Now()
	if _, _, ok := daysUntilThreshold(0, 50, 50, 90, 0, now); ok {
		t.Error("want ok=false for a zero slope (never reaches threshold)")
	}
	if _, _, ok := daysUntilThreshold(-0.5, 50, 50, 90, 0, now); ok {
		t.Error("want ok=false for a negative slope (draining, never reaches threshold)")
	}
}
