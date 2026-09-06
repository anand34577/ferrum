package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) sdnZones(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	zones, err := client.SDNZones(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

type sdnZoneRequest struct {
	Zone    string            `json:"zone"`
	Options map[string]string `json:"options"`
}

func (s *Server) createSDNZone(w http.ResponseWriter, r *http.Request) {
	var req sdnZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Zone == "" || req.Options["type"] == "" {
		writeErrorMsg(w, http.StatusBadRequest, "zone and options.type are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateSDNZone(r.Context(), req.Zone, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.zone.create", "sdn", req.Zone)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateSDNZone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	zone := chi.URLParam(r, "zone")
	if err := client.UpdateSDNZone(r.Context(), zone, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.zone.update", "sdn", zone)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteSDNZone(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	zone := chi.URLParam(r, "zone")
	if err := client.DeleteSDNZone(r.Context(), zone); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.zone.delete", "sdn", zone)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) sdnVnets(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	vnets, err := client.SDNVnets(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, vnets)
}

func (s *Server) createSDNVnet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vnet      string `json:"vnet"`
		Zone      string `json:"zone"`
		Alias     string `json:"alias,omitempty"`
		Tag       int    `json:"tag,omitempty"`
		VLANAware bool   `json:"vlanAware,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Vnet == "" || req.Zone == "" {
		writeErrorMsg(w, http.StatusBadRequest, "vnet and zone are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateSDNVnet(r.Context(), req.Vnet, req.Zone, req.Alias, req.Tag, req.VLANAware); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.vnet.create", "sdn", req.Vnet)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteSDNVnet(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	vnet := chi.URLParam(r, "vnet")
	if err := client.DeleteSDNVnet(r.Context(), vnet); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.vnet.delete", "sdn", vnet)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) sdnSubnets(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	subnets, err := client.SDNSubnets(r.Context(), chi.URLParam(r, "vnet"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, subnets)
}

func (s *Server) createSDNSubnet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR    string `json:"cidr"`
		Gateway string `json:"gateway,omitempty"`
		SNAT    bool   `json:"snat,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.CIDR == "" {
		writeErrorMsg(w, http.StatusBadRequest, "cidr is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	vnet := chi.URLParam(r, "vnet")
	if err := client.CreateSDNSubnet(r.Context(), vnet, req.CIDR, req.Gateway, req.SNAT); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.subnet.create", "sdn", vnet+"/"+req.CIDR)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteSDNSubnet takes the subnet CIDR as a query parameter rather than a
// path segment — a CIDR contains "/", which a path segment can't hold
// without percent-encoding tripping over net/http's path unescaping.
func (s *Server) deleteSDNSubnet(w http.ResponseWriter, r *http.Request) {
	subnet := r.URL.Query().Get("subnet")
	if subnet == "" {
		writeErrorMsg(w, http.StatusBadRequest, "subnet query parameter is required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	vnet := chi.URLParam(r, "vnet")
	if err := client.DeleteSDNSubnet(r.Context(), vnet, subnet); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.subnet.delete", "sdn", vnet+"/"+subnet)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) sdnControllers(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	controllers, err := client.SDNControllers(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, controllers)
}

type sdnControllerRequest struct {
	Controller string            `json:"controller"`
	Options    map[string]string `json:"options"`
}

func (s *Server) createSDNController(w http.ResponseWriter, r *http.Request) {
	var req sdnControllerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Controller == "" || req.Options["type"] == "" {
		writeErrorMsg(w, http.StatusBadRequest, "controller and options.type are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateSDNController(r.Context(), req.Controller, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.controller.create", "sdn", req.Controller)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateSDNController(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	controller := chi.URLParam(r, "controller")
	if err := client.UpdateSDNController(r.Context(), controller, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.controller.update", "sdn", controller)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteSDNController(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	controller := chi.URLParam(r, "controller")
	if err := client.DeleteSDNController(r.Context(), controller); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.controller.delete", "sdn", controller)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) sdnIPAMs(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	ipams, err := client.SDNIPAMs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ipams)
}

type sdnIPAMRequest struct {
	Ipam    string            `json:"ipam"`
	Options map[string]string `json:"options"`
}

func (s *Server) createSDNIPAM(w http.ResponseWriter, r *http.Request) {
	var req sdnIPAMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Ipam == "" || req.Options["type"] == "" {
		writeErrorMsg(w, http.StatusBadRequest, "ipam and options.type are required")
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CreateSDNIPAM(r.Context(), req.Ipam, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.ipam.create", "sdn", req.Ipam)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateSDNIPAM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Options map[string]string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	ipam := chi.URLParam(r, "ipam")
	if err := client.UpdateSDNIPAM(r.Context(), ipam, req.Options); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.ipam.update", "sdn", ipam)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteSDNIPAM(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	ipam := chi.URLParam(r, "ipam")
	if err := client.DeleteSDNIPAM(r.Context(), ipam); err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.ipam.delete", "sdn", ipam)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) applySDNConfig(w http.ResponseWriter, r *http.Request) {
	client, err := s.clientFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	upid, err := client.ApplySDNConfig(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.audit(r, "sdn.apply", "connection", chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]string{"upid": upid})
}
