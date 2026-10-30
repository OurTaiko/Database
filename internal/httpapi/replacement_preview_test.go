package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/sitekit/tja"
)

// A replacement's preview reads the new TJA block by block against the stored
// one and stores nothing; keepTitles keeps the chart's names.
func TestReplacementPreview(t *testing.T) {
	if !tja.Available() {
		t.Skip("tjaparser not found: set TJAPARSER_BIN or put it on PATH")
	}
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	source := []byte("TITLE:Original\nBPM:120\nWAVE:a.ogg\nCOURSE:Hard\nLEVEL:5\n#START\n1000,\n#END\nCOURSE:Oni\nLEVEL:8\n#START\n1100,\n#END\n")
	sum := sha256.Sum256(source)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(s.Config.Storage, digest+".tja"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	var tjaID, audioID int64
	err := s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.tja',$2,$3,'tja') RETURNING id`,
		digest+".tja", digest, len(source)).Scan(&tjaID)
	if err == nil {
		err = s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.ogg',$2,1,'ogg') RETURNING id`,
			digest+".ogg", digest).Scan(&audioID)
	}
	if err == nil {
		_, err = s.DB.Exec(ctx, `UPDATE charts SET title='Original',tja_file=$2,audio_file=$3,duration=10,title_translations='{"origin":"Original"}',subtitle_translations='{}' WHERE id=$1`, w.song, tjaID, audioID)
	}
	if err != nil {
		t.Fatal(err)
	}
	meta, issue := tja.Parse(source, "")
	if issue != nil {
		t.Fatal(issue)
	}
	tx, err := s.DB.Begin(ctx)
	if err == nil {
		err = database.SetDifficulties(ctx, tx, w.song, meta.Difficulties)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}

	send := func(user, method, path, chart string, fields map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("tja", "new.tja")
		part.Write([]byte(chart))
		for name, value := range fields {
			form.WriteField(name, value)
		}
		form.Close()
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", ID())
		r.RemoteAddr = fmt.Sprintf("10.9.%d.1:1234", w.c.n.Add(1)&255)
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens[user]})
		rec := httptest.NewRecorder()
		w.c.handler.ServeHTTP(rec, r)
		return rec
	}
	title := func() string {
		t.Helper()
		var v string
		if err := s.DB.QueryRow(ctx, `SELECT title FROM charts WHERE id=$1`, w.song).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	path := w.chart(w.song) + "/files"
	renamed := "TITLE:Renamed\nBPM:120\nWAVE:a.ogg\nCOURSE:Hard\nLEVEL:5\n#START\n1010,\n#END\nCOURSE:Easy\nLEVEL:2\n#START\n1000,\n#END\n"
	if code := send("alice", "POST", path+"/preview", renamed, nil).Code; code != 403 {
		t.Fatal("preview by a stranger", code)
	}
	rec := send("owner", "POST", path+"/preview", renamed, nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var preview struct {
		Metadata struct{ Title string }
		Previous *struct{ Title string }
		Blocks   []replacementBlock
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	want := []replacementBlock{{"Hard", "single", "changed", 0}, {"Easy", "single", "added", 0}, {"Oni", "single", "removed", 0}}
	if fmt.Sprint(preview.Blocks) != fmt.Sprint(want) || preview.Metadata.Title != "Renamed" || preview.Previous == nil || preview.Previous.Title != "Original" {
		t.Fatal("bad preview", rec.Body.String())
	}
	var blocks int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM chart_difficulties WHERE chart_id=$1`, w.song).Scan(&blocks); err != nil || blocks != 2 || title() != "Original" {
		t.Fatal("preview changed the chart", blocks, err)
	}
	if code := send("owner", "POST", path+"/preview", "broken", nil).Code; code != 422 {
		t.Fatal("broken TJA previewed", code)
	}

	if rec = send("owner", "PUT", path, renamed, map[string]string{"confirmReset": "true", "keepTitles": "true"}); rec.Code != 200 || title() != "Original" {
		t.Fatal("keepTitles replaced the name", rec.Code, rec.Body.String(), title())
	}
	demo := func() string {
		t.Helper()
		var start, end float64
		if err := s.DB.QueryRow(ctx, `SELECT demo_start,demo_end FROM charts WHERE id=$1`, w.song).Scan(&start, &end); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(start, "-", end)
	}
	// The preview range is the site's to set within the audio (10 s here), and a replacement
	// that sends it keeps it; one that does not takes the TJA's DEMOSTART again.
	if code := w.c.do("owner", "PATCH", w.chart(w.song), `{"demoStart":2,"demoEnd":6}`, nil); code != 200 || demo() != "2-6" {
		t.Fatal("preview range not set", code, demo())
	}
	if code := w.c.do("owner", "PATCH", w.chart(w.song), `{"demoStart":12}`, nil); code != 422 || demo() != "2-6" {
		t.Fatal("a range past the audio was accepted", code, demo())
	}
	renamedAgain := renamed + "COURSE:Oni\nLEVEL:8\n#START\n1100,\n#END\n"
	if rec = send("owner", "PUT", path, renamedAgain, map[string]string{"confirmReset": "true", "keepTitles": "true", "demoStart": "2", "demoEnd": "6"}); rec.Code != 200 || demo() != "2-6" {
		t.Fatal("the sent preview range was not kept", rec.Code, rec.Body.String(), demo())
	}
	if code := send("owner", "PUT", path, renamed, map[string]string{"confirmReset": "true", "demoStart": "11"}).Code; code != 422 {
		t.Fatal("a range past the audio was stored", code)
	}
	if rec = send("owner", "PUT", path, renamed, map[string]string{"confirmReset": "true"}); rec.Code != 200 || title() != "Renamed" || demo() != "0-15" {
		t.Fatal("the new file's name and DEMOSTART were not taken", rec.Code, rec.Body.String(), title(), demo())
	}
}
