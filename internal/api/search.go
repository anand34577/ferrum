package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ferrum/internal/pve"
)

// searchResult is one guest matched by a global cross-remote search —
// carries connection/type/node/vmid so the frontend can route straight to
// the result without a follow-up lookup.
type searchResult struct {
	ID             string `json:"id"` // PVE's own resource id (e.g. "qemu/100") — matches ClusterResource.ID, usable as InventoryPage's focusGuestId
	ConnectionID   string `json:"connectionId"`
	ConnectionName string `json:"connectionName"`
	Type           string `json:"type"` // "qemu" | "lxc"
	VMID           int    `json:"vmid"`
	Name           string `json:"name,omitempty"`
	Node           string `json:"node"`
	Tags           string `json:"tags,omitempty"`
	Status         string `json:"status,omitempty"`

	rank int // lower sorts first; internal only, not serialized
}

const searchMaxResults = 50

// globalSearch matches guest name/vmid/tags/node across every connection's
// already-fetched inventory — the same /cluster/resources call the fleet
// inventory view uses, so this is a fan-out of N *connections*, not a
// per-guest live query — GET /api/v1/search?q=.
//
// IP address is deliberately not searched: cluster/resources rows carry no
// IP field, and getting one requires a live per-guest agent call, which
// would turn this into an N-guest fan-out instead of an N-connection one.
func (s *Server) globalSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []searchResult{})
		return
	}

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

	resultsByConn := make([][]searchResult, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fleetFanoutLimit)
	for i, c := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c conn) {
			defer wg.Done()
			defer func() { <-sem }()
			client, err := s.clientFor(r.Context(), c.ID)
			if err != nil {
				return // unreachable connection: contributes no results
			}
			resources, err := client.ClusterResources(r.Context())
			if err != nil {
				return
			}
			resultsByConn[i] = matchResources(c.ID, c.Name, resources, q)
		}(i, c)
	}
	wg.Wait()

	var out []searchResult
	for _, rs := range resultsByConn {
		out = append(out, rs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > searchMaxResults {
		out = out[:searchMaxResults]
	}
	writeJSON(w, http.StatusOK, out)
}

// matchResources filters one connection's cached cluster/resources rows for
// guests matching q, ranking an exact vmid match first (rank 0), then a
// name-prefix match (rank 1), then any other substring match across
// name/tags/node/vmid (rank 2). Trivial relevance, deliberately no search
// index — the fleet is small enough that a linear scan per request is fine.
func matchResources(connID, connName string, resources []pve.ClusterResource, q string) []searchResult {
	qLower := strings.ToLower(q)
	var out []searchResult
	for _, res := range resources {
		if res.Type != "qemu" && res.Type != "lxc" {
			continue
		}
		nameLower := strings.ToLower(res.Name)
		vmidStr := strconv.Itoa(res.VMID)

		rank := -1
		switch {
		case vmidStr == q:
			rank = 0
		case qLower != "" && strings.HasPrefix(nameLower, qLower):
			rank = 1
		case strings.Contains(nameLower, qLower),
			strings.Contains(strings.ToLower(res.Tags), qLower),
			strings.Contains(strings.ToLower(res.Node), qLower),
			strings.Contains(vmidStr, q):
			rank = 2
		}
		if rank < 0 {
			continue
		}
		out = append(out, searchResult{
			ID:           res.ID,
			ConnectionID: connID, ConnectionName: connName,
			Type: res.Type, VMID: res.VMID, Name: res.Name, Node: res.Node,
			Tags: res.Tags, Status: res.Status, rank: rank,
		})
	}
	return out
}
