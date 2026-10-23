package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipJSON(t *testing.T) {
	big := strings.Repeat(`{"title":"Material Girl"},`, 2000)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w, finish := gzipJSON(w, r)
		defer finish()
		if r.URL.Path == "/file" {
			w.Header().Set("Content-Type", "audio/ogg")
			w.Write([]byte("OggS"))
			return
		}
		respond(w, 200, big)
	}
	for _, tc := range []struct {
		path, accept string
		gzipped      bool
	}{
		{"/json", "gzip", true},
		{"/json", "br, gzip;q=0.5", true},
		{"/json", "gzip;q=0", false},
		{"/json", "", false},
		{"/file", "gzip", false},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Accept-Encoding", tc.accept)
		w := httptest.NewRecorder()
		handler(w, r)
		body := w.Body.Bytes()
		if (w.Header().Get("Content-Encoding") == "gzip") != tc.gzipped || (tc.gzipped && !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding")) {
			t.Fatalf("%+v: headers %v", tc, w.Header())
		}
		if tc.gzipped {
			z, err := gzip.NewReader(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			if body, err = io.ReadAll(z); err != nil {
				t.Fatal(err)
			}
			if w.Body.Len() > len(body)/10 {
				t.Fatalf("barely compressed: %d of %d", w.Body.Len(), len(body))
			}
		}
		if tc.path == "/json" && !strings.Contains(string(body), "Material Girl") || tc.path == "/file" && string(body) != "OggS" {
			t.Fatalf("%+v: body %.40q", tc, body)
		}
	}
}
