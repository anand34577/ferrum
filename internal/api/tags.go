package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

// tagCount is one distinct tag and how many guests across the fleet carry
// it, returned by GET /api/v1/tags.
type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// splitTags parses PVE's guest "tags" config field into a normalized,
// deduplicated-per-guest list of individual tags. PVE stores tags as a
// single string and its own UI accepts either "," or ";" as the separator
// (newer PVE emits ";"), so both are honored here.
func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' })
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// listTags aggregates the distinct tag set (with per-tag guest counts)
// across every connection's guests — GET /api/v1/tags. Uses the same
// /cluster/resources call the fleet inventory view is built from rather
// than a per-guest config fetch, so this stays a fan-out of N connections,
// not N guests.
func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
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

	resourcesByConn := make([][]pve.ClusterResource, len(conns))
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
				return // unreachable connection: contributes no tags, doesn't fail the request
			}
			resources, err := client.ClusterResources(r.Context())
			if err != nil {
				return
			}
			resourcesByConn[i] = resources
		}(i, c)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, aggregateTagCounts(resourcesByConn))
}

// aggregateTagCounts tallies distinct guest tags across every connection's
// resource list, sorted by descending count then alphabetically.
func aggregateTagCounts(resourcesByConn [][]pve.ClusterResource) []tagCount {
	counts := map[string]int{}
	for _, resources := range resourcesByConn {
		for _, res := range resources {
			if res.Type != "qemu" && res.Type != "lxc" {
				continue
			}
			for _, tag := range splitTags(res.Tags) {
				counts[tag]++
			}
		}
	}
	out := make([]tagCount, 0, len(counts))
	for tag, n := range counts {
		out = append(out, tagCount{Tag: tag, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

type updateGuestTagsRequest struct {
	Tags string `json:"tags"`
}

// updateGuestTags sets a single guest's PVE tags field — a thin wrapper over
// UpdateGuestConfig scoped to just that field, so clearing tags (empty
// string) works the same as setting them — PUT
// /api/v1/connections/{id}/guests/{type}/{node}/{vmid}/tags.
func (s *Server) updateGuestTags(w http.ResponseWriter, r *http.Request) {
	connID, guestType, node := chi.URLParam(r, "id"), chi.URLParam(r, "type"), chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	var req updateGuestTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.UpdateGuestConfig(r.Context(), guestType, node, vmid, url.Values{"tags": {req.Tags}})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "vm.tags.update", "vm", node+"/"+guestType+"/"+chi.URLParam(r, "vmid"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}
