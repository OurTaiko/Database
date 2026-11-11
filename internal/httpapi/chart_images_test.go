package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/sitekit/tja"
)

// As on Fanmade: the cover sent is the image PREIMAGE names and the background image the one
// BGIMAGE names, which it must declare; the stored TJA names the chart's stored images, also
// after the cover alone changes, and a replacement still declaring BGIMAGE keeps the stored one.
func TestChartImagesFollowTheirHeaders(t *testing.T) {
	if !tja.Available() {
		t.Skip("tjaparser not found: set TJAPARSER_BIN or put it on PATH")
	}
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	// A chart with files whose audio stays, so no audio is decoded.
	source := []byte("TITLE:Song\nBPM:120\nWAVE:a.ogg\nCOURSE:Oni\nLEVEL:8\n#START\n1100,\n#END\n")
	sum := sha256.Sum256(source)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(s.Config.Storage, digest+".tja"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	var tjaID, audioID int64
	err := s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.tja',$2,$3,'tja') RETURNING id`, digest+".tja", digest, len(source)).Scan(&tjaID)
	if err == nil {
		err = s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.ogg',$2,1,'ogg') RETURNING id`, digest+".ogg", digest).Scan(&audioID)
	}
	if err == nil {
		_, err = s.DB.Exec(ctx, `UPDATE charts SET title='Song',tja_file=$2,audio_file=$3,duration=10 WHERE id=$1`, w.song, tjaID, audioID)
	}
	if err != nil {
		t.Fatal(err)
	}
	if meta, issue := tja.Parse(source, ""); issue != nil {
		t.Fatal(issue)
	} else if tx, err := s.DB.Begin(ctx); err != nil {
		t.Fatal(err)
	} else {
		if err = database.SetDifficulties(ctx, tx, w.song, meta.Difficulties); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	picture := func(c color.NRGBA) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
		for y := 0; y < 30; y++ {
			for x := 0; x < 40; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		return b.Bytes()
	}
	// Distinct images: equal ones would share one stored file.
	red, blue, green := picture(color.NRGBA{R: 255, A: 255}), picture(color.NRGBA{B: 255, A: 255}), picture(color.NRGBA{G: 255, A: 255})
	type part struct {
		field, name string
		data        []byte
	}
	send := func(method, path string, parts ...part) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for _, p := range parts {
			if p.name == "" {
				_ = form.WriteField(p.field, string(p.data))
				continue
			}
			out, _ := form.CreateFormFile(p.field, p.name)
			_, _ = out.Write(p.data)
		}
		_ = form.Close()
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", ID())
		r.RemoteAddr = fmt.Sprintf("10.9.%d.1:1234", w.c.n.Add(1)&255)
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens["owner"]})
		rec := httptest.NewRecorder()
		w.c.handler.ServeHTTP(rec, r)
		return rec
	}
	files := w.chart(w.song) + "/files"
	chart := func(headers string) part {
		return part{"tja", "new.tja", []byte(strings.Replace(string(source), "WAVE:a.ogg\n", "WAVE:a.ogg\n"+headers, 1))}
	}
	confirm := part{"confirmReset", "", []byte("true")}
	decode := func(rec *httptest.ResponseRecorder) Chart {
		t.Helper()
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var c Chart
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	stored := func() string {
		t.Helper()
		c, err := s.chart(ctx, w.song)
		if err != nil {
			t.Fatal(err)
		}
		f, err := s.Config.Objects.Open(ctx, c.TJAKey)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		return string(data)
	}
	declares := func(want string) {
		t.Helper()
		if text := stored(); !strings.Contains(text, want) {
			t.Fatalf("stored TJA lacks %q:\n%s", want, text)
		}
	}
	refused := func(rec *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if rec.Code != status || !strings.Contains(rec.Body.String(), `"`+code+`"`) {
			t.Fatalf("want %d %s, got %d %s", status, code, rec.Code, rec.Body.String())
		}
	}

	refused(send("PUT", files, chart("PREIMAGE:jacket.png\n"), confirm, part{"cover", "other.png", red}), 422, "TJA_PREIMAGE_MISMATCH")
	refused(send("PUT", files, chart(""), confirm, part{"bgimage", "bg.png", red}), 400, "UPLOAD_FILES_INVALID")
	refused(send("PUT", files, chart("BGIMAGE:bg.png\n"), confirm, part{"bgimage", "other.png", red}), 422, "TJA_BGIMAGE_MISMATCH")

	c := decode(send("PUT", files, chart("PREIMAGE:img\\jacket.png\nBGIMAGE:../art/BG.png\n"), confirm, part{"cover", "jacket.png", red}, part{"bgimage", "BG.png", green}))
	if c.CoverHash == "" || c.BackgroundHash == "" {
		t.Fatal("images not stored", c.CoverHash, c.BackgroundHash)
	}
	declares("\nPREIMAGE:" + c.CoverHash + ".webp\nBGIMAGE:" + c.BackgroundHash + ".webp\n")
	if code, body := w.c.raw("", w.chart(w.song)+"/bgimage"); code != 200 || hash(string(body)) != c.BackgroundHash {
		t.Fatal("background image not served", code)
	}
	var manifest struct {
		Resources map[string]struct{ ContentType string }
	}
	if code := w.c.do("", "GET", w.chart(w.song)+"/resources", "", &manifest); code != 200 || manifest.Resources["bgimage"].ContentType != "image/webp" {
		t.Fatal("manifest lacks the background image", code, manifest)
	}

	// The cover alone: PREIMAGE follows and the old TJA is released.
	before := stored()
	rec := send("PUT", w.chart(w.song)+"/cover", part{"cover", "new.png", blue})
	var changed struct{ CoverHash string }
	if _ = json.Unmarshal(rec.Body.Bytes(), &changed); rec.Code != 200 || changed.CoverHash == "" {
		t.Fatal("cover change", rec.Code, rec.Body.String())
	}
	declares("\nPREIMAGE:" + changed.CoverHash + ".webp\n")
	var left int
	beforeSum := sha256.Sum256([]byte(before))
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM chart_resources WHERE sha256=$1`, hex.EncodeToString(beforeSum[:])).Scan(&left); err != nil || left != 0 {
		t.Fatal("replaced TJA not released", left, err)
	}

	// Still declaring BGIMAGE without a file keeps the stored image; dropping it releases it.
	kept := decode(send("PUT", files, chart("BGIMAGE:renamed.jpg\n"), confirm))
	if kept.BackgroundHash != c.BackgroundHash || kept.CoverHash != changed.CoverHash {
		t.Fatal("images not kept", kept.BackgroundHash, kept.CoverHash)
	}
	declares("\nPREIMAGE:" + changed.CoverHash + ".webp\nBGIMAGE:" + c.BackgroundHash + ".webp\n")
	dropped := decode(send("PUT", files, chart(""), confirm))
	if dropped.BackgroundHash != "" || strings.Contains(stored(), "BGIMAGE") {
		t.Fatal("background image not dropped", dropped.BackgroundHash, stored())
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM chart_resources WHERE sha256=$1`, c.BackgroundHash).Scan(&left); err != nil || left != 0 {
		t.Fatal("dropped background image not released", left, err)
	}
}
