// Package export generates infrastructure-as-code (Terraform HCL, Ansible
// inventory YAML) from a live fleet's current inventory. It takes the guest
// data as input rather than fetching it — callers (internal/api/export.go)
// already have it from pve.ClusterResource and pve.GuestConfig calls made
// elsewhere, and this package stays a pure text generator so it's trivially
// unit-testable without a PVE client.
package export

import (
	"fmt"
	"regexp"
	"strings"

	"ferrum/internal/pve"
)

// Guest is one VM/LXC to render, combining the fleet inventory row with its
// full config.
type Guest struct {
	Resource pve.ClusterResource // Type ("qemu"|"lxc"), Node, VMID, Name, Status, Tags, ...
	Config   *pve.GuestConfig    // nil is tolerated — the guest still gets a minimal block
	// IP is the guest's live IP address as last reported by the QEMU/LXC
	// guest agent, when known. Empty when the agent isn't installed/running
	// or was never queried — that's a normal, expected state, not an error;
	// callers that want it populate it via GuestAgentNetworkInterfaces
	// before calling into this package.
	IP string
}

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// slugify turns a guest name into a safe identifier for both a Terraform
// resource label and an Ansible inventory hostname.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if s == "" {
		return "guest"
	}
	return s
}

// splitTags parses Proxmox's guest tag string. The web UI joins tags with
// ";" but older API responses and hand-edited configs sometimes use ",", so
// both are accepted.
func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// parseCSVKV parses Proxmox's comma-separated "key=value,key=value" config
// line syntax (disk/net/rootfs/mp values) into a map. Non-empty segments
// without an "=" are ignored — none of what this package reads out of these
// lines needs them.
func parseCSVKV(value string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		if part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			out[k] = v
		}
	}
	return out
}

// parseDiskValue splits a disk/rootfs config value's leading
// "storage:volume" segment from its "size=" attribute, e.g.
// "local-lvm:vm-100-disk-0,size=32G,ssd=1" -> ("local-lvm", "32G").
func parseDiskValue(value string) (storage, size string) {
	parts := strings.Split(value, ",")
	if len(parts) > 0 {
		if before, _, ok := strings.Cut(parts[0], ":"); ok {
			storage = before
		}
	}
	if len(parts) > 1 {
		size = parseCSVKV(strings.Join(parts[1:], ","))["size"]
	}
	return storage, size
}

// firstDisk picks the guest's primary disk — the first disk device that
// isn't an LXC secondary mountpoint ("mpN") — and returns its storage and
// size. ("", "") when the guest has no disk devices at all.
func firstDisk(disks []pve.DiskDevice) (storage, size string) {
	for _, d := range disks {
		if strings.HasPrefix(d.Key, "mp") {
			continue
		}
		return parseDiskValue(d.Value)
	}
	return "", ""
}

// rootfsDisk returns the LXC rootfs device, if the guest has one.
func rootfsDisk(disks []pve.DiskDevice) (storage, size string, ok bool) {
	for _, d := range disks {
		if d.Key == "rootfs" {
			storage, size = parseDiskValue(d.Value)
			return storage, size, true
		}
	}
	return "", "", false
}

var qemuNetModels = []string{"virtio", "e1000", "e1000e", "rtl8139", "vmxnet3", "ne2k_pci", "pcnet"}

// parseQemuNet extracts the NIC model, bridge, and VLAN tag from a QEMU
// "netN" config value, e.g. "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0,tag=10".
func parseQemuNet(value string) (model, bridge, tag string) {
	kv := parseCSVKV(value)
	bridge = kv["bridge"]
	tag = kv["tag"]
	for _, m := range qemuNetModels {
		if _, ok := kv[m]; ok {
			model = m
			break
		}
	}
	if model == "" {
		model = "virtio"
	}
	return model, bridge, tag
}

// parseLXCNet extracts the interface name, bridge, and static/DHCP IP from
// an LXC "netN" config value, e.g. "name=eth0,bridge=vmbr0,ip=dhcp".
func parseLXCNet(value string) (name, bridge, ip string) {
	kv := parseCSVKV(value)
	return kv["name"], kv["bridge"], kv["ip"]
}

// primaryNet returns the guest's first network device, if any.
func primaryNet(nets []pve.NetDevice) (pve.NetDevice, bool) {
	if len(nets) == 0 {
		return pve.NetDevice{}, false
	}
	return nets[0], true
}

func quote(s string) string {
	return fmt.Sprintf("%q", s)
}
