package database

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/tja"
)

// SetDifficulties makes chart_difficulties hold exactly the given blocks of a
// chart. Rows are rewritten only when something differs, so storing the same
// file again neither changes their IDs nor touches the chart's update_at.
func SetDifficulties(ctx context.Context, tx pgx.Tx, chartID int64, blocks []tja.Difficulty) error {
	rows, err := tx.Query(ctx, `SELECT course,side,level,maker,branching,COALESCE(content_hash,'') FROM chart_difficulties WHERE chart_id=$1`, chartID)
	if err != nil {
		return err
	}
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tja.Difficulty, error) {
		var d tja.Difficulty
		return d, row.Scan(&d.Course, &d.Side, &d.Level, &d.Maker, &d.Branching, &d.Hash)
	})
	if err != nil {
		return err
	}
	wanted := map[[2]string]tja.Difficulty{}
	for _, d := range blocks {
		d.ID, d.Line = 0, 0
		wanted[[2]string{d.Course, d.Side}] = d
	}
	same := len(stored) == len(wanted) && !slices.ContainsFunc(stored, func(d tja.Difficulty) bool {
		return wanted[[2]string{d.Course, d.Side}] != d
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
		_, err = tx.Exec(ctx, `INSERT INTO chart_difficulties(chart_id,course,side,level,maker,branching,content_hash) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(chart_id,course,side) DO UPDATE SET level=EXCLUDED.level,maker=EXCLUDED.maker,branching=EXCLUDED.branching,content_hash=COALESCE(EXCLUDED.content_hash,chart_difficulties.content_hash)
 WHERE (chart_difficulties.level,chart_difficulties.maker,chart_difficulties.branching,chart_difficulties.content_hash) IS DISTINCT FROM (EXCLUDED.level,EXCLUDED.maker,EXCLUDED.branching,COALESCE(EXCLUDED.content_hash,chart_difficulties.content_hash))`,
			chartID, d.Course, d.Side, d.Level, d.Maker, d.Branching, hash)
		if err != nil {
			return err
		}
	}
	return nil
}
