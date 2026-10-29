package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The game's preview clip: the backfill cuts one for a chart without it and leaves update_at
// alone, the resource manifest lists it, and a new preview range replaces it.
func TestPreviewClips(t *testing.T) {
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "a.ogg")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=20", "-c:a", "libvorbis", source).CombinedOutput(); err != nil {
		t.Skip("ffmpeg with libvorbis is needed:", err, string(out))
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err = os.WriteFile(filepath.Join(s.Config.Storage, digest+".ogg"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var audioID int64
	if err = s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.ogg',$2,$3,'ogg') RETURNING id`,
		digest+".ogg", digest, len(data)).Scan(&audioID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE charts SET tja_file=$2,audio_file=$2,duration=20,demo_start=2,demo_end=8 WHERE id=$1`, w.song, audioID); err != nil {
		t.Fatal(err)
	}
	clip := func() (key string, updated time.Time) {
		t.Helper()
		if err := s.DB.QueryRow(ctx, `SELECT COALESCE(r.storage_key,''),c.update_at FROM charts c LEFT JOIN chart_resources r ON r.id=c.preview_file WHERE c.id=$1`, w.song).Scan(&key, &updated); err != nil {
			t.Fatal(err)
		}
		return
	}
	_, before := clip()
	if err = s.BackfillPreviews(ctx); err != nil {
		t.Fatal(err)
	}
	first, after := clip()
	if first == "" || !after.Equal(before) {
		t.Fatal("backfill made no clip or changed update_at", first, before, after)
	}
	var manifest struct {
		Resources map[string]struct {
			URL         string
			ContentType string
		}
	}
	if code := w.c.do("", "GET", w.chart(w.song)+"/resources", "", &manifest); code != 200 || manifest.Resources["preview"].ContentType != "audio/ogg" {
		t.Fatal("manifest lacks the clip", code, manifest)
	}
	if code, body := w.c.raw("", w.chart(w.song)+"/preview"); code != 200 || len(body) == 0 {
		t.Fatal("clip not served", code)
	}
	if err = s.BackfillPreviews(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := clip(); again != first {
		t.Fatal("backfill redid an existing clip")
	}
	if code := w.c.do("owner", "PATCH", w.chart(w.song), `{"demoStart":4,"demoEnd":10}`, nil); code != 200 {
		t.Fatal("range edit", code)
	}
	second, _ := clip()
	var old int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM chart_resources WHERE storage_key=$1`, first).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if second == "" || second == first || old != 0 {
		t.Fatal("a new range kept the old clip", first, second, old)
	}
	if _, err = os.Stat(filepath.Join(s.Config.Storage, first)); !os.IsNotExist(err) {
		t.Fatal("old clip object left behind", err)
	}
}
