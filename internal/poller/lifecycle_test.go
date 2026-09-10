package poller

import (
	"testing"
	"time"

	"ferrum/internal/pve"
)

func TestTagRetentionDays(t *testing.T) {
	cases := []struct {
		name     string
		tags     string
		wantDays int
		wantOK   bool
	}{
		{"empty", "", 0, false},
		{"no retain tag", "prod;webserver", 0, false},
		{"semicolon separated", "prod;retain:7d;webserver", 7, true},
		{"comma separated", "prod,retain:30d", 30, true},
		{"exact match only", "not-retain:7d", 0, false},
		{"zero days ignored", "retain:0d", 0, false},
		{"first valid match wins", "retain:3d;retain:9d", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			days, ok := tagRetentionDays(tc.tags)
			if ok != tc.wantOK || days != tc.wantDays {
				t.Errorf("tagRetentionDays(%q) = (%d, %v), want (%d, %v)", tc.tags, days, ok, tc.wantDays, tc.wantOK)
			}
		})
	}
}

func TestSnapshotsToRetire(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour).Unix()   // 10 days old
	recent := now.Add(-2 * 24 * time.Hour).Unix() // 2 days old

	snaps := []pve.Snapshot{
		{Name: "current"}, // pseudo-entry, never retired
		{Name: "before-upgrade", SnapTime: old},
		{Name: "yesterday", SnapTime: recent},
		{Name: "no-time"}, // SnapTime 0, must never be treated as "infinitely old"
	}

	t.Run("policy disabled (0 days)", func(t *testing.T) {
		got := snapshotsToRetire(snaps, 0, now)
		if len(got) != 0 {
			t.Fatalf("expected no snapshots retired with a disabled policy, got %v", got)
		}
	})

	t.Run("7 day window retires only the old one", func(t *testing.T) {
		got := snapshotsToRetire(snaps, 7, now)
		if len(got) != 1 || got[0].Name != "before-upgrade" {
			t.Fatalf("expected exactly [before-upgrade], got %v", got)
		}
	})

	t.Run("1 day window still spares the pseudo/no-time entries", func(t *testing.T) {
		got := snapshotsToRetire(snaps, 1, now)
		names := map[string]bool{}
		for _, s := range got {
			names[s.Name] = true
		}
		if names["current"] || names["no-time"] {
			t.Fatalf("pseudo/no-time entries must never be retired, got %v", got)
		}
		if !names["before-upgrade"] || !names["yesterday"] {
			t.Fatalf("expected both real snapshots retired under a 1 day window, got %v", got)
		}
	})
}

func TestOrphanedVolumes(t *testing.T) {
	items := []pve.StorageContentItem{
		{VolID: "local-lvm:vm-100-disk-0", Content: "images"},
		{VolID: "local-lvm:vm-101-disk-0", Content: "images"},
		{VolID: "local-lvm:subvol-102-disk-0", Content: "rootdir"},
		{VolID: "local:iso/ubuntu.iso", Content: "iso"},                // not guest-attached content, never orphaned
		{VolID: "local:backup/vzdump-qemu-100.vma", Content: "backup"}, // same
	}

	t.Run("nothing referenced -> both images/rootdir flagged, iso/backup never", func(t *testing.T) {
		got := orphanedVolumes(map[string]bool{}, items)
		if len(got) != 3 {
			t.Fatalf("expected 3 orphans, got %d: %v", len(got), got)
		}
		for _, o := range got {
			if o.Content != "images" && o.Content != "rootdir" {
				t.Errorf("non-guest-disk content flagged as orphan: %+v", o)
			}
		}
	})

	t.Run("fully referenced -> no orphans", func(t *testing.T) {
		referenced := map[string]bool{
			"local-lvm:vm-100-disk-0":     true,
			"local-lvm:vm-101-disk-0":     true,
			"local-lvm:subvol-102-disk-0": true,
		}
		got := orphanedVolumes(referenced, items)
		if len(got) != 0 {
			t.Fatalf("expected no orphans, got %v", got)
		}
	})

	t.Run("one referenced -> exactly the other two flagged", func(t *testing.T) {
		referenced := map[string]bool{"local-lvm:vm-100-disk-0": true}
		got := orphanedVolumes(referenced, items)
		if len(got) != 2 {
			t.Fatalf("expected 2 orphans, got %d: %v", len(got), got)
		}
	})
}
