package httpapi

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipJSON compresses JSON responses for clients that accept gzip; the game
// catalog is several megabytes and crosses a slow link between the servers.
// Files, redirects and other responses pass through untouched, so ranges and
// content hashes of stored objects are unaffected.
func gzipJSON(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return w, func() {}
	}
	g := &gzipWriter{ResponseWriter: w}
	return g, g.close
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
			return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
		}
	}
	return false
}

type gzipWriter struct {
	http.ResponseWriter
	zip     *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(status int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		h.Add("Vary", "Accept-Encoding")
		if strings.HasPrefix(h.Get("Content-Type"), "application/json") && h.Get("Content-Encoding") == "" && status != http.StatusNoContent && status != http.StatusNotModified {
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			g.zip, _ = gzip.NewWriterLevel(g.ResponseWriter, gzip.DefaultCompression)
		}
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.zip != nil {
		return g.zip.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) close() {
	if g.zip != nil {
		g.zip.Close()
	}
}
