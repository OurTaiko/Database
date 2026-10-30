package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/tja"
)

// Background movies as on Fanmade: a replacement stores the movie transcoded under its hash
// and points BGMOVIE at it, keeps it while the TJA still declares one, drops it when the TJA
// no longer does, leaves out a movie that is not a video, and an upload that asks not to wait
// is answered 202 and reported by an upload_result notification.
func TestBackgroundMovies(t *testing.T) {
	if !tja.Available() {
		t.Skip("tjaparser not found: set TJAPARSER_BIN or put it on PATH")
	}
	dir := t.TempDir()
	movie := filepath.Join(dir, "a.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=30", "-c:v", "libx264", "-pix_fmt", "yuv420p", movie).CombinedOutput(); err != nil {
		t.Skip("ffmpeg with libx264 is needed:", err, string(out))
	}
	movieData, err := os.ReadFile(movie)
	if err != nil {
		t.Fatal(err)
	}
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	// A chart with files whose audio stays: the movie needs no audio decoding.
	source := []byte("TITLE:Song\nBPM:120\nWAVE:a.ogg\nCOURSE:Oni\nLEVEL:8\n#START\n1100,\n#END\n")
	sum := sha256.Sum256(source)
	digest := hex.EncodeToString(sum[:])
	if err = os.WriteFile(filepath.Join(s.Config.Storage, digest+".tja"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	var tjaID, audioID int64
	err = s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.tja',$2,$3,'tja') RETURNING id`, digest+".tja", digest, len(source)).Scan(&tjaID)
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

	send := func(chart, movieName string, movie []byte, async bool) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("tja", "new.tja")
		part.Write([]byte(chart))
		if movie != nil {
			part, _ = form.CreateFormFile("movie", movieName)
			part.Write(movie)
		}
		form.WriteField("confirmReset", "true")
		form.Close()
		r := httptest.NewRequest("PUT", w.chart(w.song)+"/files", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", ID())
		if async {
			r.Header.Set("Prefer", "respond-async")
		}
		r.RemoteAddr = fmt.Sprintf("10.8.%d.1:1234", w.c.n.Add(1)&255)
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens["owner"]})
		rec := httptest.NewRecorder()
		w.c.handler.ServeHTTP(rec, r)
		return rec
	}
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
	storedTJA := func() string {
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
	withMovie := strings.Replace(string(source), "WAVE:a.ogg", "WAVE:a.ogg\nBGMOVIE:a.mp4\nMOVIEOFFSET:1.5", 1)

	if code := send(string(source), "a.mp4", movieData, false).Code; code != 400 {
		t.Fatal("a movie the TJA does not declare", code)
	}
	if code := send(withMovie, "", nil, false).Code; code != 422 {
		t.Fatal("a declared movie that was not sent", code)
	}
	c := decode(send(withMovie, "a.mp4", movieData, false))
	if c.MovieHash == "" || c.MovieOffset != 1.5 || !strings.Contains(storedTJA(), "BGMOVIE:"+c.MovieHash+".mp4\n") {
		t.Fatal("movie not stored", c.MovieHash, c.MovieOffset, storedTJA())
	}
	var manifest struct {
		Resources map[string]struct{ ContentType string }
	}
	if code := w.c.do("", "GET", w.chart(w.song)+"/resources", "", &manifest); code != 200 || manifest.Resources["movie"].ContentType != "video/mp4" {
		t.Fatal("manifest lacks the movie", code, manifest)
	}
	if code, body := w.c.raw("", w.chart(w.song)+"/movie"); code != 200 || len(body) == 0 {
		t.Fatal("movie not served", code)
	}
	first := c.MovieHash

	// The stored text declares the stored movie: saving it again keeps the movie.
	if kept := decode(send(storedTJA(), "", nil, false)); kept.MovieHash != first {
		t.Fatal("movie not kept", kept.MovieHash)
	}
	// Not a video: the chart is updated and the movie left out.
	broken := decode(send(strings.Replace(withMovie, "a.mp4", "b.mp4", 1), "b.mp4", []byte("not a video"), false))
	if broken.MovieHash != "" || len(broken.FailedFiles) != 1 || broken.FailedFiles[0].Code != "MOVIE_INVALID" || !strings.Contains(storedTJA(), "BGMOVIE:\n") {
		t.Fatal("bad movie not left out", broken.MovieHash, broken.FailedFiles, storedTJA())
	}
	var left int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM chart_resources WHERE sha256=$1`, first).Scan(&left); err != nil || left != 0 {
		t.Fatal("replaced movie not released", left, err)
	}

	// Asked not to wait: 202 now, the result as a notification once processed.
	if rec := send(withMovie, "a.mp4", movieData, true); rec.Code != 202 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(30 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		if err = s.DB.QueryRow(ctx, `SELECT data->>'status' FROM notifications WHERE recipient_id=$1 AND kind='upload_result' ORDER BY created_at DESC LIMIT 1`, testUsers["owner"].ID).Scan(&status); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "updated" {
		t.Fatal("no upload_result notification", status, err)
	}
	if c, err = s.chart(ctx, w.song); err != nil || c.MovieHash == "" {
		t.Fatal("background upload did not store the movie", err)
	}

	// A new chart (only administrators upload) goes through the same job: its audio decoded,
	// its movie stored.
	oggData, err := os.ReadFile("../../kit/audio/testdata/vorbis.ogg")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, f := range []struct {
		field, name string
		data        []byte
	}{{"tja", "new.tja", []byte(strings.Replace(withMovie, "WAVE:a.ogg", "WAVE:vorbis.ogg", 1))}, {"audio", "vorbis.ogg", oggData}, {"movie", "a.mp4", movieData}} {
		part, _ := form.CreateFormFile(f.field, f.name)
		part.Write(f.data)
	}
	form.Close()
	r := httptest.NewRequest("POST", "/api/v1/charts", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("X-CSRF-Token", "csrf")
	r.Header.Set("Idempotency-Key", ID())
	r.RemoteAddr = "10.8.200.1:1234"
	r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: w.c.tokens["admin"]})
	rec := httptest.NewRecorder()
	w.c.handler.ServeHTTP(rec, r)
	var published Chart
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &published) != nil || published.MovieHash == "" || published.Duration <= 0 || published.ID == w.song {
		t.Fatal("new chart with a movie", rec.Code, rec.Body.String())
	}
}
