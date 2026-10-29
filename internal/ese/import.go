// Package ese imports the ESE (Every Song Ever) TJA repository into the
// official chart database, attaching Nijiiro IDs from the ESE mapping file.
package ese

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ourtaiko.dev/database/api/internal/audio"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/objectstore"
	"ourtaiko.dev/database/api/internal/tja"
)

// Genre folders of the ESE repository and their category bit
// (charts.category_flags). Live Festival Mode, Taiko Towers and Dan Dojo are
// multi-song or non-standard courses the charts table cannot describe; they
// are left out.
var folders = map[string]int16{
	"09 Namco Original":    0x01,
	"01 Pop":               0x02,
	"07 Game Music":        0x04,
	"02 Anime":             0x08,
	"03 Vocaloid":          0x10,
	"05 Variety":           0x20,
	"04 Children and Folk": 0x40,
	"06 Classical":         0x80,
}

type Options struct {
	// Root is the ESE checkout; Mapping is the Nijiiro ID → ESE path JSON.
	Root, Mapping string
	// Objects receives the files. A Local store under the same volume as the
	// ESE checkout gets copy-on-write clones of the audio.
	Objects objectstore.Store
	Workers int
	// DryRun parses and hashes everything but writes neither files nor rows.
	DryRun bool
}

type Report struct {
	Scanned   int               `json:"scanned"`
	Added     int               `json:"added"`
	Updated   int               `json:"updated"`
	Unchanged int               `json:"unchanged"`
	Removed   int               `json:"skippedDeleted"`
	Excluded  int               `json:"excluded"`
	Mapped    int               `json:"withNijiiroId"`
	Unmatched []string          `json:"mappingWithoutChart"`
	Issues    map[string]string `json:"issues"`
}

type entry struct {
	path      string
	nijiiro   *int32
	flags     int16
	issue     string
	meta      tja.Metadata
	tjaData   []byte
	tjaHash   string
	audioPath string
	audioName string
	audioHash string
	audioSize int64
	duration  float64
}

func hashBytes(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func fileDigest(name string) (string, int64, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// loadMapping returns ESE path → Nijiiro ID. Paths in the file use
// backslashes; empty values are songs ESE does not have.
func loadMapping(name string) (map[string]int32, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var data map[string]string
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid mapping file: %w", err)
	}
	result := map[string]int32{}
	for key, value := range data {
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(key, 10, 32)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid Nijiiro ID %q in mapping", key)
		}
		p := path.Clean(strings.ReplaceAll(value, "\\", "/"))
		if other, exists := result[p]; exists {
			return nil, fmt.Errorf("%s is mapped to both %d and %d", p, other, id)
		}
		result[p] = int32(id)
	}
	return result, nil
}

func scan(root string) ([]string, error) {
	var paths []string
	for folder := range folders {
		err := filepath.WalkDir(filepath.Join(root, folder), func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(name), ".tja") {
				rel, err := filepath.Rel(root, name)
				if err != nil {
					return err
				}
				paths = append(paths, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// prepare parses one chart and hashes its audio. A chart the database cannot
// hold gets an issue instead of an error.
func prepare(root string, e *entry) error {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.path)))
	if err != nil {
		return err
	}
	data, issue := tja.NormalizeUTF8(raw, tja.DetectEncoding(raw))
	if issue == nil {
		e.meta, issue = tja.Parse(data, "")
	}
	if issue != nil {
		e.issue = fmt.Sprintf("%s (line %d): %s", issue.Code, issue.Line, issue.Message)
		return nil
	}
	e.audioName = e.meta.Wave
	e.audioPath = filepath.Join(root, filepath.FromSlash(path.Dir(e.path)), e.audioName)
	if info, err := os.Stat(e.audioPath); err != nil || !info.Mode().IsRegular() {
		e.issue = "AUDIO_MISSING: " + e.audioName
		return nil
	}
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.audioName), "."))
	if extension != "ogg" {
		e.issue = "AUDIO_UNSUPPORTED: " + e.audioName
		return nil
	}
	if e.duration, err = audio.VorbisDuration(e.audioPath); err != nil {
		e.issue = "AUDIO_INVALID: " + err.Error()
		return nil
	}
	if e.duration <= 0 || e.duration > 1200 {
		e.issue = fmt.Sprintf("AUDIO_DURATION_INVALID: %.1f s", e.duration)
		return nil
	}
	if e.audioHash, e.audioSize, err = fileDigest(e.audioPath); err != nil {
		return err
	}
	if e.audioSize > tja.MaxAudio {
		e.issue = "AUDIO_TOO_LARGE: " + e.audioName
		return nil
	}
	// R007: the stored TJA names its audio by content hash; the TJA is hashed
	// after that rewrite.
	e.meta.Wave = e.audioHash + "." + extension
	rewritten, ok := tja.SetWave(data, e.meta.Wave)
	if !ok {
		e.issue = "TJA_WAVE_MISSING: WAVE header not found"
		return nil
	}
	e.tjaData, e.tjaHash = rewritten, hashBytes(rewritten)
	return nil
}

// Run imports every chart of the ESE genre folders. It is repeatable: a chart
// is identified by its ESE path, and an unchanged one is left alone.
func Run(ctx context.Context, pool *pgxpool.Pool, o Options) (Report, error) {
	report := Report{Issues: map[string]string{}, Unmatched: []string{}}
	mapping, err := loadMapping(o.Mapping)
	if err != nil {
		return report, err
	}
	paths, err := scan(o.Root)
	if err != nil {
		return report, err
	}
	entries := make([]*entry, len(paths))
	found := map[string]bool{}
	for i, p := range paths {
		e := &entry{path: p, flags: folders[strings.SplitN(p, "/", 2)[0]]}
		if id, ok := mapping[p]; ok {
			e.nijiiro = &id
			found[p] = true
		}
		entries[i] = e
	}
	for p, id := range mapping {
		if !found[p] {
			report.Unmatched = append(report.Unmatched, fmt.Sprintf("%d: %s", id, p))
		}
	}
	sort.Strings(report.Unmatched)
	// Row IDs follow this order: charts with a Nijiiro ID first, by ID.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.nijiiro == nil) != (b.nijiiro == nil) {
			return a.nijiiro != nil
		}
		if a.nijiiro != nil && *a.nijiiro != *b.nijiiro {
			return *a.nijiiro < *b.nijiiro
		}
		return a.path < b.path
	})
	report.Scanned = len(entries)

	workers := max(1, o.Workers)
	jobs := make(chan *entry)
	var wg sync.WaitGroup
	var failure error
	var once sync.Once
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if err := prepare(o.Root, e); err != nil {
					once.Do(func() { failure = fmt.Errorf("%s: %w", e.path, err); cancel() })
				}
			}
		}()
	}
	for i, e := range entries {
		jobs <- e
		if (i+1)%500 == 0 {
			log.Printf("prepared %d/%d charts", i+1, len(entries))
		}
	}
	close(jobs)
	wg.Wait()
	if failure != nil {
		return report, failure
	}

	if local, ok := o.Objects.(objectstore.Local); ok && !o.DryRun {
		if err = os.MkdirAll(local.Root, 0700); err != nil {
			return report, err
		}
	}
	for i, e := range entries {
		if e.issue != "" {
			report.Excluded++
			report.Issues[e.path] = e.issue
			continue
		}
		if e.nijiiro != nil {
			report.Mapped++
		}
		if o.DryRun {
			continue
		}
		outcome, err := store(ctx, pool, o.Objects, e)
		if err != nil {
			return report, fmt.Errorf("%s: %w", e.path, err)
		}
		switch outcome {
		case "added":
			report.Added++
		case "updated":
			report.Updated++
		case "deleted":
			report.Removed++
		default:
			report.Unchanged++
		}
		if (i+1)%500 == 0 {
			log.Printf("stored %d/%d charts", i+1, len(entries))
		}
	}
	return report, nil
}

// putObject places a file under its content-addressed key unless a complete
// copy is already there. source is the file's path, or empty when data holds
// the bytes.
func putObject(ctx context.Context, objects objectstore.Store, key, media, digest string, data []byte, source string) error {
	size := int64(len(data))
	if source != "" {
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		size = info.Size()
	}
	if local, ok := objects.(objectstore.Local); ok {
		target := filepath.Join(local.Root, key)
		if info, err := os.Stat(target); err == nil && info.Size() == size {
			return nil
		}
		tmp := target + ".importing"
		os.Remove(tmp)
		var err error
		if source != "" {
			err = cloneFile(source, tmp)
		} else {
			err = os.WriteFile(tmp, data, 0600)
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, target)
	}
	if existing, err := objects.Open(ctx, key); err == nil {
		complete := existing.Size() == size
		existing.Close()
		if complete {
			return nil
		}
		return fmt.Errorf("object %s exists with a different size", key)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var body io.ReadSeeker = bytes.NewReader(data)
	if source != "" {
		f, err := os.Open(source)
		if err != nil {
			return err
		}
		defer f.Close()
		body = f
	}
	if err := objects.Put(ctx, key, body, size, media, digest); !objectstore.AlreadyStored(err) {
		return err
	}
	return nil
}

func fileRecord(ctx context.Context, tx pgx.Tx, name, digest, extension string, size int64) (int64, error) {
	key := digest + "." + extension
	// Uploads and the file cleaner serialize on the same key.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "object:"+key); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(storage_key) DO UPDATE SET storage_key=EXCLUDED.storage_key RETURNING id`, key, name, digest, size, extension).Scan(&id)
	return id, err
}

func store(ctx context.Context, pool *pgxpool.Pool, objects objectstore.Store, e *entry) (string, error) {
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.audioName), "."))
	difficulties, err := json.Marshal(e.meta.Difficulties)
	if err != nil {
		return "", err
	}
	titles, _ := json.Marshal(e.meta.TitleTranslations)
	subtitles, _ := json.Marshal(e.meta.SubtitleTranslations)
	demoStart := e.meta.DemoStart
	if demoStart < 0 || demoStart >= e.duration {
		demoStart = 0
	}
	description := "Source: ESE (TJADB) · " + e.path
	// Everything a row is built from: a change in any of it updates the chart.
	digest := hashBytes(fmt.Appendf(nil, "%s\n%s\n%v\n%d\n%s\n%s\n%s\n%s\n%s\n%v\n%v\n%v\n%v\n%v\n%s", e.tjaHash, e.audioHash, e.nijiiro != nil, nijiiroValue(e.nijiiro), e.meta.Title, e.meta.Subtitle, titles, subtitles, difficulties, e.meta.BPM, e.meta.Offset, demoStart, e.duration, e.flags, description))
	key := "ese:" + e.path

	if err = putObject(ctx, objects, e.tjaHash+".tja", "application/octet-stream", e.tjaHash, e.tjaData, ""); err != nil {
		return "", err
	}
	if err = putObject(ctx, objects, e.audioHash+"."+extension, audio.MediaType(e.audioName), e.audioHash, nil, e.audioPath); err != nil {
		return "", err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var recorded string
	var chartID *int64
	err = tx.QueryRow(ctx, `SELECT payload_digest,chart_id FROM upload_requests WHERE user_id=$1 AND idempotency_key=$2 FOR UPDATE`, database.SystemUploaderID, key).Scan(&recorded, &chartID)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if exists && chartID == nil {
		// The chart was deleted on the site; a re-import does not bring it back.
		return "deleted", nil
	}
	if exists && recorded == digest {
		return "unchanged", nil
	}
	tjaFile, err := fileRecord(ctx, tx, path.Base(e.path), e.tjaHash, "tja", int64(len(e.tjaData)))
	if err != nil {
		return "", err
	}
	audioFile, err := fileRecord(ctx, tx, e.audioName, e.audioHash, extension, e.audioSize)
	if err != nil {
		return "", err
	}
	if exists {
		_, err = tx.Exec(ctx, `UPDATE charts SET nijiiro_id=$2,tja_file=$3,audio_file=$4,description=$5,title=$6,subtitle=$7,bpm=$8,offset_seconds=$9,demo_start=$10,duration=$11,
 title_translations=$12,subtitle_translations=$13,category_flags=$14,demo_end=$15 WHERE id=$1`,
			*chartID, e.nijiiro, tjaFile, audioFile, description, e.meta.Title, e.meta.Subtitle, e.meta.BPM, e.meta.Offset, demoStart, e.duration, titles, subtitles, e.flags, demoStart+15)
		if err == nil {
			err = database.SetDifficulties(ctx, tx, *chartID, e.meta.Difficulties)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE upload_requests SET payload_digest=$3,tja_sha256=$4,audio_sha256=$5 WHERE user_id=$1 AND idempotency_key=$2`, database.SystemUploaderID, key, digest, e.tjaHash, e.audioHash)
		}
		if err != nil {
			return "", err
		}
		return "updated", tx.Commit(ctx)
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO charts(nijiiro_id,uploader_id,tja_file,audio_file,description,title,subtitle,bpm,offset_seconds,demo_start,duration,title_translations,subtitle_translations,category_flags,demo_end)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
		e.nijiiro, database.SystemUploaderID, tjaFile, audioFile, description, e.meta.Title, e.meta.Subtitle, e.meta.BPM, e.meta.Offset, demoStart, e.duration, titles, subtitles, e.flags, demoStart+15).Scan(&id)
	if err != nil {
		return "", err
	}
	if err = database.SetDifficulties(ctx, tx, id, e.meta.Difficulties); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO upload_requests(user_id,idempotency_key,payload_digest,chart_id,tja_sha256,audio_sha256) VALUES($1,$2,$3,$4,$5,$6)`, database.SystemUploaderID, key, digest, id, e.tjaHash, e.audioHash); err != nil {
		return "", err
	}
	return "added", tx.Commit(ctx)
}

func nijiiroValue(id *int32) int32 {
	if id == nil {
		return 0
	}
	return *id
}
