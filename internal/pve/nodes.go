package pve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// FlexString accepts a JSON string or number and keeps the raw text. PVE
// renders some fields (notably cpuinfo.mhz) as a number on some node
// versions and as a string on others — a plain string field 500s the whole
// node-status request on the numeric variant.
type FlexString string

func (s *FlexString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = FlexString(str)
		return nil
	}
	// Numeric / bare literal: keep the bytes verbatim ("2400.02" etc).
	*s = FlexString(b)
	return nil
}

type Node struct {
	Node    string  `json:"node"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	MaxCPU  int     `json:"maxcpu"`
	Mem     int64   `json:"mem"`
	MaxMem  int64   `json:"maxmem"`
	Disk    int64   `json:"disk"`
	MaxDisk int64   `json:"maxdisk"`
	Uptime  int64   `json:"uptime"`
}

func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	var out struct {
		Data []Node `json:"data"`
	}
	if err := c.get(ctx, "/nodes", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// NodeStatus is the detailed single-node status payload (CPU model, kernel,
// load average, memory/swap/rootfs breakdown, PVE version).
type NodeStatus struct {
	CPU  float64 `json:"cpu"`  // 0..1 fraction of total cores currently used
	Wait float64 `json:"wait"` // 0..1 fraction of CPU time spent waiting on IO
	Idle float64 `json:"idle"` // 0..1 fraction idle

	CPUInfo struct {
		Model   string     `json:"model"`
		Cores   int        `json:"cores"`
		Sockets int        `json:"sockets"`
		MHz     FlexString `json:"mhz,omitempty"`
	} `json:"cpuinfo"`
	Memory struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
		Free  int64 `json:"free"`
	} `json:"memory"`
	Swap struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"swap"`
	RootFS struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
		Avail int64 `json:"avail"`
	} `json:"rootfs"`
	KSM struct {
		Shared int64 `json:"shared"` // bytes saved by kernel same-page merging
	} `json:"ksm"`
	LoadAvg    []string `json:"loadavg"`
	Uptime     int64    `json:"uptime"`
	KVersion   string   `json:"kversion"`
	PVEVersion string   `json:"pveversion"`
}

func (c *Client) NodeStatus(ctx context.Context, node string) (*NodeStatus, error) {
	var out struct {
		Data NodeStatus `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/status", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

func (c *Client) RebootNode(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	form := url.Values{"command": {"reboot"}}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/status", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) ShutdownNode(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	form := url.Values{"command": {"shutdown"}}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/status", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// AptUpdate is one pending package update on a node.
type AptUpdate struct {
	Package     string `json:"Package"`
	OldVersion  string `json:"OldVersion"`
	Version     string `json:"Version"`
	Priority    string `json:"Priority"`
	Description string `json:"Description"`
}

func (c *Client) AptUpdates(ctx context.Context, node string) ([]AptUpdate, error) {
	var out struct {
		Data []AptUpdate `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/apt/update", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// RefreshAptIndex triggers `apt-get update` on the node (async, returns a UPID).
func (c *Client) RefreshAptIndex(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/apt/update", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// UpgradeNode previously called the apt index-refresh endpoint under an
// "upgrade" name — it never installed any packages. Stock PVE has no plain
// REST action that runs apt-get upgrade/dist-upgrade; the real web UI does
// this via an interactive xterm.js task (`pveupgrade`), not a single POST.
// ponytail: erroring honestly beats silently no-op'ing under an "Upgrade"
// button. Wire real support later via a task/exec-stream endpoint if needed.
var ErrUpgradeNotSupported = errors.New("pve: package upgrade is not available via the REST API; use the node shell (pveupgrade) or a console session")

func (c *Client) UpgradeNode(ctx context.Context, node string) (string, error) {
	return "", ErrUpgradeNotSupported
}

// SyslogEntry is one line from a node's syslog.
type SyslogEntry struct {
	N int    `json:"n"`
	T string `json:"t"`
}

func (c *Client) NodeSyslog(ctx context.Context, node string, limit int) ([]SyslogEntry, error) {
	q := ""
	if limit > 0 {
		q = fmt.Sprintf("?limit=%d", limit)
	}
	var out struct {
		Data []SyslogEntry `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/syslog%s", PathEscape(node), q), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// NetworkInterface is one node network device (bridge, bond, vlan, physical nic).
type NetworkInterface struct {
	Iface       string `json:"iface"`
	Type        string `json:"type"`
	Active      int    `json:"active,omitempty"`
	Address     string `json:"address,omitempty"`
	Netmask     string `json:"netmask,omitempty"`
	Gateway     string `json:"gateway,omitempty"`
	Autostart   int    `json:"autostart,omitempty"`
	BridgePorts string `json:"bridge_ports,omitempty"`
}

func (c *Client) NodeNetwork(ctx context.Context, node string) ([]NetworkInterface, error) {
	var out struct {
		Data []NetworkInterface `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/network", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// NodeDNSConfig is a node's resolver config (/nodes/{node}/dns).
type NodeDNSConfig struct {
	Search string `json:"search,omitempty"`
	DNS1   string `json:"dns1,omitempty"`
	DNS2   string `json:"dns2,omitempty"`
	DNS3   string `json:"dns3,omitempty"`
}

func (c *Client) NodeDNS(ctx context.Context, node string) (*NodeDNSConfig, error) {
	var out struct {
		Data NodeDNSConfig `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/dns", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

func (c *Client) UpdateNodeDNS(ctx context.Context, node string, cfg NodeDNSConfig) error {
	form := url.Values{}
	if cfg.Search != "" {
		form.Set("search", cfg.Search)
	}
	if cfg.DNS1 != "" {
		form.Set("dns1", cfg.DNS1)
	}
	if cfg.DNS2 != "" {
		form.Set("dns2", cfg.DNS2)
	}
	if cfg.DNS3 != "" {
		form.Set("dns3", cfg.DNS3)
	}
	return c.put(ctx, fmt.Sprintf("/nodes/%s/dns", PathEscape(node)), form, nil)
}

// NodeTimeInfo is a node's clock/timezone (/nodes/{node}/time).
type NodeTimeInfo struct {
	Timezone  string `json:"timezone"`
	Time      int64  `json:"time"`
	LocalTime int64  `json:"localtime"`
}

func (c *Client) NodeTime(ctx context.Context, node string) (*NodeTimeInfo, error) {
	var out struct {
		Data NodeTimeInfo `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/time", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

func (c *Client) SetNodeTimezone(ctx context.Context, node, timezone string) error {
	return c.put(ctx, fmt.Sprintf("/nodes/%s/time", PathEscape(node)), url.Values{"timezone": {timezone}}, nil)
}

// NodeHosts is the raw content of a node's /etc/hosts, as PVE manages it
// (/nodes/{node}/hosts) — Data is the whole file; Digest guards concurrent
// edits (PVE rejects a write whose digest doesn't match the current file).
type NodeHosts struct {
	Data   string `json:"data"`
	Digest string `json:"digest,omitempty"`
}

func (c *Client) NodeHosts(ctx context.Context, node string) (*NodeHosts, error) {
	var out struct {
		Data NodeHosts `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/hosts", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// UpdateNodeHosts replaces /etc/hosts content. digest must be the value last
// read from NodeHosts to avoid clobbering a concurrent edit.
func (c *Client) UpdateNodeHosts(ctx context.Context, node, data, digest string) error {
	form := url.Values{"data": {data}}
	if digest != "" {
		form.Set("digest", digest)
	}
	return c.post(ctx, fmt.Sprintf("/nodes/%s/hosts", PathEscape(node)), form, nil)
}

// --- Tasks ---

type Task struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	User      string `json:"user"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime,omitempty"`
	ID        string `json:"id,omitempty"`

	// ExitStatus is only set by the single-task endpoint
	// (/tasks/{upid}/status), where Status is the lifecycle state
	// ("running"/"stopped") and the success/failure verdict lives here
	// ("OK", or the error text). The *list* endpoints instead put that
	// verdict straight into Status. Normalize() erases the difference so
	// callers can read Status uniformly.
	ExitStatus string `json:"exitstatus,omitempty"`
}

// Normalize folds the single-task shape into the list shape: a finished task
// reports its exit verdict in Status, exactly as /nodes/{node}/tasks does.
// Without this, "is my task OK?" checks silently fail on the status endpoint,
// which answers "stopped" rather than "OK".
func (t *Task) Normalize() {
	if t.ExitStatus != "" {
		t.Status = t.ExitStatus
	}
}

// ClusterTasks returns the cluster-wide recent task list in one call.
// Prefer it over fanning NodeTasks out across every node: PVE aggregates
// the same data server-side, so an N-node cluster costs 1 upstream request
// instead of N on every poll tick.
//
// Unlike /nodes/{node}/tasks, PVE's /cluster/tasks endpoint takes no query
// parameters at all — sending ?limit here 400s ("property is not defined in
// schema"), so limit is applied client-side after the fetch instead.
func (c *Client) ClusterTasks(ctx context.Context, limit int) ([]Task, error) {
	var out struct {
		Data []Task `json:"data"`
	}
	if err := c.get(ctx, "/cluster/tasks", &out); err != nil {
		return nil, err
	}
	if limit > 0 && len(out.Data) > limit {
		out.Data = out.Data[:limit]
	}
	return out.Data, nil
}

func (c *Client) NodeTasks(ctx context.Context, node string, limit int) ([]Task, error) {
	q := ""
	if limit > 0 {
		q = "?limit=" + strconv.Itoa(limit)
	}
	var out struct {
		Data []Task `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks%s", PathEscape(node), q), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) TaskStatus(ctx context.Context, node, upid string) (*Task, error) {
	var out struct {
		Data Task `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/status", PathEscape(node), url.PathEscape(upid)), &out); err != nil {
		return nil, err
	}
	out.Data.Normalize()
	return &out.Data, nil
}

// TaskLog returns the task's output. PVE defaults this endpoint to the
// first 50 lines, which silently truncated every non-trivial backup or
// migration log; ask for a real window instead.
func (c *Client) TaskLog(ctx context.Context, node, upid string) ([]string, error) {
	var out struct {
		Data []struct {
			T string `json:"t"`
		} `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/log?start=0&limit=5000", PathEscape(node), url.PathEscape(upid)), &out); err != nil {
		return nil, err
	}
	lines := make([]string, len(out.Data))
	for i, l := range out.Data {
		lines[i] = l.T
	}
	return lines, nil
}

func (c *Client) CancelTask(ctx context.Context, node, upid string) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/tasks/%s", PathEscape(node), url.PathEscape(upid)), nil, nil)
}

// NodeTermProxy requests a short-lived shell (xterm.js) console ticket for
// the node's own host shell — a root-equivalent terminal on the hypervisor
// itself, distinct from any guest's console.
func (c *Client) NodeTermProxy(ctx context.Context, node string) (*TermProxy, error) {
	var out struct {
		Data TermProxy `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/termproxy", PathEscape(node)), url.Values{}, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// WakeOnLan sends a WoL magic packet to wake a powered-off node — only works
// when the node's network card and BIOS support it and its MAC is configured
// in datacenter.cfg.
func (c *Client) WakeOnLan(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/wakeonlan", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// StartAllGuests starts every guest configured to autostart on this node
// (in onboot-priority order) — the same bulk action as PVE's own
// "Bulk Start" in its node view.
func (c *Client) StartAllGuests(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/startall", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// StopAllGuests stops every running guest on this node.
func (c *Client) StopAllGuests(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/stopall", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// JournalEntry is one line of a node's systemd journal
// (/nodes/{node}/journal) — the structured, filterable replacement for the
// legacy plain-text syslog endpoint (NodeSyslog).
type JournalEntry struct {
	N int    `json:"n"`
	T string `json:"t"`
}

// UnmarshalJSON accepts either the documented {"n": <int>, "t": <string>}
// shape or a bare string — some PVE versions return one line of the journal
// as a plain string with no line number instead of the object shape, and
// that alone otherwise fails json.Unmarshal for the entire journal array
// (one differently-shaped line 502s the whole request; see FlexString above
// for the same kind of PVE version inconsistency elsewhere in this file).
// "n" itself is also seen as a quoted number on some versions, so it's
// decoded via FlexString and parsed loosely rather than declared int.
func (e *JournalEntry) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		e.N, e.T = 0, s
		return nil
	}
	var a struct {
		N FlexString `json:"n"`
		T string     `json:"t"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	n, _ := strconv.Atoi(string(a.N)) // non-numeric/absent "n" just stays 0
	*e = JournalEntry{N: n, T: a.T}
	return nil
}

// NodeJournal fetches the tail of a node's systemd journal. lastEntries
// limits how many lines back from the end are returned (PVE's "since"/
// "until" cursor params aren't exposed here — this covers the common "show
// recent log" case the old NodeSyslog covered).
func (c *Client) NodeJournal(ctx context.Context, node string, lastEntries int) ([]JournalEntry, error) {
	q := ""
	if lastEntries > 0 {
		q = fmt.Sprintf("?lastentries=%d", lastEntries)
	}
	var out struct {
		Data []JournalEntry `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/journal%s", PathEscape(node), q), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// --- Services ---

// NodeService is one systemd unit PVE manages on a node
// (/nodes/{node}/services), e.g. pveproxy, pvedaemon, pvestatd, corosync,
// pve-cluster, pvescheduler, sshd.
type NodeService struct {
	Service     string `json:"service"`
	Name        string `json:"name,omitempty"`
	Desc        string `json:"desc,omitempty"`
	State       string `json:"state"` // "running" / "stopped" / ...
	ActiveState string `json:"active-state,omitempty"`
	UnitState   string `json:"unit-state,omitempty"`
}

// NodeServices lists every service PVE manages on a node.
func (c *Client) NodeServices(ctx context.Context, node string) ([]NodeService, error) {
	var out struct {
		Data []NodeService `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/services", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// NodeServiceState fetches the current state of a single service.
func (c *Client) NodeServiceState(ctx context.Context, node, service string) (*NodeService, error) {
	var out struct {
		Data NodeService `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/services/%s/state", PathEscape(node), PathEscape(service)), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// NodeServiceAction starts/stops/restarts/reloads a systemd unit PVE manages.
// Some services (corosync, pve-cluster) are critical to cluster membership —
// PVE's own API has no special-casing for them, and neither does this method;
// any confirmation/safety messaging belongs in the caller (the UI).
func (c *Client) NodeServiceAction(ctx context.Context, node, service, action string) error {
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	path := fmt.Sprintf("/nodes/%s/services/%s/%s", PathEscape(node), PathEscape(service), PathEscape(action))
	return c.post(ctx, path, url.Values{}, nil)
}
