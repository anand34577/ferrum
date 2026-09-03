package pve

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RRDPoint is one sample from Proxmox's built-in RRD time-series store —
// every node and guest already tracks these locally, so historical charts
// need no time-series storage of our own.
//
// PVE exposes far more columns than the handful this UI originally parsed:
// nodes also carry swap/iowait/loadavg and (PVE 8.2+) kernel PSI pressure
// metrics, and guests expose per-NIC ("nics_<iface>") and per-block-device
// ("blockstat_<drive>") breakdowns on newer schemas. Anything outside the
// typed fields below lands in Extra verbatim so the UI can chart it without
// a backend change when PVE adds columns.
type RRDPoint struct {
	Time int64 `json:"time"`

	// Common (node + guest): CPU is a 0..1 fraction of allocated cores.
	CPU       float64 `json:"cpu,omitempty"`
	MaxCPU    float64 `json:"maxcpu,omitempty"`
	Mem       int64   `json:"mem,omitempty"`
	MaxMem    int64   `json:"maxmem,omitempty"`
	Disk      int64   `json:"disk,omitempty"`
	MaxDisk   int64   `json:"maxdisk,omitempty"`
	NetIn     float64 `json:"netin,omitempty"`     // bytes/sec rate (PVE stores rates in RRD)
	NetOut    float64 `json:"netout,omitempty"`    // bytes/sec rate
	DiskRead  float64 `json:"diskread,omitempty"`  // bytes/sec rate
	DiskWrite float64 `json:"diskwrite,omitempty"` // bytes/sec rate

	// Node-only.
	Swap    float64 `json:"swap,omitempty"`    // swap used, bytes
	MaxSwap float64 `json:"maxswap,omitempty"` // swap total, bytes
	IOWait  float64 `json:"iowait,omitempty"`  // 0..1 fraction of CPU time spent waiting on IO
	LoadAvg float64 `json:"loadavg,omitempty"` // 1-minute load average

	// Kernel PSI pressure (PVE 8.2+), stored as 0..100 percentages. Empty on
	// older PVE releases — the UI hides these charts when absent.
	PressureCPUSome    float64 `json:"pressurecpusome,omitempty"`
	PressureIOSome     float64 `json:"pressureiosome,omitempty"`
	PressureIOFull     float64 `json:"pressureiofull,omitempty"`
	PressureMemorySome float64 `json:"pressurememorysome,omitempty"`
	PressureMemoryFull float64 `json:"pressurememoryfull,omitempty"`

	// Per-device guest stats and any other column PVE adds later, keyed by
	// the raw RRD column name (e.g. "nics_net0" → bytes/sec).
	Extra map[string]float64 `json:"extra,omitempty"`
}

// rrdKnownKeys maps the JSON column names we decode into typed fields. The
// per-point decode keeps everything else in Extra.
var rrdKnownKeys = map[string]bool{
	"time": true, "cpu": true, "maxcpu": true, "mem": true, "maxmem": true,
	"disk": true, "maxdisk": true, "netin": true, "netout": true,
	"diskread": true, "diskwrite": true, "swap": true, "maxswap": true,
	"iowait": true, "loadavg": true,
	"pressurecpusome": true, "pressureiosome": true, "pressureiofull": true,
	"pressurememorysome": true, "pressurememoryfull": true,
}

// nodeRRDAliases remaps node-only RRD column names onto the same typed
// fields guest RRD already uses for the identical stat. PVE's node rrddata
// uses a different schema than guest (qemu/lxc) rrddata: memtotal/memused
// instead of maxmem/mem, swaptotal/swapused instead of maxswap/swap, and
// roottotal/rootused (the root filesystem) instead of maxdisk/disk. Without
// this remap every node-level memory/swap/disk chart silently decodes to
// zero — the "mem"/"maxmem" columns it's looking for simply don't exist on
// a node row, they only exist on guest rows.
var nodeRRDAliases = map[string]string{
	"memused":   "mem",
	"memtotal":  "maxmem",
	"swapused":  "swap",
	"swaptotal": "maxswap",
	"rootused":  "disk",
	"roottotal": "maxdisk",
}

// decodeRRDPoints converts the generic per-sample maps PVE returns into
// RRDPoints, splitting known columns from per-device/extra ones. Nulls
// (RRD gaps for samples before a guest existed) decode as absent fields.
// forNode selects the node RRD column aliases above; guest rows already
// match the typed fields directly and need no remapping.
func decodeRRDPoints(rows []map[string]any, forNode bool) []RRDPoint {
	if len(rows) == 0 {
		return nil
	}
	out := make([]RRDPoint, 0, len(rows))
	for _, row := range rows {
		var p RRDPoint
		var extra map[string]float64
		for k, v := range row {
			f, ok := toFloat(v)
			if !ok {
				continue
			}
			if forNode {
				if alias, ok := nodeRRDAliases[k]; ok {
					k = alias
				}
			}
			switch k {
			case "time":
				p.Time = int64(f)
			case "cpu":
				p.CPU = f
			case "maxcpu":
				p.MaxCPU = f
			case "mem":
				p.Mem = int64(f)
			case "maxmem":
				p.MaxMem = int64(f)
			case "disk":
				p.Disk = int64(f)
			case "maxdisk":
				p.MaxDisk = int64(f)
			case "netin":
				p.NetIn = f
			case "netout":
				p.NetOut = f
			case "diskread":
				p.DiskRead = f
			case "diskwrite":
				p.DiskWrite = f
			case "swap":
				p.Swap = f
			case "maxswap":
				p.MaxSwap = f
			case "iowait":
				p.IOWait = f
			case "loadavg":
				p.LoadAvg = f
			case "pressurecpusome":
				p.PressureCPUSome = f
			case "pressureiosome":
				p.PressureIOSome = f
			case "pressureiofull":
				p.PressureIOFull = f
			case "pressurememorysome":
				p.PressureMemorySome = f
			case "pressurememoryfull":
				p.PressureMemoryFull = f
			default:
				if extra == nil {
					extra = make(map[string]float64, len(row))
				}
				extra[k] = f
			}
		}
		if extra != nil {
			p.Extra = extra
		}
		out = append(out, p)
	}
	return out
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// RRDTimeframe is one of PVE's supported RRD windows.
type RRDTimeframe string

const (
	RRDHour  RRDTimeframe = "hour"
	RRDDay   RRDTimeframe = "day"
	RRDWeek  RRDTimeframe = "week"
	RRDMonth RRDTimeframe = "month"
	RRDYear  RRDTimeframe = "year"
)

// RRDCF is the RRD consolidation function: AVERAGE (default, smooth trend)
// or MAX (worst-case envelope — reveals spikes the average hides).
type RRDCF string

const (
	RRDAverage RRDCF = "AVERAGE"
	RRDMax     RRDCF = "MAX"
)

func (c *Client) NodeRRDData(ctx context.Context, node string, timeframe RRDTimeframe, cf RRDCF) ([]RRDPoint, error) {
	var out struct {
		Data []map[string]any `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/rrddata?timeframe=%s&cf=%s", PathEscape(node), timeframe, rrDCFOrAverage(cf))
	if err := c.get(ctx, path, &out); err != nil {
		if rrdResolutionMismatch(err) {
			return nil, nil // no usable RRD archives yet — charts show "no data"
		}
		return nil, err
	}
	return decodeRRDPoints(out.Data, true), nil
}

func (c *Client) GuestRRDData(ctx context.Context, guestType, node string, vmid int, timeframe RRDTimeframe, cf RRDCF) ([]RRDPoint, error) {
	var out struct {
		Data []map[string]any `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/%s/%d/rrddata?timeframe=%s&cf=%s", PathEscape(node), PathEscape(guestType), vmid, timeframe, rrDCFOrAverage(cf))
	if err := c.get(ctx, path, &out); err != nil {
		if rrdResolutionMismatch(err) {
			return nil, nil // no usable RRD archives yet — charts show "no data"
		}
		return nil, err
	}
	return decodeRRDPoints(out.Data, false), nil
}

// rrdResolutionMismatch reports PVE's "got wrong time resolution (21600 !=
// 60)" failure: the node's RRD archives for that window exist only at a
// coarser step than the timeframe asks for (fresh nodes, migrated RRD
// files). It's a permanent no-data condition for that window, not a fault —
// polling it every few seconds just floods the log with 502s.
func rrdResolutionMismatch(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	return se.StatusCode == 500 && strings.Contains(se.Body, "wrong time resolution")
}

func rrDCFOrAverage(cf RRDCF) string {
	if cf == RRDMax {
		return string(RRDMax)
	}
	return string(RRDAverage)
}
