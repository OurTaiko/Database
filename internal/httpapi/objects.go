package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Matches a chart_resources row r that some chart still refers to.
const referencedFile = `EXISTS(SELECT 1 FROM charts c WHERE c.tja_file=r.id OR c.audio_file=r.id OR c.cover_file=r.id OR c.preview_file=r.id)`

// fileExtension is the stored form: lowercase, without the dot.
func fileExtension(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// Identical files share one object, so writers and the cleaner serialize on
// its key until tx ends. Without this the cleaner could delete an object that
// an uncommitted upload is about to reference.
func lockObject(ctx context.Context, tx pgx.Tx, key string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "object:"+key)
	return e
}

// storeFile stores path as {sha256}.{extension} and returns its file ID,
// reusing the record and object of identical content (R005). The first
// registered name is kept. The caller references the ID in tx.
func (s *Server) storeFile(ctx context.Context, tx pgx.Tx, path, name, extension, media, digest string, size int64) (int64, error) {
	key := digest + "." + extension
	if e := lockObject(ctx, tx, key); e != nil {
		return 0, e
	}
	// Records the write before it happens, so an object left by a failed
	// upload can be found again.
	if _, e := s.DB.Exec(ctx, `INSERT INTO pending_objects(storage_key) VALUES($1) ON CONFLICT DO NOTHING`, key); e != nil {
		return 0, e
	}
	stored, e := s.objectStored(ctx, key, size)
	if e != nil {
		return 0, e
	}
	if !stored {
		f, e := os.Open(path)
		if e != nil {
			return 0, e
		}
		e = s.Config.Objects.Put(ctx, key, f, size, media, digest)
		f.Close()
		if e != nil {
			return 0, e
		}
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(storage_key) DO UPDATE SET storage_key=EXCLUDED.storage_key RETURNING id`, key, name, digest, size, extension).Scan(&id)
	if e == nil {
		_, e = tx.Exec(ctx, `DELETE FROM pending_objects WHERE storage_key=$1`, key)
	}
	return id, e
}

// fileDigest returns the SHA-256 and size of a staged file.
func fileDigest(path string) (string, int64, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, e
}

// objectStored reports whether key already holds the complete object. The
// caller holds its lock; an interrupted local write is removed for rewriting.
func (s *Server) objectStored(ctx context.Context, key string, size int64) (bool, error) {
	f, e := s.Config.Objects.Open(ctx, key)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	complete := f.Size() == size
	f.Close()
	if complete {
		return true, nil
	}
	return false, s.Config.Objects.Delete(ctx, key)
}

// releaseFiles deletes the given file records and their objects unless a
// chart still refers to them. Each check runs under the object's key lock.
func (s *Server) releaseFiles(ctx context.Context, ids []int64) error {
	var failures []error
	for _, id := range ids {
		failures = append(failures, s.releaseFile(ctx, id))
	}
	return errors.Join(failures...)
}

func (s *Server) releaseFile(ctx context.Context, id int64) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var key string
	e = tx.QueryRow(ctx, `SELECT storage_key FROM chart_resources WHERE id=$1`, id).Scan(&key)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = lockObject(ctx, tx, key); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `DELETE FROM chart_resources r WHERE r.id=$1 AND NOT `+referencedFile, id)
	if e != nil || tag.RowsAffected() == 0 {
		return e
	}
	// Queue first: a failed object delete stays visible in retired_files.
	if _, e = tx.Exec(ctx, `INSERT INTO retired_files(storage_key) VALUES($1) ON CONFLICT DO NOTHING`, key); e != nil {
		return e
	}
	if e = s.Config.Objects.Delete(ctx, key); e != nil {
		return errors.Join(e, tx.Commit(ctx))
	}
	if _, e = tx.Exec(ctx, `DELETE FROM retired_files WHERE storage_key=$1`, key); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
