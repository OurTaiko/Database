package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R042: the title and subtitle are the database's and can be edited at any
// time; the BPM stays the TJA's. The stored TJA never changes, and the
// package hands out a copy with the database's names in its headers.
func TestTitleEditsKeepTheStoredTJA(t *testing.T) {
	w := interactionFixture(t)
	c, s := w.c, w.s
	ctx := context.Background()
	store := func(name string, data []byte) (int64, string) {
		t.Helper()
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		key := digest + "." + ext
		if err := os.WriteFile(filepath.Join(s.Config.Storage, key), data, 0o600); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,$2,$3,$4,$5) RETURNING id`,
			key, name, digest, len(data), ext).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id, digest
	}
	audioID, audioHash := store("song.ogg", []byte("OggS fake audio"))
	source := "TITLE:Stored\nTITLEJA:保存\nSUBTITLE:--Artist\nBPM:150\nWAVE:" + audioHash + ".ogg\nCOURSE:Oni\nLEVEL:8\n#START\n1,\n#END\n"
	tjaID, tjaHash := store("song.tja", []byte(source))
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET tja_file=$2,audio_file=$3,title='Stored',subtitle='--Artist',bpm=150,duration=10,
	 title_translations='{"en":"Stored","ja":"保存"}',subtitle_translations='{"en":"--Artist"}' WHERE id=$1`, w.song, tjaID, audioID); err != nil {
		t.Fatal(err)
	}
	song := w.chart(w.song)
	if code := c.do("owner", "PATCH", song, `{"bpm":200}`, nil); code != 422 {
		t.Fatal("BPM of a chart with files changed", code)
	}
	var chart Chart
	if code := c.do("owner", "PATCH", song, `{"title":"Edited","subtitle":"Someone","titleTranslations":{"zh":"改名"}}`, &chart); code != 200 {
		t.Fatal("title edit", code)
	}
	// The translations are their own: the English one does not follow the title.
	if chart.Title != "Edited" || chart.Subtitle != "Someone" || chart.BPM != 150 || chart.TJAHash != tjaHash ||
		chart.TitleTranslations["en"] != "Stored" || chart.TitleTranslations["zh"] != "改名" {
		t.Fatalf("after edit: %q %q %v %s %v", chart.Title, chart.Subtitle, chart.BPM, chart.TJAHash, chart.TitleTranslations)
	}
	if code := c.do("owner", "PATCH", song, `{"title":"  "}`, nil); code != 422 {
		t.Fatal("empty title accepted", code)
	}
	stored, err := os.ReadFile(filepath.Join(s.Config.Storage, tjaHash+".tja"))
	if err != nil || string(stored) != source {
		t.Fatal("stored TJA changed", err)
	}
	code, body := c.raw("", song+"/download")
	if code != 200 {
		t.Fatal("download", code)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := archive.Open("song.tja")
	if err != nil {
		t.Fatal(err)
	}
	packaged, _ := io.ReadAll(f)
	want := "TITLE:Edited\nSUBTITLE:Someone\nTITLEEN:Stored\nTITLEJA:保存\nTITLEZH:改名\nSUBTITLEEN:--Artist\nBPM:150\n"
	if !strings.HasPrefix(string(packaged), want) || !strings.Contains(string(packaged), "#START\n1,\n#END") {
		t.Fatalf("packaged TJA:\n%s", packaged)
	}
}
