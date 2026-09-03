package pve

import (
	"context"
	"fmt"
	"net/url"
)

// Disk is one physical drive as reported by PVE's disk manager
// (/nodes/{node}/disks/list) — every drive on the node, not just ones
// backing a configured storage, so an NVMe sitting idle (or holding the
// boot/root filesystem) still shows up.
type Disk struct {
	DevPath string `json:"devpath"`
	Model   string `json:"model,omitempty"`
	Serial  string `json:"serial,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
	Size    int64  `json:"size"`
	Type    string `json:"type"`              // "hdd" | "ssd" | "usb" | "unknown"
	RPM     int    `json:"rpm,omitempty"`     // 0 for SSD/NVMe
	Wearout int    `json:"wearout,omitempty"` // SSD/NVMe life remaining, percent (0-100); absent for HDDs
	Health  string `json:"health,omitempty"`  // "PASSED" | "FAILED" | "UNKNOWN"
	Used    string `json:"used,omitempty"`    // what's using it: "LVM", "ZFS", "partitions", "" (unused)
	WWN     string `json:"wwn,omitempty"`
	GPT     int    `json:"gpt,omitempty"`
	OSDID   int    `json:"osdid,omitempty"` // Ceph OSD id, -1 when not a Ceph OSD
}

// NodeDisks lists every physical disk on the node.
func (c *Client) NodeDisks(ctx context.Context, node string) ([]Disk, error) {
	var out struct {
		Data []Disk `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/disks/list", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SmartAttribute is one row of a SMART attribute table — the ATA/SATA shape
// (id/name/value/worst/threshold/raw/flags); NVMe drives report a shorter,
// differently-named set of health fields instead, which land in Attributes
// with only Name/Value populated so the UI can render either uniformly.
type SmartAttribute struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`
	Worst     string `json:"worst,omitempty"`
	Threshold string `json:"threshold,omitempty"`
	Raw       string `json:"raw,omitempty"`
	Flags     string `json:"flags,omitempty"`
	Fail      string `json:"fail,omitempty"`
}

// SmartData is the full SMART report for one disk (/nodes/{node}/disks/smart).
type SmartData struct {
	Type       string           `json:"type"` // "ata" | "nvme" | "unknown"
	Health     string           `json:"health,omitempty"`
	Attributes []SmartAttribute `json:"attributes,omitempty"`
	Text       string           `json:"text,omitempty"` // raw smartctl output, when PVE doesn't parse a structured table
}

// DiskSMART fetches the full SMART report for one disk, identified by its
// devpath (e.g. "/dev/nvme0n1") from NodeDisks.
func (c *Client) DiskSMART(ctx context.Context, node, devpath string) (*SmartData, error) {
	var out struct {
		Data SmartData `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/disks/smart?disk=%s", PathEscape(node), url.QueryEscape(devpath))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}
