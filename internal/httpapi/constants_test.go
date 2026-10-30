package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/sitekit/constants"
	"ourtaiko.dev/sitekit/tja"
)

// Difficulty constants from storing to rating: every route stored with the blocks, rated by the
// first calibration, not recalibrated by the same data, refilled by the startup backfill; none
// of it touches the chart's update_at, which block changes still do (R010).
func TestDifficultyConstants(t *testing.T) {
	if !tja.Available() {
		t.Skip("tjaparser not found: set TJAPARSER_BIN or put it on PATH")
	}
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	source := []byte("TITLE:Constants\nBPM:120\nWAVE:a.ogg\nCOURSE:Hard\nLEVEL:5\n#START\n1020,\n1122,\n#END\n" +
		"COURSE:Oni\nLEVEL:8\n#START\n1000,\n#BRANCHSTART p,80,95\n#N\n1000,\n#E\n2020,\n#M\n3434,\n#BRANCHEND\n1,\n#END\n")
	sum := sha256.Sum256(source)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(s.Config.Storage, digest+".tja"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	var tjaID int64
	if err := s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.tja',$2,$3,'tja') RETURNING id`,
		digest+".tja", digest, len(source)).Scan(&tjaID); err != nil {
		t.Fatal(err)
	}
	var audioID int64
	if err := s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,'a.ogg',$2,1,'ogg') RETURNING id`,
		digest+".ogg", digest).Scan(&audioID); err != nil {
		t.Fatal(err)
	}
	// The titles as the backfill reads them from the file, so that it changes nothing else.
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET tja_file=$2,audio_file=$3,duration=10,title_translations='{"origin":"Constants"}',subtitle_translations='{}' WHERE id=$1`,
		w.song, tjaID, audioID); err != nil {
		t.Fatal(err)
	}
	meta, issue := tja.Parse(source, "")
	if issue != nil {
		t.Fatal(issue)
	}
	setDifficulties := func() {
		t.Helper()
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = database.SetDifficulties(ctx, tx, w.song, meta.Difficulties); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	setDifficulties()

	// Every route of each block, under its content hash: note count, main constant (nil until
	// rated), calibration; and the chart's update_at.
	type row struct {
		notes       int32
		main        *float64
		calibration *int32
	}
	stored := func() (map[string]row, time.Time) {
		t.Helper()
		rows, err := s.DB.Query(ctx, `SELECT d.course||'/'||k.route,k.note_count,k.main_constant,k.calibration_version
 FROM chart_difficulties d JOIN difficulty_constants k ON k.content_hash=d.content_hash WHERE d.chart_id=$1`, w.song)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := map[string]row{}
		for rows.Next() {
			var key string
			var r row
			if err = rows.Scan(&key, &r.notes, &r.main, &r.calibration); err != nil {
				t.Fatal(err)
			}
			result[key] = r
		}
		var updated time.Time
		if err = s.DB.QueryRow(ctx, `SELECT update_at FROM charts WHERE id=$1`, w.song).Scan(&updated); err != nil {
			t.Fatal(err)
		}
		return result, updated
	}
	uploaded, updated := stored()
	// Oni: 1 common note and 1 after the branch, with 1, 2 or 4 on the routes; not rated yet.
	notes := map[string]int32{"Hard/unbranched": 6, "Oni/normal": 3, "Oni/expert": 4, "Oni/master": 6}
	for key, n := range notes {
		if r, ok := uploaded[key]; !ok || r.notes != n || r.main != nil {
			t.Fatalf("stored %s: %+v", key, uploaded[key])
		}
	}
	if len(uploaded) != len(notes) {
		t.Fatalf("stored %v", uploaded)
	}

	// The first calibration rates every row, and touches no chart.
	result, err := constants.Recalibrate(ctx, s.DB)
	if err != nil || !result.New || result.Calibration.Version != 1 || result.Rated != 4 {
		t.Fatalf("first calibration: %+v %v", result, err)
	}
	rated, at := stored()
	for key, r := range rated {
		if r.main == nil || *r.calibration != 1 {
			t.Fatalf("%s not rated: %+v", key, r)
		}
	}
	if !at.Equal(updated) {
		t.Fatalf("calibrating touched the chart: update_at %v → %v", updated, at)
	}
	// The chart page gets every rated route of each block; lists go without.
	var detail Chart
	if code := w.c.do("", "GET", w.chart(w.song)+"?by=id", "", &detail); code != 200 {
		t.Fatal("detail", code)
	}
	for _, d := range detail.Difficulties {
		var routes []string
		for _, c := range d.Constants {
			routes = append(routes, c.Route)
		}
		want := map[string]string{"Hard": "unbranched", "Oni": "normal expert master"}[d.Course]
		last := d.Course + "/" + routes[len(routes)-1]
		if strings.Join(routes, " ") != want || d.Constants[len(d.Constants)-1].MainConstant != *rated[last].main {
			t.Fatalf("%s: constants %+v", d.Course, d.Constants)
		}
	}
	if code, body := w.c.raw("", "/api/v1/charts"); code != 200 || strings.Contains(string(body), `"constants"`) {
		t.Fatalf("the list carries constants: %d", code)
	}
	if again, err := constants.Recalibrate(ctx, s.DB); err != nil || again.New || again.Rated != 0 {
		t.Fatalf("second calibration: %+v %v", again, err)
	}

	// Without its rows (as before migration 012) the backfill brings them back, rated, and leaves
	// update_at alone.
	if _, err = s.DB.Exec(ctx, `DELETE FROM difficulty_constants`); err != nil {
		t.Fatal(err)
	}
	if err = s.backfillContentHashes(ctx, "v1:", constants.Version); err != nil {
		t.Fatal(err)
	}
	backfilled, at := stored()
	for key, r := range rated {
		if b := backfilled[key]; b.notes != r.notes || b.main == nil || *b.main != *r.main || !at.Equal(updated) {
			t.Fatalf("backfill %s: %+v, want %+v; update_at %v → %v", key, b, r, updated, at)
		}
	}
	// Storing the same reading again changes nothing.
	setDifficulties()
	if _, at := stored(); !at.Equal(updated) {
		t.Fatalf("same file again: update_at %v → %v", updated, at)
	}
	// A block's columns count as a change of the chart (R010), as before 011.
	if _, err := s.DB.Exec(ctx, `UPDATE chart_difficulties SET maker='Someone' WHERE chart_id=$1 AND course='Hard'`, w.song); err != nil {
		t.Fatal(err)
	}
	if _, at := stored(); !at.After(updated) {
		t.Fatal("a maker change no longer touches the chart")
	}
}
