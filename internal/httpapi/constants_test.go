package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ourtaiko.dev/database/api/internal/constants"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/tja"
)

// A stored chart's blocks carry the raw difficulty constants (a branching block its master
// route); the startup backfill fills blocks stored without them (before migration 011). Being
// derived, they leave the chart's update_at alone (R010 counts the other block columns).
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

	type row struct {
		version         int16
		stamina, rhythm float64
		notes           int32
	}
	stored := func() (map[string]*row, time.Time) {
		t.Helper()
		rows, err := s.DB.Query(ctx, `SELECT course,constants_version,raw_stamina,raw_rhythm,note_count FROM chart_difficulties WHERE chart_id=$1`, w.song)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := map[string]*row{}
		for rows.Next() {
			var course string
			var version *int16
			var stamina, rhythm *float64
			var notes *int32
			if err = rows.Scan(&course, &version, &stamina, &rhythm, &notes); err != nil {
				t.Fatal(err)
			}
			result[course] = nil
			if version != nil {
				result[course] = &row{*version, *stamina, *rhythm, *notes}
			}
		}
		var updated time.Time
		if err = s.DB.QueryRow(ctx, `SELECT update_at FROM charts WHERE id=$1`, w.song).Scan(&updated); err != nil {
			t.Fatal(err)
		}
		return result, updated
	}
	uploaded, updated := stored()
	// Oni: 1 common note, 4 on the master route, 1 after.
	if uploaded["Hard"] == nil || uploaded["Oni"] == nil || uploaded["Hard"].notes != 6 || uploaded["Oni"].notes != 6 ||
		uploaded["Oni"].version != constants.Version || uploaded["Oni"].stamina <= 0 {
		t.Fatalf("stored %+v %+v", uploaded["Hard"], uploaded["Oni"])
	}

	if _, err := s.DB.Exec(ctx, `UPDATE chart_difficulties SET (`+database.ConstantsColumnNames+`)=(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL) WHERE chart_id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	cleared, at := stored()
	if cleared["Hard"] != nil || !at.Equal(updated) {
		t.Fatalf("clearing the constants: %+v, update_at %v → %v", cleared["Hard"], updated, at)
	}
	if err := s.backfillContentHashes(ctx, "v1:", constants.Version); err != nil {
		t.Fatal(err)
	}
	backfilled, at := stored()
	if *backfilled["Hard"] != *uploaded["Hard"] || *backfilled["Oni"] != *uploaded["Oni"] || !at.Equal(updated) {
		t.Fatalf("backfill: %+v %+v, update_at %v → %v", backfilled["Hard"], backfilled["Oni"], updated, at)
	}
	// Storing the same reading again changes nothing.
	setDifficulties()
	if again, at := stored(); *again["Oni"] != *uploaded["Oni"] || !at.Equal(updated) {
		t.Fatalf("same file again: %+v, update_at %v → %v", again["Oni"], updated, at)
	}
	// A block's other columns still count as a change of the chart.
	if _, err := s.DB.Exec(ctx, `UPDATE chart_difficulties SET maker='Someone' WHERE chart_id=$1 AND course='Hard'`, w.song); err != nil {
		t.Fatal(err)
	}
	if _, at := stored(); !at.After(updated) {
		t.Fatal("a maker change no longer touches the chart")
	}
}
