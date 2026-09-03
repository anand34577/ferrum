package pve

import (
	"encoding/json"
	"testing"
)

// PVE rrddata rows: gaps decode as nulls, node rows carry swap/iowait/PSI,
// and newer guest schemas add per-device columns we must preserve in Extra.
func TestDecodeRRDPoints(t *testing.T) {
	raw := `[
		{"time":1700000000,"cpu":0.5,"maxcpu":8,"mem":1000000,"maxmem":4000000,"netin":12.5,"netout":3.25,"loadavg":0.42,"iowait":0.07,"swap":null,"pressurecpusome":0.25,"nics_net0":77.5,"blockstat_scsi0":19.75},
		{"time":1700000060,"cpu":null,"mem":null,"netin":null,"netout":null}
	]`

	var rows []map[string]any
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	points := decodeRRDPoints(rows)
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}

	p := points[0]
	if p.Time != 1700000000 {
		t.Errorf("time = %d", p.Time)
	}
	if p.CPU != 0.5 || p.MaxCPU != 8 {
		t.Errorf("cpu fields = %v/%v", p.CPU, p.MaxCPU)
	}
	if p.Mem != 1000000 || p.MaxMem != 4000000 {
		t.Errorf("mem fields = %v/%v", p.Mem, p.MaxMem)
	}
	if p.NetIn != 12.5 || p.NetOut != 3.25 {
		t.Errorf("net rates = %v/%v", p.NetIn, p.NetOut)
	}
	if p.LoadAvg != 0.42 {
		t.Errorf("loadavg = %v", p.LoadAvg)
	}
	if p.IOWait != 0.07 {
		t.Errorf("iowait = %v", p.IOWait)
	}
	if p.Swap != 0 {
		t.Errorf("null swap should decode as absent, got %v", p.Swap)
	}
	if p.PressureCPUSome != 0.25 {
		t.Errorf("pressurecpusome = %v", p.PressureCPUSome)
	}
	if p.Extra["nics_net0"] != 77.5 || p.Extra["blockstat_scsi0"] != 19.75 {
		t.Errorf("per-device extra = %v", p.Extra)
	}
	if _, ok := p.Extra["cpu"]; ok {
		t.Errorf("known column leaked into Extra: %v", p.Extra)
	}

	// An all-null row must decode without error and stay empty.
	gap := points[1]
	if gap.CPU != 0 || gap.Mem != 0 || len(gap.Extra) != 0 {
		t.Errorf("gap row decoded nonzero: %+v", gap)
	}
}

func TestDecodeRRDPointsEmpty(t *testing.T) {
	if got := decodeRRDPoints(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
