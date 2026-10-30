package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"ourtaiko.dev/database/api/internal/tja"
)

// Makers typed on the website, one per block, as on Fanmade: an upload, an edit and a
// replacement set them; without them a block's maker is the TJA's.
func TestDifficultyMakers(t *testing.T) {
	if !tja.Available() {
		t.Skip("tjaparser not found: set TJAPARSER_BIN or put it on PATH")
	}
	w := interactionFixture(t)
	audio, err := os.ReadFile("../../kit/audio/testdata/vorbis.ogg")
	if err != nil {
		t.Fatal(err)
	}
	source := "TITLE:Maker Test\nMAKER:Original\nBPM:120\nWAVE:vorbis.ogg\n"
	for _, course := range []string{"Hard", "Oni"} {
		source += "COURSE:" + course + "\nLEVEL:5\n#START\n1000,\n#END\n"
	}
	send := func(method, path string, files bool, fields map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("tja", "chart.tja")
		part.Write([]byte(source))
		if files {
			part, _ = form.CreateFormFile("audio", "vorbis.ogg")
			part.Write(audio)
		}
		for name, value := range fields {
			form.WriteField(name, value)
		}
		form.Close()
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", ID())
		r.RemoteAddr = "10.8.200.1:1234"
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens["admin"]})
		rec := httptest.NewRecorder()
		w.c.handler.ServeHTTP(rec, r)
		return rec
	}
	makers := func(c Chart) map[string]string {
		names := map[string]string{}
		for _, d := range c.Difficulties {
			names[d.Course] = d.Maker
		}
		return names
	}
	typed := `[{"course":"Hard","side":"single","maker":"A"},{"course":"Oni","side":"single","maker":" B "}]`
	rec := send("POST", "/api/v1/charts", true, map[string]string{"difficultyMakers": typed})
	var c Chart
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &c) != nil {
		t.Fatal("upload", rec.Code, rec.Body.String())
	}
	if got := makers(c); got["Hard"] != "A" || got["Oni"] != "B" || c.Maker != "A | B" {
		t.Fatal("typed makers", got, c.Maker)
	}
	for _, bad := range []string{`[{"course":"Hard","side":"single","maker":"A"}]`, `{}`, `[{"course":"Tower","side":"single","maker":"A"},{"course":"Oni","side":"single","maker":"B"}]`} {
		if rec = send("POST", "/api/v1/charts", true, map[string]string{"difficultyMakers": bad}); rec.Code != 422 || !bytes.Contains(rec.Body.Bytes(), []byte("MAKERS_INVALID")) {
			t.Fatal("bad makers accepted", bad, rec.Code, rec.Body.String())
		}
	}
	// An edit changes only the makers it is given.
	var edited Chart
	if code := w.c.do("admin", "PATCH", w.chart(c.ID), `{"difficultyMakers":[{"course":"Hard","side":"single","maker":"C"},{"course":"Oni","side":"single","maker":"B"}]}`, &edited); code != 200 || makers(edited)["Hard"] != "C" || makers(edited)["Oni"] != "B" {
		t.Fatal("edit", code, makers(edited))
	}
	if code := w.c.do("admin", "PATCH", w.chart(c.ID), `{"difficultyMakers":[{"course":"Hard","side":"single","maker":"C"}]}`, nil); code != 422 {
		t.Fatal("partial makers accepted", code)
	}
	// A replacement without makers takes the TJA's; with them, the typed ones.
	if rec = send("PUT", w.chart(c.ID)+"/files", false, map[string]string{"confirmReset": "true"}); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &c) != nil || makers(c)["Oni"] != "Original" {
		t.Fatal("replacement", rec.Code, rec.Body.String())
	}
	if rec = send("PUT", w.chart(c.ID)+"/files", false, map[string]string{"confirmReset": "true", "difficultyMakers": typed}); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &c) != nil || makers(c)["Hard"] != "A" {
		t.Fatal("replacement with makers", rec.Code, rec.Body.String())
	}
}
