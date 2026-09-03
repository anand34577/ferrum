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
	return &out.Data, nil
}

func (c *Client) TaskLog(ctx context.Context, node, upid string) ([]string, error) {
	var out struct {
		Data []struct {
			T string `json:"t"`
		} `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/log", PathEscape(node), url.PathEscape(upid)), &out); err != nil {
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
