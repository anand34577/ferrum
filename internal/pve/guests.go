package pve

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// GuestLiveStatus is the guest's current runtime status from
// /nodes/{node}/{qemu|lxc}/{vmid}/status/current. It carries data the
// cluster/resources overview doesn't: live network/disk counters, memory
// ballooning detail, and HA manager state.
//
// NetIn/NetOut/DiskRead/DiskWrite are CUMULATIVE byte counters since the
// guest started (not rates) — the UI derives rates from successive polls.
type GuestLiveStatus struct {
	Status    string  `json:"status"`
	Name      string  `json:"name,omitempty"`
	CPU       float64 `json:"cpu,omitempty"` // 0..1 fraction of allocated CPUs
	CPUs      int     `json:"cpus,omitempty"`
	Mem       int64   `json:"mem,omitempty"`
	MaxMem    int64   `json:"maxmem,omitempty"`
	Swap      int64   `json:"swap,omitempty"`      // LXC only
	MaxSwap   int64   `json:"maxswap,omitempty"`   // LXC only
	NetIn     int64   `json:"netin,omitempty"`     // cumulative bytes
	NetOut    int64   `json:"netout,omitempty"`    // cumulative bytes
	DiskRead  int64   `json:"diskread,omitempty"`  // cumulative bytes
	DiskWrite int64   `json:"diskwrite,omitempty"` // cumulative bytes
	Disk      int64   `json:"disk,omitempty"`
	MaxDisk   int64   `json:"maxdisk,omitempty"`
	Uptime    int64   `json:"uptime,omitempty"`
	Balloon   int64   `json:"balloon,omitempty"` // QEMU: current balloon target, bytes
	Lock      string  `json:"lock,omitempty"`
	Tags      string  `json:"tags,omitempty"`
	HAState   string  `json:"hastate,omitempty"` // HA manager state ("started"/"stopped"/...) when HA-managed
}

// GuestLiveStatus fetches the guest's current status. guestType is "qemu" or "lxc".
func (c *Client) GuestLiveStatus(ctx context.Context, guestType, node string, vmid int) (*GuestLiveStatus, error) {
	var out struct {
		Data GuestLiveStatus `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/%s/%d/status/current", PathEscape(node), PathEscape(guestType), vmid), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// GuestPowerAction issues a start/stop/shutdown/reset/suspend/resume action.
func (c *Client) GuestPowerAction(ctx context.Context, guestType, node string, vmid int, action string) (string, error) {
	if guestType != "qemu" && guestType != "lxc" {
		return "", fmt.Errorf("unknown guest type %q", guestType)
	}
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", PathEscape(node), PathEscape(guestType), vmid, PathEscape(action))
	var out struct {
		Data string `json:"data"` // UPID
	}
	if err := c.post(ctx, path, url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// GuestConfig is the subset of a VM/LXC's config commonly shown/edited.
type GuestConfig struct {
	Name    string `json:"name,omitempty"`
	Cores   int    `json:"cores,omitempty"`
	Sockets int    `json:"sockets,omitempty"`
	Memory  int    `json:"memory,omitempty"` // MB
	OSType  string `json:"ostype,omitempty"`
	Boot    string `json:"boot,omitempty"`
	Onboot  int    `json:"onboot,omitempty"`
	Tags    string `json:"tags,omitempty"`
	Notes   string `json:"notes,omitempty"`

	// Raw holds every key PVE returned, unfiltered — hardware lines (net0,
	// scsi0, ide2, rootfs, mp0, ...), CPU type, BIOS, machine type, and
	// anything else this struct doesn't explicitly model. Exposed to the API
	// (not just kept for internal use) so the UI can show real hardware
	// instead of silently dropping everything this struct doesn't name.
	Raw map[string]any `json:"raw,omitempty"`

	// Disks and NetworkDevices are Raw, parsed into a shape the UI can list
	// directly instead of re-deriving Proxmox's terse "scsi0" key format
	// and comma-separated value string on the frontend.
	Disks          []DiskDevice `json:"disks"`
	NetworkDevices []NetDevice  `json:"networkDevices"`
}

// DiskDevice is one storage attachment parsed from a guest's raw config —
// covers QEMU disk buses (scsi/virtio/ide/sata) and LXC's rootfs/mountpoints.
type DiskDevice struct {
	Key   string `json:"key"`   // e.g. "scsi0", "rootfs", "mp0"
	Value string `json:"value"` // raw Proxmox value string, e.g. "local-lvm:vm-100-disk-0,size=32G"
}

// NetDevice is one network interface parsed from a guest's raw config
// (netN for both QEMU and LXC).
type NetDevice struct {
	Key   string `json:"key"` // e.g. "net0"
	Value string `json:"value"`
}

var diskKeyPrefixes = []string{"scsi", "virtio", "ide", "sata", "mp"}

func parseHardware(raw map[string]any) (disks []DiskDevice, nets []NetDevice) {
	for key, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if key == "rootfs" {
			disks = append(disks, DiskDevice{Key: key, Value: s})
			continue
		}
		if strings.HasPrefix(key, "net") {
			nets = append(nets, NetDevice{Key: key, Value: s})
			continue
		}
		for _, prefix := range diskKeyPrefixes {
			if strings.HasPrefix(key, prefix) && len(key) > len(prefix) {
				// scsihw/virtio-scsi-pci-style keys aren't disks; only prefix+digits is.
				if _, err := strconv.Atoi(key[len(prefix):]); err == nil {
					disks = append(disks, DiskDevice{Key: key, Value: s})
				}
				break
			}
		}
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Key < disks[j].Key })
	sort.Slice(nets, func(i, j int) bool { return nets[i].Key < nets[j].Key })
	return disks, nets
}

func (c *Client) GuestConfig(ctx context.Context, guestType, node string, vmid int) (*GuestConfig, error) {
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/%s/%d/config", PathEscape(node), PathEscape(guestType), vmid), &out); err != nil {
		return nil, err
	}
	cfg := &GuestConfig{Raw: out.Data}
	if v, ok := out.Data["name"].(string); ok {
		cfg.Name = v
	}
	if v, ok := out.Data["ostype"].(string); ok {
		cfg.OSType = v
	}
	if v, ok := out.Data["boot"].(string); ok {
		cfg.Boot = v
	}
	if v, ok := out.Data["tags"].(string); ok {
		cfg.Tags = v
	}
	if v, ok := out.Data["description"].(string); ok {
		cfg.Notes = v
	}
	if v, ok := out.Data["cores"].(float64); ok {
		cfg.Cores = int(v)
	}
	if v, ok := out.Data["sockets"].(float64); ok {
		cfg.Sockets = int(v)
	}
	if v, ok := out.Data["memory"].(float64); ok {
		cfg.Memory = int(v)
	}
	if v, ok := out.Data["onboot"].(float64); ok {
		cfg.Onboot = int(v)
	}
	cfg.Disks, cfg.NetworkDevices = parseHardware(cfg.Raw)
	return cfg, nil
}

// UpdateGuestConfig applies a partial config change (cores/memory/name/tags/
// notes/boot order etc). Only non-empty fields in `set` are sent.
func (c *Client) UpdateGuestConfig(ctx context.Context, guestType, node string, vmid int, set url.Values) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/config", PathEscape(node), PathEscape(guestType), vmid)
	var out struct {
		Data string `json:"data"`
	}
	if err := c.put(ctx, path, set, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CloneOptions configures a full or linked clone of an existing guest.
type CloneOptions struct {
	NewID       int    // required: target VMID
	Name        string // new guest name/hostname
	TargetNode  string // empty = same node
	Full        bool   // full clone vs linked clone (linked only valid from a template)
	Storage     string // target storage (full clones only)
	Description string
}

func (c *Client) CloneGuest(ctx context.Context, guestType, node string, vmid int, opts CloneOptions) (string, error) {
	form := url.Values{"newid": {strconv.Itoa(opts.NewID)}}
	if opts.Name != "" {
		form.Set("name", opts.Name)
	}
	if opts.TargetNode != "" {
		form.Set("target", opts.TargetNode)
	}
	if opts.Full {
		form.Set("full", "1")
	}
	if opts.Storage != "" {
		form.Set("storage", opts.Storage)
	}
	if opts.Description != "" {
		form.Set("description", opts.Description)
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/clone", PathEscape(node), PathEscape(guestType), vmid), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// MigrateGuest starts an offline or online (live) migration to another node.
func (c *Client) MigrateGuest(ctx context.Context, guestType, node string, vmid int, targetNode string, online bool) (string, error) {
	form := url.Values{"target": {targetNode}}
	if online {
		form.Set("online", "1")
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/migrate", PathEscape(node), PathEscape(guestType), vmid), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// Snapshot describes one point-in-time snapshot of a guest.
type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SnapTime    int64  `json:"snaptime,omitempty"`
	Parent      string `json:"parent,omitempty"`
	VMState     int    `json:"vmstate,omitempty"`
}

func (c *Client) ListSnapshots(ctx context.Context, guestType, node string, vmid int) ([]Snapshot, error) {
	var out struct {
		Data []Snapshot `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/%s/%d/snapshot", PathEscape(node), PathEscape(guestType), vmid), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, guestType, node string, vmid int, name, description string, includeState bool) (string, error) {
	form := url.Values{"snapname": {name}}
	if description != "" {
		form.Set("description", description)
	}
	if includeState && guestType == "qemu" {
		form.Set("vmstate", "1")
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/snapshot", PathEscape(node), PathEscape(guestType), vmid), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) RollbackSnapshot(ctx context.Context, guestType, node string, vmid int, name string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/snapshot/%s/rollback", PathEscape(node), PathEscape(guestType), vmid, url.PathEscape(name)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) DeleteSnapshot(ctx context.Context, guestType, node string, vmid int, name string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.delete(ctx, fmt.Sprintf("/nodes/%s/%s/%d/snapshot/%s", PathEscape(node), PathEscape(guestType), vmid, url.PathEscape(name)), nil, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// DeleteGuest destroys a VM/LXC. purgeJobs also removes it from backup jobs.
func (c *Client) DeleteGuest(ctx context.Context, guestType, node string, vmid int, purgeJobs bool) (string, error) {
	form := url.Values{}
	if purgeJobs {
		form.Set("purge", "1")
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.delete(ctx, fmt.Sprintf("/nodes/%s/%s/%d", PathEscape(node), PathEscape(guestType), vmid), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) SetTemplate(ctx context.Context, guestType, node string, vmid int) error {
	return c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/template", PathEscape(node), PathEscape(guestType), vmid), url.Values{}, nil)
}

func (c *Client) UnlockGuest(ctx context.Context, guestType, node string, vmid int) error {
	return c.UpdateGuestConfigRaw(ctx, guestType, node, vmid, url.Values{"delete": {"lock"}})
}

// UpdateGuestConfigRaw is UpdateGuestConfig without the parsed-response
// wrapper, used for maintenance operations like clearing "lock".
func (c *Client) UpdateGuestConfigRaw(ctx context.Context, guestType, node string, vmid int, form url.Values) error {
	return c.put(ctx, fmt.Sprintf("/nodes/%s/%s/%d/config", PathEscape(node), PathEscape(guestType), vmid), form, nil)
}

// ResizeDisk grows a guest's disk. size is a PVE size delta/absolute string,
// e.g. "+10G" to grow by 10GB or "32G" to set an absolute size.
func (c *Client) ResizeDisk(ctx context.Context, guestType, node string, vmid int, disk, size string) error {
	form := url.Values{"disk": {disk}, "size": {size}}
	return c.put(ctx, fmt.Sprintf("/nodes/%s/%s/%d/resize", PathEscape(node), PathEscape(guestType), vmid), form, nil)
}

// NextID asks Proxmox for the next free VMID.
func (c *Client) NextID(ctx context.Context) (int, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.get(ctx, "/cluster/nextid", &out); err != nil {
		return 0, err
	}
	return strconv.Atoi(out.Data)
}

// CreateVMOptions models the common fields for provisioning a new QEMU VM,
// including cloud-init driven configuration for automated setup.
type CreateVMOptions struct {
	VMID    int
	Name    string
	Node    string
	Cores   int
	Memory  int // MB
	Storage string
	DiskGB  int
	ISO     string // e.g. "local:iso/ubuntu-24.04.iso" — omit when cloning a cloud-init template instead
	Bridge  string // e.g. "vmbr0"

	// Cloud-init (only applied when CIUser is set — the VM must use a
	// cloud-init-capable base image/template for these to take effect).
	CIUser       string
	CIPassword   string
	SSHPublicKey string
	IPConfig     string // e.g. "ip=dhcp" or "ip=10.0.0.5/24,gw=10.0.0.1"
	Nameserver   string
}

func (c *Client) CreateVM(ctx context.Context, opts CreateVMOptions) (string, error) {
	form := url.Values{
		"vmid":   {strconv.Itoa(opts.VMID)},
		"cores":  {strconv.Itoa(opts.Cores)},
		"memory": {strconv.Itoa(opts.Memory)},
	}
	if opts.Name != "" {
		form.Set("name", opts.Name)
	}
	if opts.Bridge != "" {
		form.Set("net0", "virtio,bridge="+opts.Bridge)
	}
	if opts.Storage != "" && opts.DiskGB > 0 {
		form.Set("scsi0", fmt.Sprintf("%s:%d", opts.Storage, opts.DiskGB))
		form.Set("scsihw", "virtio-scsi-pci")
	}
	if opts.ISO != "" {
		form.Set("ide2", opts.ISO+",media=cdrom")
	}
	if opts.CIUser != "" {
		form.Set("ciuser", opts.CIUser)
		form.Set("ide3", opts.Storage+":cloudinit")
	}
	if opts.CIPassword != "" {
		form.Set("cipassword", opts.CIPassword)
	}
	if opts.SSHPublicKey != "" {
		form.Set("sshkeys", url.QueryEscape(opts.SSHPublicKey))
	}
	if opts.IPConfig != "" {
		form.Set("ipconfig0", opts.IPConfig)
	}
	if opts.Nameserver != "" {
		form.Set("nameserver", opts.Nameserver)
	}

	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/qemu", PathEscape(opts.Node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateLXCOptions models the common fields for provisioning a new container.
type CreateLXCOptions struct {
	VMID         int
	Hostname     string
	Node         string
	Cores        int
	Memory       int // MB
	Storage      string
	DiskGB       int
	Template     string // e.g. "local:vztmpl/ubuntu-24.04-standard_24.04-1_amd64.tar.zst"
	Bridge       string
	Password     string
	SSHPublicKey string
	IPConfig     string
	Unprivileged bool
}

func (c *Client) CreateLXC(ctx context.Context, opts CreateLXCOptions) (string, error) {
	form := url.Values{
		"vmid":       {strconv.Itoa(opts.VMID)},
		"cores":      {strconv.Itoa(opts.Cores)},
		"memory":     {strconv.Itoa(opts.Memory)},
		"ostemplate": {opts.Template},
	}
	if opts.Hostname != "" {
		form.Set("hostname", opts.Hostname)
	}
	if opts.Storage != "" && opts.DiskGB > 0 {
		form.Set("rootfs", fmt.Sprintf("%s:%d", opts.Storage, opts.DiskGB))
	}
	if opts.Bridge != "" {
		net := "name=eth0,bridge=" + opts.Bridge
		if opts.IPConfig != "" {
			net += "," + opts.IPConfig
		} else {
			net += ",ip=dhcp"
		}
		form.Set("net0", net)
	}
	if opts.Password != "" {
		form.Set("password", opts.Password)
	}
	if opts.SSHPublicKey != "" {
		form.Set("ssh-public-keys", url.QueryEscape(opts.SSHPublicKey))
	}
	if opts.Unprivileged {
		form.Set("unprivileged", "1")
	}

	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/lxc", PathEscape(opts.Node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// VNCProxy requests a short-lived VNC ticket/port for a QEMU or LXC console.
type VNCProxy struct {
	Ticket string `json:"ticket"`
	Port   string `json:"port"`
	User   string `json:"user"`
}

func (c *Client) OpenVNCProxy(ctx context.Context, guestType, node string, vmid int) (*VNCProxy, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/vncproxy", PathEscape(node), PathEscape(guestType), vmid)
	form := url.Values{"websocket": {"1"}}
	var out struct {
		Data VNCProxy `json:"data"`
	}
	if err := c.post(ctx, path, form, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// AgentNetworkInterface is one interface reported by the QEMU guest agent
// (requires `qemu-guest-agent` installed and running inside the VM, and the
// agent enabled in the VM's config — Proxmox's own UI shows this same data
// under the VM's "Network" tab; LXC containers don't have a guest agent
// concept since Proxmox already controls their network namespace directly).
type AgentNetworkInterface struct {
	Name            string   `json:"name"`
	HardwareAddress string   `json:"hardware-address,omitempty"`
	IPAddresses     []string `json:"ip-addresses"`
}

// GuestAgentNetworkInterfaces queries the live IP/MAC info the in-guest
// agent reports. Returns a clear error (surfaced to the UI as-is) if the
// agent isn't installed/running/enabled rather than an empty list, so the
// UI can tell "no agent" apart from "agent reports no interfaces".
func (c *Client) GuestAgentNetworkInterfaces(ctx context.Context, node string, vmid int) ([]AgentNetworkInterface, error) {
	var out struct {
		Data struct {
			Result []struct {
				Name            string `json:"name"`
				HardwareAddress string `json:"hardware-address,omitempty"`
				IPAddresses     []struct {
					IPAddress string `json:"ip-address"`
				} `json:"ip-addresses,omitempty"`
			} `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	interfaces := make([]AgentNetworkInterface, 0, len(out.Data.Result))
	for _, r := range out.Data.Result {
		ips := make([]string, 0, len(r.IPAddresses))
		for _, ip := range r.IPAddresses {
			ips = append(ips, ip.IPAddress)
		}
		interfaces = append(interfaces, AgentNetworkInterface{Name: r.Name, HardwareAddress: r.HardwareAddress, IPAddresses: ips})
	}
	return interfaces, nil
}

// VNCWebSocketPath builds the path (relative to the host, port 8006) for the
// authenticated VNC websocket upgrade used once a VNCProxy ticket is minted.
func VNCWebSocketPath(guestType, node string, vmid int, port, ticket string) string {
	v := url.Values{"port": {port}, "vncticket": {ticket}}
	return fmt.Sprintf("/api2/json/nodes/%s/%s/%d/vncwebsocket?%s", PathEscape(node), PathEscape(guestType), vmid, v.Encode())
}
