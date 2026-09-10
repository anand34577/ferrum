package api

import (
	"testing"

	"ferrum/internal/pve"
)

func baseGuestConfig() *pve.GuestConfig {
	return &pve.GuestConfig{
		Name:   "web-01",
		Cores:  2,
		Memory: 2048,
		Disks: []pve.DiskDevice{
			{Key: "scsi0", Value: "local-lvm:vm-100-disk-0,size=32G"},
		},
		NetworkDevices: []pve.NetDevice{
			{Key: "net0", Value: "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0,tag=10"},
		},
	}
}

func baseFirewallRules() []pve.FirewallRule {
	return []pve.FirewallRule{
		{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22"},
	}
}

func TestDiffGuestConfig_NoChange(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 0 {
		t.Fatalf("expected no drift for identical config, got %v", changes)
	}
}

func TestDiffGuestConfig_CoresChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.Cores = 4

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 {
		t.Fatalf("expected exactly 1 change, got %d: %v", len(changes), changes)
	}
	if changes[0].Field != "cores" || changes[0].Baseline != "2" || changes[0].Current != "4" {
		t.Fatalf("unexpected change: %+v", changes[0])
	}
}

func TestDiffGuestConfig_MemoryChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.Memory = 4096

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 || changes[0].Field != "memory" {
		t.Fatalf("expected exactly 1 memory change, got %v", changes)
	}
}

func TestDiffGuestConfig_DiskSizeChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.Disks = []pve.DiskDevice{
		{Key: "scsi0", Value: "local-lvm:vm-100-disk-0,size=64G"},
	}

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 {
		t.Fatalf("expected exactly 1 change, got %d: %v", len(changes), changes)
	}
	if changes[0].Field != "disk:scsi0" || changes[0].Baseline != "32G" || changes[0].Current != "64G" {
		t.Fatalf("unexpected change: %+v", changes[0])
	}
}

func TestDiffGuestConfig_DiskAddedAndRemoved(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.Disks = append(current.Disks, pve.DiskDevice{Key: "scsi1", Value: "local-lvm:vm-100-disk-1,size=10G"})

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 || changes[0].Field != "disk:scsi1" || changes[0].Baseline != "(none)" || changes[0].Current != "10G" {
		t.Fatalf("expected 1 disk-added change, got %v", changes)
	}

	// Reverse: baseline has the extra disk, current doesn't (removed).
	changes = diffGuestConfig(current, baseline, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 || changes[0].Field != "disk:scsi1" || changes[0].Baseline != "10G" || changes[0].Current != "(none)" {
		t.Fatalf("expected 1 disk-removed change, got %v", changes)
	}
}

func TestDiffGuestConfig_NetworkBridgeChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.NetworkDevices = []pve.NetDevice{
		{Key: "net0", Value: "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr1,tag=10"},
	}

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 || changes[0].Field != "network:net0" {
		t.Fatalf("expected exactly 1 network change, got %v", changes)
	}
}

func TestDiffGuestConfig_NetworkVlanChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.NetworkDevices = []pve.NetDevice{
		{Key: "net0", Value: "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0,tag=20"},
	}

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 1 || changes[0].Field != "network:net0" {
		t.Fatalf("expected exactly 1 network change, got %v", changes)
	}
}

func TestDiffGuestConfig_FirewallRuleAdded(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	baselineFW := baseFirewallRules()
	currentFW := append(baseFirewallRules(), pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "80"})

	changes := diffGuestConfig(baseline, current, baselineFW, currentFW)
	if len(changes) != 1 || changes[0].Field != "firewall.ruleCount" {
		t.Fatalf("expected exactly 1 firewall.ruleCount change, got %v", changes)
	}
	if changes[0].Baseline != "1" || changes[0].Current != "2" {
		t.Fatalf("unexpected rule counts: %+v", changes[0])
	}
}

func TestDiffGuestConfig_FirewallRuleRemoved(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	baselineFW := append(baseFirewallRules(), pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "80"})
	currentFW := baseFirewallRules()

	changes := diffGuestConfig(baseline, current, baselineFW, currentFW)
	if len(changes) != 1 || changes[0].Field != "firewall.ruleCount" || changes[0].Baseline != "2" || changes[0].Current != "1" {
		t.Fatalf("expected 1 firewall.ruleCount change (2 -> 1), got %v", changes)
	}
}

func TestDiffGuestConfig_FirewallRuleChangedSameCount(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	baselineFW := []pve.FirewallRule{{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22"}}
	currentFW := []pve.FirewallRule{{Type: "in", Action: "DROP", Proto: "tcp", Dport: "22"}}

	changes := diffGuestConfig(baseline, current, baselineFW, currentFW)
	if len(changes) != 1 || changes[0].Field != "firewall.rules" {
		t.Fatalf("expected exactly 1 firewall.rules change, got %v", changes)
	}
}

func TestDiffGuestConfig_FirewallRuleOrderDoesNotCountAsDrift(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	baselineFW := []pve.FirewallRule{
		{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22"},
		{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "80"},
	}
	currentFW := []pve.FirewallRule{
		{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "80"},
		{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22"},
	}

	changes := diffGuestConfig(baseline, current, baselineFW, currentFW)
	if len(changes) != 0 {
		t.Fatalf("reordered-but-identical rule sets should not drift, got %v", changes)
	}
}

func TestDiffGuestConfig_MultipleFieldsChanged(t *testing.T) {
	baseline := baseGuestConfig()
	current := baseGuestConfig()
	current.Cores = 8
	current.Memory = 8192

	changes := diffGuestConfig(baseline, current, baseFirewallRules(), baseFirewallRules())
	if len(changes) != 2 {
		t.Fatalf("expected exactly 2 changes, got %d: %v", len(changes), changes)
	}
}
