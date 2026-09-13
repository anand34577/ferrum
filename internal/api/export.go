package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ferrum/internal/export"
	"ferrum/internal/pve"
)

// exportFanoutLimit bounds how many per-guest GuestConfig (and, for the
// Ansible export, guest-agent network) calls run concurrently — a large
// fleet can have hundreds of guests, and this is a one-shot admin action,
// not a poll tick, so it can afford real concurrency without the tighter
// caps used elsewhere.
const exportFanoutLimit = 16

// gatherExportGuests fetches the connection's live inventory (like
// overview.go) plus each guest's full config (like getGuestConfig), and
// hands both to internal/export as a []export.Guest — the shape that
// package's generators actually consume. withAgentIPs additionally queries
// each running guest's agent-reported IP for the Ansible export; skipped for
// Terraform, which has no use for it and would otherwise pay that cost for
// nothing.
func (s *Server) gatherExportGuests(ctx context.Context, connID string, withAgentIPs bool) ([]export.Guest, error) {
	client, err := s.clientFor(ctx, connID)
	if err != nil {
		return nil, err
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return nil, err
	}

	var guestResources []pve.ClusterResource
	for _, res := range resources {
		if (res.Type == "qemu" || res.Type == "lxc") && res.Template != 1 {
			guestResources = append(guestResources, res)
		}
	}

	guests := make([]export.Guest, len(guestResources))
	var wg sync.WaitGroup
	sem := make(chan struct{}, exportFanoutLimit)
	for i, res := range guestResources {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, res pve.ClusterResource) {
			defer wg.Done()
			defer func() { <-sem }()
			g := export.Guest{Resource: res}
			if cfg, err := client.GuestConfig(ctx, res.Type, res.Node, res.VMID); err == nil {
				g.Config = cfg
			}
			if withAgentIPs && res.Status == "running" {
				g.IP = firstAgentIP(ctx, client, res)
			}
			guests[i] = g
		}(i, res)
	}
	wg.Wait()

	return guests, nil
}

// firstAgentIP best-effort resolves a guest's live IP for the Ansible
// export. Any failure (agent not installed/running for QEMU, container
// stopped, etc) just means no IP — never surfaced as an export-blocking
// error, since most fleets have guests without the agent enabled. Reported
// addresses are sanitized (pve.SanitizeAgentIP) before use: agent output
// comes from inside the guest and must never be embedded verbatim in
// generated files.
func firstAgentIP(ctx context.Context, client *pve.Client, res pve.ClusterResource) string {
	var ifaces []pve.AgentNetworkInterface
	var err error
	if res.Type == "qemu" {
		ifaces, err = client.GuestAgentNetworkInterfaces(ctx, res.Node, res.VMID)
	} else {
		ifaces, err = client.LXCInterfaces(ctx, res.Node, res.VMID)
	}
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Name == "lo" {
			continue
		}
		for _, ip := range iface.IPAddresses {
			if ip := pve.SanitizeAgentIP(ip); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func (s *Server) connectionName(ctx context.Context, id string) string {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM connections WHERE id = ?`, id).Scan(&name); err != nil {
		return id
	}
	return name
}

func writeDownload(w http.ResponseWriter, filename, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// exportTerraform generates Terraform HCL (Telmate/proxmox provider shapes)
// for the connection's current live inventory. Admin-only and audited —
// exporting the full guest inventory (hardware sizing, network layout,
// storage placement) is sensitive infrastructure detail.
func (s *Server) exportTerraform(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	guests, err := s.gatherExportGuests(ctx, connID, false)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	name := s.connectionName(ctx, connID)
	body := export.GenerateTerraform(name, guests)
	s.audit(r, "export.terraform", "connection", connID)
	writeDownload(w, fmt.Sprintf("%s.tf", slugForFilename(name)), "text/plain; charset=utf-8", body)
}

// exportAnsible generates a YAML Ansible inventory for the connection's
// current live inventory. Admin-only and audited, same rationale as
// exportTerraform.
func (s *Server) exportAnsible(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	guests, err := s.gatherExportGuests(ctx, connID, true)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	name := s.connectionName(ctx, connID)
	body := export.GenerateAnsibleInventory(name, guests)
	s.audit(r, "export.ansible", "connection", connID)
	writeDownload(w, fmt.Sprintf("%s-inventory.yml", slugForFilename(name)), "application/x-yaml; charset=utf-8", body)
}

// slugForFilename is a minimal, dependency-free filename-safe transform —
// good enough for a downloaded file's name, not meant to be a general slug
// utility.
func slugForFilename(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		case r == ' ':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "export"
	}
	return string(out)
}
