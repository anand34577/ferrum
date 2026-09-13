package pve

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
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

// --- Provisioning: turning an idle physical disk into usable storage.
// Everything below is destructive (wipes/repartitions the disk) or creates
// a new pool/volume group; callers must gate these behind an admin check
// and a scary confirmation, same as any other irreversible operation.

// wipeTaskPollInterval/wipeTaskMaxWait bound how long WipeDisk waits for a
// worker-style wipedisk UPID to finish before giving up.
const (
	wipeTaskPollInterval = 500 * time.Millisecond
	wipeTaskMaxWait      = 2 * time.Minute
)

// WipeDisk destroys the partition table (and any filesystem signatures) on
// devpath, returning it to "unused" so it can be handed to InitGPT or one of
// the CreateXStorage calls below. On PVE versions where wipedisk runs as a
// background worker the response is a UPID rather than null — in that case
// the call waits for the task to finish, so the disk is genuinely "unused"
// on return either way (matching the inline behavior of the versions that
// answer synchronously).
func (c *Client) WipeDisk(ctx context.Context, node, devpath string) error {
	form := url.Values{"disk": {devpath}}
	var out struct {
		Data any `json:"data"` // null (answered inline) or a UPID string
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/wipedisk", PathEscape(node)), form, &out); err != nil {
		return err
	}
	upid, ok := out.Data.(string)
	if !ok || !strings.HasPrefix(upid, "UPID:") {
		return nil
	}
	return c.waitTask(ctx, node, upid)
}

// waitTask polls /nodes/{node}/tasks/{upid}/status until the task stops
// running, translating a failed exit status into an error.
func (c *Client) waitTask(ctx context.Context, node, upid string) error {
	deadline := time.Now().Add(wipeTaskMaxWait)
	for {
		task, err := c.TaskStatus(ctx, node, upid)
		if err != nil {
			return err
		}
		switch task.Status {
		case "OK":
			return nil
		case "running", "":
			// still working ("" = state not recorded yet)
		default:
			return fmt.Errorf("wipedisk task failed: %s", task.Status)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wipedisk task did not finish within %s", wipeTaskMaxWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wipeTaskPollInterval):
		}
	}
}

// InitGPT writes a fresh GPT partition table to devpath. uuid is optional
// (PVE generates one when empty). Returns a UPID.
func (c *Client) InitGPT(ctx context.Context, node, devpath, uuid string) (string, error) {
	form := url.Values{"disk": {devpath}}
	if uuid != "" {
		form.Set("uuid", uuid)
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/initgpt", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func boolForm(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// CreateDirectoryStorage formats devpath with ext4 and mounts it under
// /mnt/pve/<name>, optionally registering it as a "dir" storage
// (add_storage=1) so it's immediately usable for VM disks/backups/ISOs.
// Returns a UPID.
func (c *Client) CreateDirectoryStorage(ctx context.Context, node, devpath, name string, addStorage bool) (string, error) {
	form := url.Values{"device": {devpath}, "name": {name}, "add_storage": {boolForm(addStorage)}}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/directory", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateLVMStorage creates an LVM volume group on devpath, optionally
// registering it as an "lvm" storage. Returns a UPID.
func (c *Client) CreateLVMStorage(ctx context.Context, node, devpath, name string, addStorage bool) (string, error) {
	form := url.Values{"device": {devpath}, "name": {name}, "add_storage": {boolForm(addStorage)}}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/lvm", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateLVMThinStorage creates an LVM-thin pool on devpath, optionally
// registering it as an "lvmthin" storage. Returns a UPID.
func (c *Client) CreateLVMThinStorage(ctx context.Context, node, devpath, name string, addStorage bool) (string, error) {
	form := url.Values{"device": {devpath}, "name": {name}, "add_storage": {boolForm(addStorage)}}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/lvmthin", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateZFSPool creates a ZFS pool spanning devices (raidlevel e.g. "single",
// "mirror", "raid10", "raidz", "raidz2", "raidz3"), optionally registering it
// as a "zfspool" storage. ashift <= 0 leaves it at PVE's default. Returns a
// UPID.
func (c *Client) CreateZFSPool(ctx context.Context, node, name string, devices []string, raidLevel string, ashift int, addStorage bool) (string, error) {
	form := url.Values{
		"name":        {name},
		"devices":     {strings.Join(devices, ",")},
		"add_storage": {boolForm(addStorage)},
	}
	if raidLevel != "" {
		form.Set("raidlevel", raidLevel)
	}
	if ashift > 0 {
		form.Set("ashift", strconv.Itoa(ashift))
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/disks/zfs", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// ZFSPoolInfo is one existing ZFS pool as reported by the disk manager
// (/nodes/{node}/disks/zfs) — distinct from the guest-facing storage config,
// this is what's actually on-disk regardless of whether it's registered.
type ZFSPoolInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size,omitempty"`
	Free   int64  `json:"free,omitempty"`
	Health string `json:"health,omitempty"`
}

// NodeZFSPools lists ZFS pools already present on node.
func (c *Client) NodeZFSPools(ctx context.Context, node string) ([]ZFSPoolInfo, error) {
	var out struct {
		Data []ZFSPoolInfo `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/disks/zfs", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// LVMVolumeGroup is one existing LVM volume group (/nodes/{node}/disks/lvm).
type LVMVolumeGroup struct {
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
	Free int64  `json:"free,omitempty"`
}

// NodeLVMVolumeGroups lists LVM volume groups already present on node.
func (c *Client) NodeLVMVolumeGroups(ctx context.Context, node string) ([]LVMVolumeGroup, error) {
	var out struct {
		Data []LVMVolumeGroup `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/disks/lvm", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// LVMThinPool is one existing LVM-thin pool (/nodes/{node}/disks/lvmthin).
type LVMThinPool struct {
	LV   string `json:"lv"`
	VG   string `json:"vg"`
	Size int64  `json:"size,omitempty"`
	Used int64  `json:"used,omitempty"`
}

// NodeLVMThinPools lists LVM-thin pools already present on node.
func (c *Client) NodeLVMThinPools(ctx context.Context, node string) ([]LVMThinPool, error) {
	var out struct {
		Data []LVMThinPool `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/disks/lvmthin", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
