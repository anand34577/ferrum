package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// bulkTarget identifies one guest, on one connection, to act on. Targets in
// a single request can span any mix of connections/clusters — fanning an
// action out across independent Proxmox instances in one call is the
// capability this endpoint adds over the existing single-guest actions.
type bulkTarget struct {
	ConnID string `json:"connId"`
	Type   string `json:"type"` // "qemu" | "lxc"
	Node   string `json:"node"`
	VMID   int    `json:"vmid"`
}

// valid reports whether t has every field a real target needs.
func (t bulkTarget) valid() bool {
	return t.ConnID != "" && (t.Type == "qemu" || t.Type == "lxc") && t.Node != "" && t.VMID != 0
}

// bulkActionRequest is the body of POST /api/v1/bulk/guests/action.
type bulkActionRequest struct {
	Targets []bulkTarget `json:"targets"`
	Action  string       `json:"action"` // start|stop|shutdown|reboot|suspend|resume|snapshot|delete|tag

	// Action-specific parameters.
	SnapshotName string `json:"snapshotName,omitempty"` // required for action=="snapshot"
	Tags         string `json:"tags,omitempty"`         // used for action=="tag" (empty clears tags)
	PurgeJobs    bool   `json:"purgeJobs,omitempty"`    // optional for action=="delete"
}

// maxBulkTargets bounds one request's target list — a body larger than that
// is abuse or a client bug, not a real bulk operation.
const maxBulkTargets = 500

// bulkActionResult reports one target's outcome. Results are collected into
// an array (not failed-fast) so a partial failure across a large or mixed
// target set is visible per-guest instead of silently swallowed or aborting
// every other target.
type bulkActionResult struct {
	ConnID  string `json:"connId"`
	Type    string `json:"type"`
	Node    string `json:"node"`
	VMID    int    `json:"vmid"`
	Success bool   `json:"success"`
	UPID    string `json:"upid,omitempty"`
	Error   string `json:"error,omitempty"`
}

// bulkPowerActions are the plain power actions forwarded as-is to
// pve.Client.GuestPowerAction. Deliberately excludes "reset" (present on the
// single-guest endpoint) — not part of the action set this endpoint's spec
// calls for.
var bulkPowerActions = map[string]bool{
	"start": true, "stop": true, "shutdown": true, "reboot": true, "suspend": true, "resume": true,
}

var bulkNonPowerActions = map[string]bool{"snapshot": true, "delete": true, "tag": true}

// bulkGuestAction executes one action across a set of guests spanning
// potentially many different connections/clusters at once, collecting a
// per-target success/failure result — POST /api/v1/bulk/guests/action.
func (s *Server) bulkGuestAction(w http.ResponseWriter, r *http.Request) {
	var req bulkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Targets) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "targets is required")
		return
	}
	if len(req.Targets) > maxBulkTargets {
		writeErrorMsg(w, http.StatusBadRequest, "too many targets (max 500)")
		return
	}
	if !bulkPowerActions[req.Action] && !bulkNonPowerActions[req.Action] {
		writeErrorMsg(w, http.StatusBadRequest, "unsupported action")
		return
	}
	if req.Action == "snapshot" && req.SnapshotName == "" {
		writeErrorMsg(w, http.StatusBadRequest, "snapshotName is required for action=snapshot")
		return
	}

	// Fan out across targets concurrently, same bound as the fleet
	// overview/inventory fan-outs — targets can hit many different
	// connections (or repeatedly hit one), and each is an independent
	// network call.
	out := make([]bulkActionResult, len(req.Targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, t := range req.Targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t bulkTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = s.runBulkAction(r, t, req)
		}(i, t)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, out)
}

// runBulkAction executes req.Action against one target, translating any
// failure into a result entry rather than an error return so the caller's
// fan-out loop never has to special-case a failed target.
func (s *Server) runBulkAction(r *http.Request, t bulkTarget, req bulkActionRequest) bulkActionResult {
	res := bulkActionResult{ConnID: t.ConnID, Type: t.Type, Node: t.Node, VMID: t.VMID}
	if !t.valid() {
		res.Error = "invalid target"
		return res
	}
	// Mirrors guestPowerAction's guard in inventory.go: LXC suspend/resume
	// (criu checkpoint) is unreliable enough that PVE just returns a generic
	// 5xx for it.
	if t.Type == "lxc" && (req.Action == "suspend" || req.Action == "resume") {
		res.Error = "suspend/resume is not supported for containers"
		return res
	}

	client, err := s.clientFor(r.Context(), t.ConnID)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	target := fmt.Sprintf("%s/%s/%d", t.Node, t.Type, t.VMID)
	var upid string
	var auditAction string
	switch {
	case bulkPowerActions[req.Action]:
		upid, err = client.GuestPowerAction(r.Context(), t.Type, t.Node, t.VMID, req.Action)
		auditAction = "vm." + req.Action
	case req.Action == "snapshot":
		upid, err = client.CreateSnapshot(r.Context(), t.Type, t.Node, t.VMID, req.SnapshotName, "", false)
		auditAction = "vm.snapshot.create"
	case req.Action == "delete":
		upid, err = client.DeleteGuest(r.Context(), t.Type, t.Node, t.VMID, req.PurgeJobs)
		auditAction = "vm.delete"
	case req.Action == "tag":
		upid, err = client.UpdateGuestConfig(r.Context(), t.Type, t.Node, t.VMID, url.Values{"tags": {req.Tags}})
		auditAction = "vm.tags.update"
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Success = true
	res.UPID = upid
	// Audit every individual action, same as the single-guest equivalents —
	// see e.g. guestPowerAction in inventory.go.
	s.audit(r, auditAction, "vm", target)
	return res
}
