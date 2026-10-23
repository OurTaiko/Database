package httpapi

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestEmbeddedSite(t *testing.T) {
	site := fstest.MapFS{
		"index.html":         {Data: []byte("<html>OurTaikoDatabase</html>")},
		"assets/app.js":      {Data: []byte("console.log('app')")},
		"site.webmanifest":   {Data: []byte("{}")},
		"icons/icon-192.png": {Data: []byte("png")},
	}
	for _, tc := range []struct {
		method, path, cache string
		status              int
		empty               bool
	}{
		{"GET", "/", "no-cache", 200, false},
		{"GET", "/charts/123", "no-cache", 200, false},
		{"GET", "/index.html", "no-cache", 200, false},
		{"HEAD", "/dans/1", "no-cache", 200, true},
		{"GET", "/assets/app.js", "public, max-age=31536000, immutable", 200, false},
		{"GET", "/site.webmanifest", "public, max-age=3600", 200, false},
		{"GET", "/icons/icon-192.png", "public, max-age=3600", 200, false},
		{"GET", "/assets/missing.js", "no-store", 404, false},
		{"GET", "/api/v1/missing", "no-store", 404, false},
		{"GET", "/.gitkeep", "no-store", 404, false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(nil, Config{Site: site}).Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || w.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("status=%d cache=%q body=%s", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
			}
			if tc.empty && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
	w := httptest.NewRecorder()
	New(nil, Config{}).Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 404 {
		t.Fatalf("API-only site status=%d", w.Code)
	}
}
