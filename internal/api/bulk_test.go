package api

import "testing"

func TestBulkTargetValid(t *testing.T) {
	cases := []struct {
		name string
		t    bulkTarget
		want bool
	}{
		{"valid qemu", bulkTarget{ConnID: "c1", Type: "qemu", Node: "pve-01", VMID: 100}, true},
		{"valid lxc", bulkTarget{ConnID: "c1", Type: "lxc", Node: "pve-01", VMID: 100}, true},
		{"missing connId", bulkTarget{Type: "qemu", Node: "pve-01", VMID: 100}, false},
		{"missing node", bulkTarget{ConnID: "c1", Type: "qemu", VMID: 100}, false},
		{"zero vmid", bulkTarget{ConnID: "c1", Type: "qemu", Node: "pve-01"}, false},
		{"bad type", bulkTarget{ConnID: "c1", Type: "storage", Node: "pve-01", VMID: 100}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.t.valid(); got != c.want {
				t.Errorf("valid() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBulkPowerActionsExcludesReset(t *testing.T) {
	// The single-guest power endpoint (allowedPowerActions in inventory.go)
	// includes "reset"; the bulk action set deliberately doesn't.
	if bulkPowerActions["reset"] {
		t.Error(`bulkPowerActions should not include "reset"`)
	}
	for _, a := range []string{"start", "stop", "shutdown", "reboot", "suspend", "resume"} {
		if !bulkPowerActions[a] {
			t.Errorf("bulkPowerActions missing %q", a)
		}
	}
}
