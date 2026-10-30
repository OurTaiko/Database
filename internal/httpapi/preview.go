package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/audio"
)

// refreshPreview cuts the game's song-select preview clip (as Fanmade's): 15 seconds of
// Ogg at the chart's preview range, listed in the resource manifest as "preview". source
// is a local copy of the chart's current audio, or "" to read the stored one. With force
// false a chart that has a clip is left alone (the backfill). It runs after the change
// that calls for it has committed, under the chart's row lock, so the range and audio it
// reads are current; a failure leaves the chart without a clip, which the backfill retries.
func (s *Server) refreshPreview(parent context.Context, id string, source string, force bool) (err error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var audioKey, audioSHA string
	var audioSize int64
	var start, end, duration float64
	var previous *int64
	err = tx.QueryRow(ctx, `SELECT r.storage_key,r.sha256,r.byte_size,c.demo_start,c.demo_end,c.duration,c.preview_file FROM charts c JOIN chart_resources r ON r.id=c.audio_file
 WHERE c.id=$1 AND `+publishedChart+` FOR UPDATE OF c`, id).Scan(&audioKey, &audioSHA, &audioSize, &start, &end, &duration, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || (!force && previous != nil) {
		return err
	}
	dir, err := os.MkdirTemp(s.Config.Storage, "preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if source == "" {
		source = filepath.Join(dir, "audio")
		if err = s.fetch(ctx, audioKey, audioSHA, audioSize, source); err != nil {
			return err
		}
	}
	output := filepath.Join(dir, "preview.ogg")
	if err = audio.Preview(ctx, source, output, start, end, duration); err != nil {
		// A stale clip would play the old range: drop it so the backfill makes a new one.
		if force && previous != nil {
			if _, e := tx.Exec(ctx, `UPDATE charts SET preview_file=NULL WHERE id=$1`, id); e == nil && tx.Commit(ctx) == nil {
				err = errors.Join(err, s.releaseFiles(parent, []int64{*previous}))
			}
		}
		return err
	}
	digest, size, err := fileDigest(output)
	if err != nil {
		return err
	}
	file, err := s.storeFile(ctx, tx, output, "preview.ogg", "ogg", "audio/ogg", digest, size)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE charts SET preview_file=$2 WHERE id=$1`, id, file); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if previous != nil && *previous != file {
		return s.releaseFiles(parent, []int64{*previous})
	}
	return nil
}

// fetch copies a stored file into path and verifies it against its record (as Fanmade's).
func (s *Server) fetch(ctx context.Context, key, sha string, size int64, path string) error {
	in, err := s.Config.Objects.Open(ctx, key)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, size+1))
	if err = errors.Join(err, out.Close()); err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sha {
		return fmt.Errorf("stored file integrity mismatch: %s", key)
	}
	return nil
}

// previewAfter refreshes a chart's clip after a request changed its audio or range. The
// change itself stands either way; a failure is logged and left to the backfill.
func (s *Server) previewAfter(ctx context.Context, id string, source string) {
	if err := s.refreshPreview(context.WithoutCancel(ctx), id, source, true); err != nil {
		log.Printf("preview clip for chart %s pending: %v", id, err)
	}
}

// BackfillPreviews cuts the clip of every published chart with files that has none,
// oldest first; a failure is logged and retried on the next pass.
func (s *Server) BackfillPreviews(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT c.id FROM charts c WHERE `+publishedChart+` AND c.audio_file IS NOT NULL AND c.preview_file IS NULL ORDER BY c.created_at,c.id`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		log.Printf("preview clip backfill: %d charts", len(ids))
	}
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.refreshPreview(ctx, id, "", false); err != nil {
			failed++
			log.Printf("preview clip for chart %s failed: %v", id, err)
		}
	}
	if len(ids) > 0 {
		log.Printf("preview clip backfill: %d charts done, %d failed", len(ids)-failed, failed)
	}
	return nil
}

// RunPreviewBackfill runs BackfillPreviews at start-up and every hour.
func (s *Server) RunPreviewBackfill(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := s.BackfillPreviews(ctx); err != nil && ctx.Err() == nil {
			log.Printf("preview clip backfill pending: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
