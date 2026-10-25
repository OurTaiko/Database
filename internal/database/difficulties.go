package database

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/constants"
	"ourtaiko.dev/database/api/internal/tja"
)

// ConstantsColumnNames are chart_difficulties' difficulty-constant columns (migration 011), in
// the order ConstantsColumns gives their values.
const ConstantsColumnNames = `constants_version,raw_stamina,raw_handspeed,raw_burst,raw_complex,raw_complex_ratio,raw_rhythm,raw_rhythm_ratio,note_count`

// ConstantsColumns are the values for ConstantsColumnNames: all NULL without constants.
func ConstantsColumns(raw *constants.Raw) []any {
	if raw == nil {
		return make([]any, 9)
	}
	return []any{constants.Version, raw.Stamina, raw.Handspeed, raw.Burst, raw.Complex, raw.ComplexRatio, raw.Rhythm, raw.RhythmRatio, raw.NoteCount}
}

// SetDifficulties makes chart_difficulties hold exactly the given blocks of a
// chart. Rows are rewritten only when something differs, so storing the same
// file again neither changes their IDs nor touches the chart's update_at.
func SetDifficulties(ctx context.Context, tx pgx.Tx, chartID int64, blocks []tja.Difficulty) error {
	rows, err := tx.Query(ctx, `SELECT course,side,level,maker,branching,COALESCE(content_hash,''),`+ConstantsColumnNames+` FROM chart_difficulties WHERE chart_id=$1`, chartID)
	if err != nil {
		return err
	}
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tja.Difficulty, error) {
		var d tja.Difficulty
		var version *int16
		var raw constants.Raw
		var count *int32
		var values [7]*float64
		err := row.Scan(&d.Course, &d.Side, &d.Level, &d.Maker, &d.Branching, &d.Hash, &version,
			&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &count)
		if err == nil && version != nil && int(*version) == constants.Version {
			raw.Stamina, raw.Handspeed, raw.Burst, raw.Complex = *values[0], *values[1], *values[2], *values[3]
			raw.ComplexRatio, raw.Rhythm, raw.RhythmRatio, raw.NoteCount = *values[4], *values[5], *values[6], int(*count)
			d.Constants = &raw
		}
		return d, err
	})
	if err != nil {
		return err
	}
	wanted := map[[2]string]tja.Difficulty{}
	for _, d := range blocks {
		d.ID, d.Line = 0, 0
		wanted[[2]string{d.Course, d.Side}] = d
	}
	// A stored block whose constants are missing, or from another algorithm version, differs.
	sameBlock := func(a, b tja.Difficulty) bool {
		sameConstants := a.Constants == nil && b.Constants == nil || a.Constants != nil && b.Constants != nil && *a.Constants == *b.Constants
		a.Constants, b.Constants = nil, nil
		return a == b && sameConstants
	}
	same := len(stored) == len(wanted) && !slices.ContainsFunc(stored, func(d tja.Difficulty) bool {
		return !sameBlock(wanted[[2]string{d.Course, d.Side}], d)
	})
	if same {
		return nil
	}
	// Keep the rows, and with them the IDs, of slots that remain. A slot that
	// the new file no longer has goes with its scores, as on Fanmade (R037);
	// a retried submission of such a score is answered SCORE_REMOVED.
	for _, d := range stored {
		if _, ok := wanted[[2]string{d.Course, d.Side}]; ok {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO retired_score_requests(user_id,idempotency_key)
 SELECT user_id,idempotency_key FROM scores WHERE idempotency_key IS NOT NULL AND difficulty_id IN (SELECT id FROM chart_difficulties WHERE chart_id=$1 AND course=$2 AND side=$3) ON CONFLICT DO NOTHING`, chartID, d.Course, d.Side); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM scores WHERE difficulty_id IN (SELECT id FROM chart_difficulties WHERE chart_id=$1 AND course=$2 AND side=$3)`, chartID, d.Course, d.Side); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM chart_difficulties WHERE chart_id=$1 AND course=$2 AND side=$3`, chartID, d.Course, d.Side); err != nil {
			return err
		}
	}
	for _, d := range blocks {
		var hash *string
		if d.Hash != "" {
			hash = &d.Hash
		}
		// A changed content_hash retires the row's scores and invalidates 段位 songs (trigger).
		// Constants missing from the new reading keep the stored ones (all nine are NULL together).
		args := append([]any{chartID, d.Course, d.Side, d.Level, d.Maker, d.Branching, hash}, ConstantsColumns(d.Constants)...)
		_, err = tx.Exec(ctx, `INSERT INTO chart_difficulties(chart_id,course,side,level,maker,branching,content_hash,`+ConstantsColumnNames+`)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
 ON CONFLICT(chart_id,course,side) DO UPDATE SET level=EXCLUDED.level,maker=EXCLUDED.maker,branching=EXCLUDED.branching,content_hash=COALESCE(EXCLUDED.content_hash,chart_difficulties.content_hash),
 constants_version=COALESCE(EXCLUDED.constants_version,chart_difficulties.constants_version),
 raw_stamina=COALESCE(EXCLUDED.raw_stamina,chart_difficulties.raw_stamina),
 raw_handspeed=COALESCE(EXCLUDED.raw_handspeed,chart_difficulties.raw_handspeed),
 raw_burst=COALESCE(EXCLUDED.raw_burst,chart_difficulties.raw_burst),
 raw_complex=COALESCE(EXCLUDED.raw_complex,chart_difficulties.raw_complex),
 raw_complex_ratio=COALESCE(EXCLUDED.raw_complex_ratio,chart_difficulties.raw_complex_ratio),
 raw_rhythm=COALESCE(EXCLUDED.raw_rhythm,chart_difficulties.raw_rhythm),
 raw_rhythm_ratio=COALESCE(EXCLUDED.raw_rhythm_ratio,chart_difficulties.raw_rhythm_ratio),
 note_count=COALESCE(EXCLUDED.note_count,chart_difficulties.note_count)
 WHERE (chart_difficulties.level,chart_difficulties.maker,chart_difficulties.branching,chart_difficulties.content_hash) IS DISTINCT FROM (EXCLUDED.level,EXCLUDED.maker,EXCLUDED.branching,COALESCE(EXCLUDED.content_hash,chart_difficulties.content_hash))
  OR EXCLUDED.constants_version IS NOT NULL AND (chart_difficulties.constants_version,chart_difficulties.raw_stamina,chart_difficulties.raw_handspeed,
   chart_difficulties.raw_burst,chart_difficulties.raw_complex,chart_difficulties.raw_complex_ratio,chart_difficulties.raw_rhythm,
   chart_difficulties.raw_rhythm_ratio,chart_difficulties.note_count) IS DISTINCT FROM (EXCLUDED.constants_version,EXCLUDED.raw_stamina,
   EXCLUDED.raw_handspeed,EXCLUDED.raw_burst,EXCLUDED.raw_complex,EXCLUDED.raw_complex_ratio,EXCLUDED.raw_rhythm,EXCLUDED.raw_rhythm_ratio,EXCLUDED.note_count)`,
			args...)
		if err != nil {
			return err
		}
	}
	return nil
}
