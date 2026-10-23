package httpapi

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// site serves the embedded build, falling back to the SPA for page routes only.
func (s *Server) site(w http.ResponseWriter, r *http.Request) {
	if s.Config.Site == nil || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" && name != "." {
		if info, err := fs.Stat(s.Config.Site, name); err == nil && !info.IsDir() && !strings.HasPrefix(name, ".") {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, s.Config.Site, name)
			return
		}
		if path.Ext(name) != "" || strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
	}
	page, err := fs.ReadFile(s.Config.Site, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
}
