package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// connectionCertificateDTO is one TLS certificate on one node of a
// connection, shaped for a settings page to render expiry directly rather
// than waiting for the alert evaluator's own threshold check (see
// internal/poller/certificates.go) to trip.
type connectionCertificateDTO struct {
	Node        string   `json:"node"`
	Filename    string   `json:"filename"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	NotBefore   string   `json:"notBefore,omitempty"`
	NotAfter    string   `json:"notAfter,omitempty"`
	DaysLeft    *float64 `json:"daysLeft,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}

// connectionCertificates surfaces every node's TLS certificates (and their
// expiry) for one connection. PBS connections aren't covered — Ferrum only
// stores PVE connections in the connections table today.
func (s *Server) connectionCertificates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	client, err := s.clientFor(r.Context(), id)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "connection unreachable: "+err.Error())
		return
	}
	resources, err := client.ClusterResources(r.Context())
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "fetching nodes failed: "+err.Error())
		return
	}

	out := []connectionCertificateDTO{}
	for _, res := range resources {
		if res.Type != "node" || res.Node == "" {
			continue
		}
		certs, err := client.NodeCertificates(r.Context(), res.Node)
		if err != nil {
			slog.Warn("connection certificates: fetching node certificates failed", "connectionId", id, "node", res.Node, "error", err)
			continue
		}
		for _, c := range certs {
			dto := connectionCertificateDTO{
				Node: res.Node, Filename: c.Filename, Subject: c.Subject, Issuer: c.Issuer, Fingerprint: c.Fingerprint,
			}
			if c.NotBefore > 0 {
				dto.NotBefore = time.Unix(c.NotBefore, 0).UTC().Format(time.RFC3339)
			}
			if c.NotAfter > 0 {
				notAfter := time.Unix(c.NotAfter, 0).UTC()
				dto.NotAfter = notAfter.Format(time.RFC3339)
				days := time.Until(notAfter).Hours() / 24
				dto.DaysLeft = &days
			}
			out = append(out, dto)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
