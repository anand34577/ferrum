package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ferrum/internal/pve"
)

// --- Baseline capture/clear ---

// captureGuestBaseline snapshots a guest's current config + firewall rules
// and stores them as its drift baseline, replacing any previous baseline for
// the same guest (one baseline per connection/guest-type/vmid — see the
// UNIQUE constraint on config_baselines).
func (s *Server) captureGuestBaseline(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	cfg, err := client.GuestConfig(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	fwRules, err := client.GuestFirewallRules(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	fwJSON, err := json.Marshal(fwRules)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	var createdBy *string
	if u := userFromContext(r); u != nil {
		id := u.ID
		createdBy = &id
	}

	name := cfg.Name
	if name == "" {
		name = fmt.Sprintf("%s/%d", guestType, vmid)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO config_baselines (id, connection_id, guest_type, node, vmid, name, snapshot_json, firewall_snapshot_json, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (connection_id, guest_type, vmid) DO UPDATE SET
			node = excluded.node, name = excluded.name,
			snapshot_json = excluded.snapshot_json, firewall_snapshot_json = excluded.firewall_snapshot_json,
			created_by = excluded.created_by, created_at = excluded.created_at`,
		uuid.NewString(), connID, guestType, node, vmid, name, string(cfgJSON), string(fwJSON), createdBy, now,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.audit(r, "drift.baseline.capture", "guest", node+"/"+guestType+"/"+strconv.Itoa(vmid))
	writeJSON(w, http.StatusOK, map[string]string{"baselineAt": now})
}

func (s *Server) clearGuestBaseline(w http.ResponseWriter, r *http.Request) {
	connID, guestType := chi.URLParam(r, "id"), chi.URLParam(r, "type")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM config_baselines WHERE connection_id = ? AND guest_type = ? AND vmid = ?`,
		connID, guestType, vmid,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "drift.baseline.clear", "guest", chi.URLParam(r, "node")+"/"+guestType+"/"+strconv.Itoa(vmid))
	w.WriteHeader(http.StatusNoContent)
}

// --- Drift diff ---

// DriftChange is one field that differs between a baseline and the guest's
// live config.
type DriftChange struct {
	Field    string `json:"field"`
	Baseline string `json:"baseline"`
	Current  string `json:"current"`
}

type driftResponse struct {
	HasBaseline bool          `json:"hasBaseline"`
	BaselineAt  string        `json:"baselineAt,omitempty"`
	Drifted     bool          `json:"drifted"`
	Changes     []DriftChange `json:"changes,omitempty"`
}

type storedBaseline struct {
	name      string
	snapshot  string
	firewall  string
	createdAt string
}

func (s *Server) getGuestDrift(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	var b storedBaseline
	err = s.db.QueryRowContext(r.Context(),
		`SELECT name, snapshot_json, firewall_snapshot_json, created_at FROM config_baselines WHERE connection_id = ? AND guest_type = ? AND vmid = ?`,
		connID, guestType, vmid,
	).Scan(&b.name, &b.snapshot, &b.firewall, &b.createdAt)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, driftResponse{HasBaseline: false, Drifted: false})
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	var baselineCfg pve.GuestConfig
	var baselineFW []pve.FirewallRule
	if err := json.Unmarshal([]byte(b.snapshot), &baselineCfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := json.Unmarshal([]byte(b.firewall), &baselineFW); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	liveCfg, err := client.GuestConfig(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	liveFW, err := client.GuestFirewallRules(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	changes := diffGuestConfig(&baselineCfg, liveCfg, baselineFW, liveFW)
	writeJSON(w, http.StatusOK, driftResponse{
		HasBaseline: true,
		BaselineAt:  b.createdAt,
		Drifted:     len(changes) > 0,
		Changes:     changes,
	})
}

// diffGuestConfig is the pure comparison behind the drift endpoints — no
// network calls, so it's fully table-driven-testable. Deliberately limited
// to the fields that matter operationally: cores, memory, disk sizes,
// network bridges/VLANs, and firewall rule count/contents. Everything else
// in a guest's raw config (lock, digest, meta, ...) is noisy and changes on
// its own without meaning anything, so it's left out on purpose.
func diffGuestConfig(baseline, current *pve.GuestConfig, baselineFW, currentFW []pve.FirewallRule) []DriftChange {
	var changes []DriftChange

	if baseline.Cores != current.Cores {
		changes = append(changes, DriftChange{"cores", strconv.Itoa(baseline.Cores), strconv.Itoa(current.Cores)})
	}
	if baseline.Memory != current.Memory {
		changes = append(changes, DriftChange{"memory", strconv.Itoa(baseline.Memory), strconv.Itoa(current.Memory)})
	}

	changes = append(changes, diffDisks(baseline.Disks, current.Disks)...)
	changes = append(changes, diffNetworks(baseline.NetworkDevices, current.NetworkDevices)...)
	changes = append(changes, diffFirewall(baselineFW, currentFW)...)

	return changes
}

func diffDisks(baseline, current []pve.DiskDevice) []DriftChange {
	baseByKey := diskSizesByKey(baseline)
	curByKey := diskSizesByKey(current)
	var changes []DriftChange
	for _, key := range unionKeys(baseByKey, curByKey) {
		b, bok := baseByKey[key]
		c, cok := curByKey[key]
		if !bok {
			b = "(none)"
		}
		if !cok {
			c = "(none)"
		}
		if b != c {
			changes = append(changes, DriftChange{"disk:" + key, b, c})
		}
	}
	return changes
}

func diskSizesByKey(disks []pve.DiskDevice) map[string]string {
	out := make(map[string]string, len(disks))
	for _, d := range disks {
		kv := parseKV(d.Value)
		size, ok := kv["size"]
		if !ok {
			size = "(unspecified)"
		}
		out[d.Key] = size
	}
	return out
}

func diffNetworks(baseline, current []pve.NetDevice) []DriftChange {
	baseByKey := netSummaryByKey(baseline)
	curByKey := netSummaryByKey(current)
	var changes []DriftChange
	for _, key := range unionKeys(baseByKey, curByKey) {
		b, bok := baseByKey[key]
		c, cok := curByKey[key]
		if !bok {
			b = "(none)"
		}
		if !cok {
			c = "(none)"
		}
		if b != c {
			changes = append(changes, DriftChange{"network:" + key, b, c})
		}
	}
	return changes
}

func netSummaryByKey(nets []pve.NetDevice) map[string]string {
	out := make(map[string]string, len(nets))
	for _, n := range nets {
		kv := parseKV(n.Value)
		bridge := kv["bridge"]
		if bridge == "" {
			bridge = "(no bridge)"
		}
		vlan := kv["tag"]
		if vlan == "" {
			vlan = "untagged"
		}
		out[n.Key] = fmt.Sprintf("bridge=%s,vlan=%s", bridge, vlan)
	}
	return out
}

// parseKV parses a PVE comma-separated config value string (e.g.
// "local-lvm:vm-100-disk-0,size=32G" or "virtio=BC:24:11:AA,bridge=vmbr0,tag=10")
// into its key=value tokens. Leading positional tokens without "=" (the
// volid on a disk, the model=mac pair's own key on a net device is already
// "key=value" so it's captured normally) are simply skipped.
func parseKV(value string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func unionKeys(a, b map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func diffFirewall(baseline, current []pve.FirewallRule) []DriftChange {
	if len(baseline) != len(current) {
		return []DriftChange{{"firewall.ruleCount", strconv.Itoa(len(baseline)), strconv.Itoa(len(current))}}
	}
	baseSigs := firewallSignatures(baseline)
	curSigs := firewallSignatures(current)
	baseJoined := strings.Join(baseSigs, "; ")
	curJoined := strings.Join(curSigs, "; ")
	if baseJoined != curJoined {
		return []DriftChange{{"firewall.rules", baseJoined, curJoined}}
	}
	return nil
}

// firewallSignatures reduces each rule to the fields that determine its
// actual effect (position and comment don't) and returns them sorted, so two
// rule sets with the same rules in a different order compare as identical.
func firewallSignatures(rules []pve.FirewallRule) []string {
	sigs := make([]string, 0, len(rules))
	for _, r := range rules {
		sigs = append(sigs, fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s",
			r.Type, r.Action, r.Source, r.Dest, r.Proto, r.Dport, r.Sport, r.Macro))
	}
	sort.Strings(sigs)
	return sigs
}

// --- Fleet-wide summary ---

type driftSummaryEntry struct {
	ConnectionID   string `json:"connectionId"`
	ConnectionName string `json:"connectionName"`
	GuestType      string `json:"guestType"`
	Node           string `json:"node"`
	VMID           int    `json:"vmid"`
	Name           string `json:"name"`
	BaselineAt     string `json:"baselineAt"`
	ChangeCount    int    `json:"changeCount"`
}

// driftSummary reports every guest, across every connection, that has a
// baseline AND is currently drifted from it. Fans out per-connection the
// same bounded-concurrency way the fleet overview and inventory endpoints
// do — one slow/unreachable connection shouldn't stall the whole summary.
func (s *Server) driftSummary(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name FROM connections ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	type conn struct{ ID, Name string }
	var conns []conn
	for rows.Next() {
		var c conn
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			rows.Close()
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		conns = append(conns, c)
	}
	rows.Close()

	results := make([][]driftSummaryEntry, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = s.driftedGuestsForConnection(r.Context(), c.ID, c.Name)
		}(i, c)
	}
	wg.Wait()

	out := []driftSummaryEntry{}
	for _, r := range results {
		out = append(out, r...)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) driftedGuestsForConnection(ctx context.Context, connID, connName string) []driftSummaryEntry {
	rows, err := s.db.QueryContext(ctx, `
		SELECT guest_type, node, vmid, name, snapshot_json, firewall_snapshot_json, created_at
		FROM config_baselines WHERE connection_id = ?`, connID)
	if err != nil {
		return nil
	}
	type baselineRow struct {
		guestType, node, name, snapshot, firewall, createdAt string
		vmid                                                 int
	}
	var baselines []baselineRow
	for rows.Next() {
		var b baselineRow
		if err := rows.Scan(&b.guestType, &b.node, &b.vmid, &b.name, &b.snapshot, &b.firewall, &b.createdAt); err != nil {
			rows.Close()
			return nil
		}
		baselines = append(baselines, b)
	}
	rows.Close()
	if len(baselines) == 0 {
		return nil
	}

	client, err := s.clientFor(ctx, connID)
	if err != nil {
		return nil // connection unreachable — nothing to report for it this pass
	}

	var out []driftSummaryEntry
	for _, b := range baselines {
		var baselineCfg pve.GuestConfig
		var baselineFW []pve.FirewallRule
		if err := json.Unmarshal([]byte(b.snapshot), &baselineCfg); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(b.firewall), &baselineFW); err != nil {
			continue
		}
		liveCfg, err := client.GuestConfig(ctx, b.guestType, b.node, b.vmid)
		if err != nil {
			continue // guest unreachable/deleted this pass — skip rather than fail the whole summary
		}
		liveFW, err := client.GuestFirewallRules(ctx, b.guestType, b.node, b.vmid)
		if err != nil {
			continue
		}
		changes := diffGuestConfig(&baselineCfg, liveCfg, baselineFW, liveFW)
		if len(changes) == 0 {
			continue
		}
		out = append(out, driftSummaryEntry{
			ConnectionID: connID, ConnectionName: connName,
			GuestType: b.guestType, Node: b.node, VMID: b.vmid, Name: b.name,
			BaselineAt: b.createdAt, ChangeCount: len(changes),
		})
	}
	return out
}
