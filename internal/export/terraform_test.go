package export

import (
	"strings"
	"testing"

	"ferrum/internal/pve"
)

func TestGenerateTerraform(t *testing.T) {
	guests := []Guest{
		{
			Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 100, Name: "web01", Status: "running", Tags: "prod;web"},
			Config: &pve.GuestConfig{
				Name: "web01", Cores: 4, Sockets: 1, Memory: 4096, Onboot: 1,
				Disks:          []pve.DiskDevice{{Key: "scsi0", Value: "local-lvm:vm-100-disk-0,size=32G,ssd=1"}},
				NetworkDevices: []pve.NetDevice{{Key: "net0", Value: "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0,tag=10"}},
			},
		},
		{
			Resource: pve.ClusterResource{Type: "lxc", Node: "pve2", VMID: 101, Name: "db-01", Status: "running", Tags: "prod"},
			Config: &pve.GuestConfig{
				Name: "db-01", Cores: 2, Memory: 2048, Onboot: 0,
				Disks:          []pve.DiskDevice{{Key: "rootfs", Value: "local-lvm:vm-101-disk-0,size=16G"}},
				NetworkDevices: []pve.NetDevice{{Key: "net0", Value: "name=eth0,bridge=vmbr1,ip=dhcp"}},
			},
		},
		{
			// No config at all — must still render a minimal, valid block.
			Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 102, Name: "bare", Status: "stopped"},
		},
	}

	out := GenerateTerraform("prod-cluster", guests)

	for _, want := range []string{
		`resource "proxmox_vm_qemu" "vm_100_web01"`,
		`target_node = "pve1"`,
		`vmid        = 100`,
		`cores       = 4`,
		`memory      = 4096`,
		`onboot      = true`,
		`tags        = "prod;web"`,
		`storage = "local-lvm"`,
		`size    = "32G"`,
		`model  = "virtio"`,
		`bridge = "vmbr0"`,
		`tag    = 10`,

		`resource "proxmox_lxc" "ct_101_db-01"`,
		`hostname     = "db-01"`,
		`cores        = 2`,
		`memory       = 2048`,
		`storage = "local-lvm"`,
		`size    = "16G"`,
		`name   = "eth0"`,
		`bridge = "vmbr1"`,
		`ip     = "dhcp"`,

		`resource "proxmox_vm_qemu" "vm_102_bare"`,
		"no primary disk detected",
		"no network interface detected",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}

	// VMID ordering must be stable (sorted).
	i100 := strings.Index(out, "vm_100_web01")
	i101 := strings.Index(out, "ct_101_db-01")
	i102 := strings.Index(out, "vm_102_bare")
	if !(i100 < i101 && i101 < i102) {
		t.Errorf("expected resources ordered by vmid, got positions 100=%d 101=%d 102=%d", i100, i101, i102)
	}
}

func TestParseDiskValue(t *testing.T) {
	tests := []struct {
		value       string
		wantStorage string
		wantSize    string
	}{
		{"local-lvm:vm-100-disk-0,size=32G,ssd=1", "local-lvm", "32G"},
		{"local-lvm:vm-100-disk-0,size=8G", "local-lvm", "8G"},
		{"nfs-backup:vm-100-disk-1", "nfs-backup", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		storage, size := parseDiskValue(tt.value)
		if storage != tt.wantStorage || size != tt.wantSize {
			t.Errorf("parseDiskValue(%q) = (%q, %q), want (%q, %q)", tt.value, storage, size, tt.wantStorage, tt.wantSize)
		}
	}
}
