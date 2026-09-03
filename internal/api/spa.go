package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SetWebFS attaches the embedded (or on-disk, in dev mode) built frontend.
// Unknown, non-API routes fall back to index.html so client-side routing works.
func (s *Server) SetWebFS(webFS fs.FS) {
	s.webFS = webFS
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if s.webFS == nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}

	reqPath := strings.TrimPrefix(r.URL.Path, "/")
	if reqPath == "" {
		reqPath = "index.html"
	}
	if f, err := s.webFS.Open(reqPath); err == nil {
		f.Close()
		http.ServeFileFS(w, r, s.webFS, reqPath)
		return
	}

	// Client-side route (e.g. /dashboard) — serve the SPA shell.
	http.ServeFileFS(w, r, s.webFS, path.Join("index.html"))
}
