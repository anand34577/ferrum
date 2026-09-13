package export

import (
	"strings"
	"testing"

	"ferrum/internal/pve"
)

func TestGenerateAnsibleInventory(t *testing.T) {
	guests := []Guest{
		{
			Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 100, Name: "web01", Tags: "prod;web"},
			IP:       "192.168.1.50",
		},
		{
			Resource: pve.ClusterResource{Type: "lxc", Node: "pve1", VMID: 101, Name: "db-01", Tags: "prod"},
			// No IP — agent not installed/reporting.
		},
		{
			Resource: pve.ClusterResource{Type: "qemu", Node: "pve2", VMID: 102, Name: "app01", Tags: "prod,app"},
			IP:       "192.168.1.60",
		},
		{
			// A storage/pool row should never show up as a host.
			Resource: pve.ClusterResource{Type: "storage", Node: "pve1", Name: "local-lvm"},
		},
	}

	out := GenerateAnsibleInventory("prod-cluster", guests)

	for _, want := range []string{
		"node_pve1:",
		"node_pve2:",
		"tag_prod:",
		"tag_web:",
		"tag_app:",
		"web01:",
		"ansible_host: 192.168.1.50",
		"db-01: {}", // no IP known -> empty mapping, no ansible_host
		"app01:",
		"ansible_host: 192.168.1.60",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}

	if strings.Contains(out, "local-lvm:\n") || strings.Contains(out, "local-lvm: {}") {
		t.Error("storage resource should not appear as an inventory host")
	}
}

func TestGenerateAnsibleInventoryEmpty(t *testing.T) {
	out := GenerateAnsibleInventory("empty-cluster", nil)
	if !strings.Contains(out, "all:\n  hosts: {}\n") {
		t.Errorf("expected an empty hosts stanza for no guests, got:\n%s", out)
	}
}

func TestGenerateAnsibleInventoryDuplicateNames(t *testing.T) {
	guests := []Guest{
		{Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 100, Name: "web"}},
		{Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 200, Name: "web"}},
	}
	out := GenerateAnsibleInventory("cluster", guests)
	if !strings.Contains(out, "web:") {
		t.Errorf("expected first guest to keep the plain name, got:\n%s", out)
	}
	if !strings.Contains(out, "web-200:") {
		t.Errorf("expected second guest disambiguated by vmid, got:\n%s", out)
	}
}

func TestGenerateAnsibleInventoryRejectsMaliciousIP(t *testing.T) {
	guests := []Guest{
		{
			Resource: pve.ClusterResource{Type: "qemu", Node: "pve1", VMID: 100, Name: "evil"},
			IP:       "10.0.0.5\n  ansible_python_interpreter: /tmp/x",
		},
		{
			// A CIDR suffix is agent-reported too (LXC) — strip, don't write it.
			Resource: pve.ClusterResource{Type: "lxc", Node: "pve1", VMID: 101, Name: "cidr"},
			IP:       "192.168.1.50/24",
		},
	}
	out := GenerateAnsibleInventory("cluster", guests)

	if strings.Contains(out, "ansible_python_interpreter") {
		t.Errorf("injected YAML made it into the inventory:\n%s", out)
	}
	if !strings.Contains(out, "evil: {}") {
		t.Errorf("host with an invalid IP should fall back to an empty mapping:\n%s", out)
	}
	if strings.Contains(out, "ansible_host: 10.0.0.5") {
		t.Errorf("malformed IP should never be written as ansible_host:\n%s", out)
	}
	if !strings.Contains(out, "ansible_host: 192.168.1.50\n") {
		t.Errorf("valid CIDR address should be stripped to its bare IP:\n%s", out)
	}
}
