package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/pve"
)

func rrdTimeframe(r *http.Request) pve.RRDTimeframe {
	switch r.URL.Query().Get("timeframe") {
	case "day":
		return pve.RRDDay
	case "week":
		return pve.RRDWeek
	case "month":
		return pve.RRDMonth
	case "year":
		return pve.RRDYear
	default:
		return pve.RRDHour
	}
}

// rrdCF picks the consolidation function: AVERAGE (default) or MAX, which
// the UI overlays as a "peak envelope" band on top of the average series.
func rrdCF(r *http.Request) pve.RRDCF {
	if r.URL.Query().Get("cf") == "MAX" {
		return pve.RRDMax
	}
	return pve.RRDAverage
}

func (s *Server) nodeRRDData(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	points, err := client.NodeRRDData(r.Context(), chi.URLParam(r, "node"), rrdTimeframe(r), rrdCF(r))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) guestRRDData(w http.ResponseWriter, r *http.Request) {
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
	points, err := client.GuestRRDData(r.Context(), guestType, node, vmid, rrdTimeframe(r), rrdCF(r))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

// guestLiveStatus exposes /nodes/{node}/{type}/{vmid}/status/current — live
// network/disk counters (cumulative since guest start; the UI derives rates),
// ballooning detail, and HA state that the cluster overview lacks.
func (s *Server) guestLiveStatus(w http.ResponseWriter, r *http.Request) {
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
	status, err := client.GuestLiveStatus(r.Context(), guestType, node, vmid)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
