package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type templateItem struct {
	VolID   string `json:"volid"`
	Node    string `json:"node"`
	Storage string `json:"storage"`
	Content string `json:"content"` // "iso" | "vztmpl"
	Size    int64  `json:"size,omitempty"`
}

// listTemplates aggregates ISO images and LXC templates across every node
// and storage on a connection, so the create-guest wizard can offer a real
// picker instead of asking the user to type a volume ID from memory.
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	nodes, err := client.Nodes(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}

	out := []templateItem{}
	for _, node := range nodes {
		storages, err := client.NodeStorage(r.Context(), node.Node)
		if err != nil {
			continue // one unreachable/misconfigured node shouldn't blank the whole catalog
		}
		for _, storage := range storages {
			if storage.Active == 0 {
				continue
			}
			items, err := client.StorageContent(r.Context(), node.Node, storage.Storage)
			if err != nil {
				continue
			}
			for _, item := range items {
				if item.Content != "iso" && item.Content != "vztmpl" {
					continue
				}
				out = append(out, templateItem{
					VolID: item.VolID, Node: node.Node, Storage: storage.Storage,
					Content: item.Content, Size: item.Size,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
