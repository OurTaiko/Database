package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
)

// Uploads are UTF-8 only: the browser converts Shift-JIS and the rest first, and the form has
// no encoding field.
func TestUploadsAreUTF8Only(t *testing.T) {
	w := interactionFixture(t)
	audio, err := os.ReadFile("../../kit/audio/testdata/vorbis.ogg")
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("TITLE:テスト\nBPM:120\nWAVE:vorbis.ogg\nCOURSE:Oni\nLEVEL:5\n#START\n1000,\n#END\n")
	sjis, err := japanese.ShiftJIS.NewEncoder().Bytes(source)
	if err != nil {
		t.Fatal(err)
	}
	post := func(data []byte, fields ...string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("tja", "chart.tja")
		part.Write(data)
		part, _ = form.CreateFormFile("audio", "vorbis.ogg")
		part.Write(audio)
		for i := 0; i+1 < len(fields); i += 2 {
			form.WriteField(fields[i], fields[i+1])
		}
		form.Close()
		r := httptest.NewRequest("POST", "/api/v1/charts", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", ID())
		r.RemoteAddr = "10.8.200.1:1234"
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens["owner"]})
		rec := httptest.NewRecorder()
		w.c.handler.ServeHTTP(rec, r)
		return rec
	}
	if rec := post(sjis); rec.Code != 422 || !strings.Contains(rec.Body.String(), "TJA_ENCODING_INVALID") {
		t.Fatal("Shift-JIS must be refused", rec.Code, rec.Body.String())
	}
	for _, encoding := range []string{"utf-8", "shift-jis"} {
		if rec := post(source, "encoding", encoding); rec.Code != 400 || !strings.Contains(rec.Body.String(), "UPLOAD_FILES_INVALID") {
			t.Fatal("encoding field accepted", encoding, rec.Code, rec.Body.String())
		}
	}
}
