package api

import (
	"testing"

	"ferrum/internal/pve"
)

func TestMatchResourcesRanksExactVMIDThenNamePrefixThenSubstring(t *testing.T) {
	resources := []pve.ClusterResource{
		{Type: "qemu", VMID: 100, Name: "webserver", Node: "pve-01", Tags: "prod"},
		{Type: "lxc", VMID: 200, Name: "web-cache", Node: "pve-02", Tags: ""},
		{Type: "qemu", VMID: 300, Name: "database", Node: "pve-01", Tags: "web-team"},
		{Type: "node", VMID: 0, Name: "pve-01"}, // non-guest row, must never match
	}

	got := matchResources("c1", "Cluster One", resources, "web")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3 (webserver prefix, web-cache prefix, database via tag substring)", len(got))
	}
	// Both "webserver" and "web-cache" are name-prefix matches (rank 1);
	// "database" only matches via its tag (rank 2) and must sort after them.
	if got[0].rank != 1 || got[1].rank != 1 {
		t.Errorf("expected the two name-prefix matches ranked first, got ranks %d, %d", got[0].rank, got[1].rank)
	}
	if got[2].Name != "database" || got[2].rank != 2 {
		t.Errorf("expected database (tag substring match) last, got %+v", got[2])
	}
}

func TestMatchResourcesExactVMIDOutranksNameMatch(t *testing.T) {
	resources := []pve.ClusterResource{
		{Type: "qemu", VMID: 100, Name: "100-and-something", Node: "pve-01"},
		{Type: "qemu", VMID: 999, Name: "100", Node: "pve-01"}, // name is literally "100" but vmid isn't
	}
	got := matchResources("c1", "Cluster One", resources, "100")
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].VMID != 100 || got[0].rank != 0 {
		t.Errorf("expected exact vmid match (100) ranked first, got %+v", got[0])
	}
}

func TestMatchResourcesNonMatchingQueryReturnsNothing(t *testing.T) {
	resources := []pve.ClusterResource{
		{Type: "qemu", VMID: 100, Name: "webserver", Node: "pve-01", Tags: "prod"},
	}
	got := matchResources("c1", "Cluster One", resources, "nonexistent")
	if len(got) != 0 {
		t.Fatalf("got %d results, want 0", len(got))
	}
}
