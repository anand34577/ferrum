package api

import (
	"testing"

	"ferrum/internal/pve"
)

func TestDedupeSharedStorageCollapsesSameVolumeAcrossNodes(t *testing.T) {
	// An NFS share mounted on 4 nodes without PVE's "Shared" checkbox ticked
	// (Shared: 0) — cluster/resources reports it once per node, byte-for-byte
	// identical each time. Must collapse to one.
	rows := []pve.ClusterResource{
		{Node: "pve-01", Storage: "nas", PluginType: "nfs", Shared: 0, MaxDisk: 2_000_000_000_000, Disk: 1_000_000_000_000},
		{Node: "pve-02", Storage: "nas", PluginType: "nfs", Shared: 0, MaxDisk: 2_000_000_000_000, Disk: 1_000_000_000_000},
		{Node: "pve-03", Storage: "nas", PluginType: "nfs", Shared: 0, MaxDisk: 2_000_000_000_000, Disk: 1_000_000_000_000},
		{Node: "pve-04", Storage: "nas", PluginType: "nfs", Shared: 0, MaxDisk: 2_000_000_000_000, Disk: 1_000_000_000_000},
	}
	got := dedupeSharedStorage(rows)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 (deduped)", len(got))
	}
	if got[0].MaxDisk != 2_000_000_000_000 {
		t.Errorf("MaxDisk = %d, want the single volume's real capacity, not summed across nodes", got[0].MaxDisk)
	}
}

func TestDedupeSharedStorageCollapsesProperlyFlaggedShared(t *testing.T) {
	rows := []pve.ClusterResource{
		{Node: "pve-01", Storage: "pbs-backup", PluginType: "pbs", Shared: 1, MaxDisk: 5_000_000_000_000, Disk: 2_000_000_000_000},
		{Node: "pve-02", Storage: "pbs-backup", PluginType: "pbs", Shared: 1, MaxDisk: 5_000_000_000_000, Disk: 2_000_000_000_000},
	}
	got := dedupeSharedStorage(rows)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 (deduped)", len(got))
	}
}

func TestDedupeSharedStorageKeepsGenuinelySeparateLocalDisks(t *testing.T) {
	// Same name ("local-lvm"), different nodes, different usage — this is
	// real per-node capacity that must be summed, not deduped away.
	rows := []pve.ClusterResource{
		{Node: "pve-01", Storage: "local-lvm", PluginType: "lvmthin", Shared: 0, MaxDisk: 1_000_000_000_000, Disk: 300_000_000_000},
		{Node: "pve-02", Storage: "local-lvm", PluginType: "lvmthin", Shared: 0, MaxDisk: 1_000_000_000_000, Disk: 450_000_000_000},
	}
	got := dedupeSharedStorage(rows)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (local disks are never deduped)", len(got))
	}
}

func TestDedupeSharedStorageKeepsDistinctVolumesEvenWithMatchingCapacity(t *testing.T) {
	// Two different NFS exports that happen to be identically sized but
	// currently hold different amounts of data — a real coincidence in
	// total size alone isn't enough to treat them as the same volume.
	rows := []pve.ClusterResource{
		{Node: "pve-01", Storage: "nas-a", PluginType: "nfs", Shared: 0, MaxDisk: 1_000_000_000_000, Disk: 100_000_000_000},
		{Node: "pve-01", Storage: "nas-b", PluginType: "nfs", Shared: 0, MaxDisk: 1_000_000_000_000, Disk: 200_000_000_000},
	}
	got := dedupeSharedStorage(rows)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (different names, never the same volume)", len(got))
	}
}
