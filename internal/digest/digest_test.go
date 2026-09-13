package digest

import (
	"strings"
	"testing"
	"time"

	"ferrum/internal/pve"
)

func TestRender(t *testing.T) {
	generated := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		summary     FleetSummary
		wantSubject string
		wantBody    []string // substrings expected in the rendered body
	}{
		{
			name: "healthy fleet with a backup and an alert",
			summary: FleetSummary{
				GeneratedAt:          generated,
				TotalConnections:     2,
				ReachableConnections: 2,
				TotalNodes:           5,
				OnlineNodes:          5,
				TotalGuests:          20,
				RunningGuests:        17,
				AvgCPUPct:            42.5,
				AvgMemPct:            60.1,
				AvgStoragePct:        71.0,
				AlertsCritical:       1,
				AlertsWarning:        3,
				BackupTotal:          10,
				BackupOK:             9,
				Connections: []ConnectionSummary{
					{
						Name: "prod-cluster", Reachable: true,
						Nodes: 3, OnlineNodes: 3, Guests: 15, RunningGuests: 13,
						CPUPct: 45, MemPct: 62, StoragePct: 70,
						BackupTotal: 7, BackupOK: 7,
					},
					{
						Name: "edge-01", Error: "context deadline exceeded", Reachable: false,
					},
				},
			},
			wantSubject: "Ferrum fleet digest — 2026-09-10",
			wantBody: []string{
				"Connections:    2/2 reachable",
				"Nodes:          5/5 online",
				"Guests:         17/20 running",
				"CPU 42.5%",
				"Memory 60.1%",
				"Storage 71.0%",
				"Backups (7d):   9/10 succeeded (90%)",
				"Active alerts:  1 critical, 3 warning",
				"prod-cluster: 3/3 nodes online, 13/15 guests running",
				"backups 7/7 ok",
				"edge-01: UNREACHABLE (context deadline exceeded)",
			},
		},
		{
			name: "no backups observed",
			summary: FleetSummary{
				GeneratedAt:          generated,
				TotalConnections:     1,
				ReachableConnections: 1,
				Connections: []ConnectionSummary{
					{Name: "solo", Reachable: true, Nodes: 1, OnlineNodes: 1},
				},
			},
			wantSubject: "Ferrum fleet digest — 2026-09-10",
			wantBody: []string{
				"Backups (7d):   no vzdump tasks observed",
				"Active alerts:  0 critical, 0 warning",
			},
		},
		{
			name: "pbs rows report reachability, not node counts",
			summary: FleetSummary{
				GeneratedAt:          generated,
				TotalConnections:     2,
				ReachableConnections: 2,
				Connections: []ConnectionSummary{
					{Name: "backup-server", Type: "pbs", Reachable: true, Datastores: 3},
					{Name: "dead-pbs", Type: "pbs", Error: "connection refused", Reachable: false},
				},
			},
			wantSubject: "Ferrum fleet digest — 2026-09-10",
			wantBody: []string{
				// A reachable PBS server must not render as "0/0 nodes online"
				// — it isn't a PVE cluster, so it gets its own line.
				"backup-server: PBS backup server reachable (3 datastores)",
				"dead-pbs: UNREACHABLE (connection refused)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := tt.summary.Render()
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
			for _, want := range tt.wantBody {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q\nfull body:\n%s", want, body)
				}
			}
		})
	}
}

func TestStoragePct(t *testing.T) {
	// storagePct is exercised indirectly through Build in production, but a
	// direct table-driven check keeps the aggregation logic itself under
	// test without needing a live/mocked PVE client.
	tests := []struct {
		name string
		res  []pve.ClusterResource
		want float64
	}{
		{name: "empty", res: nil, want: 0},
		{
			name: "half used",
			res:  []pve.ClusterResource{{Type: "storage", MaxDisk: 100, Disk: 50}},
			want: 50,
		},
		{
			name: "zero capacity ignored",
			res: []pve.ClusterResource{
				{Type: "storage", MaxDisk: 0, Disk: 0},
				{Type: "storage", MaxDisk: 200, Disk: 100},
			},
			want: 50,
		},
		{
			name: "non-storage rows ignored",
			res: []pve.ClusterResource{
				{Type: "node", MaxDisk: 999, Disk: 999},
				{Type: "storage", MaxDisk: 100, Disk: 25},
			},
			want: 25,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storagePct(tt.res)
			if got != tt.want {
				t.Errorf("storagePct() = %v, want %v", got, tt.want)
			}
		})
	}
}
