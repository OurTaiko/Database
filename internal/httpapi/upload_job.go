package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/sitekit/audio"
	"ourtaiko.dev/sitekit/cover"
	"ourtaiko.dev/sitekit/tja"
	"ourtaiko.dev/sitekit/video"
)

// uploadJob is an accepted upload (as Fanmade's): its files are staged and its fast checks
// passed. Decoding, transcoding and storing happen after the request returns.
type uploadJob struct {
	user, key, dir           string
	existingID               string // set when replacing a chart's files
	audioHash, movieHash     string // files of that chart the upload keeps
	tja, audio, cover, movie *stagedFile
	normalized               []byte
	meta                     tja.Metadata
	description              string
	flags                    CategoryFlags
	nijiiroID                *int32
	officialID               *string
	fields                   map[string]string // the sent preview range, read once the audio is
	keepMovie, keepTitles    bool
	async                    bool
	done                     chan uploadResult
}

// fileFailure names an optional file that was left out of a published chart.
type fileFailure struct {
	File string `json:"file"`
	Code string `json:"code"`
}

// uploadError rejects the whole upload. cause marks an internal failure.
type uploadError struct {
	status        int
	code, message string
	cause         error
}

type uploadResult struct {
	chart Chart
	err   *uploadError
}

const (
	// Jobs waiting with staged files; temporary storage holds about this many.
	maxPendingUploads = 4
	uploadTimeout     = 20 * time.Minute
	movieTimeout      = 15 * time.Minute
)

func (s *Server) enqueue(j *uploadJob) {
	s.worker.Do(func() { go s.runUploads() })
	s.jobs <- j
}

// One job at a time: decoding and transcoding are CPU-bound.
func (s *Server) runUploads() {
	for j := range s.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
		result := s.processUpload(ctx, j)
		os.RemoveAll(j.dir)
		s.finishUpload(j, result)
		cancel()
		<-s.pending
		j.done <- result
	}
}

func rejected(status int, code, message string) uploadResult {
	return uploadResult{err: &uploadError{status: status, code: code, message: message}}
}
func failed(cause error) uploadResult {
	return uploadResult{err: &uploadError{status: 503, code: "SERVICE_UNAVAILABLE", cause: cause}}
}

// processUpload decodes and stores an accepted upload. A bad TJA, audio or preview range
// rejects everything before any object is written; a bad cover or movie is left out and
// reported while the chart is still published.
func (s *Server) processUpload(ctx context.Context, j *uploadJob) uploadResult {
	var failures []fileFailure
	af := j.audio
	var current Chart
	duration := 0.0
	if j.existingID != "" {
		var err error
		current, err = s.chart(ctx, j.existingID)
		if errors.Is(err, pgx.ErrNoRows) {
			return rejected(404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		}
		if err != nil {
			return failed(err)
		}
		// Kept files must still be the ones the uploader saw.
		if (af == nil && current.AudioHash != j.audioHash) || (j.keepMovie && current.MovieHash != j.movieHash) {
			return rejected(409, "CHART_FILES_CHANGED", "该乐曲的文件已被其他更新替换，请重新打开谱面页面")
		}
		duration = current.Duration
	}
	if af != nil {
		var err error
		if duration, err = audio.Validate(ctx, af.path, af.name); err != nil {
			log.Printf("upload %s audio rejected: %v", j.key, err)
			return rejected(422, "AUDIO_INVALID", "音频无法解码，或不是单音轨的 OGG Vorbis / MP3，或时长超过 20 分钟")
		}
	}
	demoStart, demoEnd, ok := previewRange(j.fields, j.meta.DemoStart, duration)
	if !ok {
		return rejected(422, "PREVIEW_RANGE_INVALID", "试听起点必须位于音频内，终点须晚于起点且不超过 1215 秒")
	}
	var coverData []byte
	if j.cover != nil {
		raw, err := os.ReadFile(j.cover.path)
		if err != nil {
			return failed(err)
		}
		coverData, err = cover.Encode(ctx, j.cover.name, raw)
		if errors.Is(err, cover.ErrInvalid) {
			failures = append(failures, fileFailure{"cover", "COVER_INVALID"})
		} else if err != nil {
			return failed(err)
		}
	}
	// The movie is stored without sound, re-encoded only if over the limits.
	var moviePath, movieSHA string
	var movieSize int64
	if j.movie != nil {
		moviePath = filepath.Join(j.dir, "movie."+video.Extension)
		limited, cancel := context.WithTimeout(ctx, movieTimeout)
		err := video.Transcode(limited, j.movie.path, moviePath)
		cancel()
		switch {
		case errors.Is(err, video.ErrInvalid):
			failures = append(failures, fileFailure{"movie", "MOVIE_INVALID"})
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			failures = append(failures, fileFailure{"movie", "MOVIE_TOO_SLOW"})
		case err != nil:
			return failed(err)
		default:
			if movieSHA, movieSize, err = fileDigest(moviePath); err != nil {
				return failed(err)
			}
		}
	} else if j.keepMovie {
		movieSHA = j.movieHash
	}
	// R007: the stored TJA names its audio and movie by content, matching their objects. A
	// movie that was left out is no longer declared.
	wave := current.Wave
	if af != nil {
		wave = af.sha + "." + fileExtension(af.name)
	}
	stored, ok := tja.SetWave(j.normalized, wave)
	if ok && j.meta.Movie != "" {
		name := ""
		if movieSHA != "" {
			name = movieSHA + "." + video.Extension
		}
		stored, ok = tja.SetMovie(stored, name)
	}
	if !ok {
		return failed(errors.New("validated TJA lost a rewritten header"))
	}
	tjaPath := filepath.Join(j.dir, "stored.tja")
	if err := os.WriteFile(tjaPath, stored, 0600); err != nil {
		return failed(err)
	}
	tjaSHA := hash(string(stored))

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return failed(err)
	}
	defer tx.Rollback(context.Background())
	var previousTJA, previousAudio, previousCover, previousMovie *int64
	if j.existingID != "" {
		// Serialize with other replacements, deletion and score submissions, and confirm the
		// kept files once more now that nothing can change them.
		var audioSHA, movieHash string
		err = tx.QueryRow(ctx, `SELECT c.tja_file,c.audio_file,c.cover_file,c.movie_file,COALESCE(af.sha256,''),COALESCE(mf.sha256,'') FROM charts c
 LEFT JOIN chart_resources af ON af.id=c.audio_file LEFT JOIN chart_resources mf ON mf.id=c.movie_file WHERE c.id=$1 AND `+publishedChart+` FOR UPDATE OF c`,
			j.existingID).Scan(&previousTJA, &previousAudio, &previousCover, &previousMovie, &audioSHA, &movieHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return rejected(404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		}
		if err != nil {
			return failed(err)
		}
		if (af == nil && (previousAudio == nil || audioSHA != j.audioHash)) || (j.keepMovie && movieHash != j.movieHash) {
			return rejected(409, "CHART_FILES_CHANGED", "该乐曲的文件已被其他更新替换，请重新打开谱面页面")
		}
	}
	tjaFile, err := s.storeFile(ctx, tx, tjaPath, j.tja.name, "tja", "application/octet-stream", tjaSHA, int64(len(stored)))
	if err != nil {
		return failed(err)
	}
	var audioFile int64
	if af != nil {
		if audioFile, err = s.storeFile(ctx, tx, af.path, af.name, fileExtension(af.name), audio.MediaType(af.name), af.sha, af.size); err != nil {
			return failed(err)
		}
	} else {
		audioFile = *previousAudio
	}
	coverFile := previousCover
	if coverData != nil {
		id, err := s.storeImage(ctx, tx, coverData)
		if err != nil {
			return failed(err)
		}
		coverFile = &id
	}
	var movieFile *int64
	if moviePath != "" && movieSHA != "" {
		id, err := s.storeFile(ctx, tx, moviePath, "movie."+video.Extension, video.Extension, video.MediaType, movieSHA, movieSize)
		if err != nil {
			return failed(err)
		}
		movieFile = &id
	} else if j.keepMovie {
		movieFile = previousMovie
	}
	titles, _ := json.Marshal(j.meta.TitleTranslations)
	subtitles, _ := json.Marshal(j.meta.SubtitleTranslations)
	id := j.existingID
	if id != "" {
		// Names and their translations come from the new TJA again, as on Fanmade, unless
		// keepTitles keeps the chart's own.
		_, err = tx.Exec(ctx, `UPDATE charts SET tja_file=$2,audio_file=$3,cover_file=$4,description=$5,category_flags=$6,nijiiro_id=$7,official_id=$8,
 bpm=$11,offset_seconds=$12,demo_start=$13,demo_end=$14,duration=$15,movie_file=$19,movie_offset=$20,
 title=CASE WHEN $18 THEN title ELSE $9 END,subtitle=CASE WHEN $18 THEN subtitle ELSE $10 END,
 title_translations=CASE WHEN $18 THEN title_translations ELSE $16 END,subtitle_translations=CASE WHEN $18 THEN subtitle_translations ELSE $17 END WHERE id=$1`,
			id, tjaFile, audioFile, coverFile, j.description, j.flags, j.nijiiroID, j.officialID,
			j.meta.Title, j.meta.Subtitle, j.meta.BPM, j.meta.Offset, demoStart, demoEnd, duration, titles, subtitles, j.keepTitles, movieFile, j.meta.MovieOffset)
	} else {
		id = ID()
		_, err = tx.Exec(ctx, `INSERT INTO charts(id,nijiiro_id,official_id,owner_ids,tja_file,audio_file,cover_file,description,title,subtitle,bpm,offset_seconds,demo_start,duration,title_translations,subtitle_translations,category_flags,demo_end,movie_file,movie_offset)
 VALUES($20,$1,$2,ARRAY[$3::text],$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
			j.nijiiroID, j.officialID, j.user, tjaFile, audioFile, coverFile, j.description, j.meta.Title, j.meta.Subtitle, j.meta.BPM, j.meta.Offset, demoStart, duration, titles, subtitles, j.flags, demoEnd, movieFile, j.meta.MovieOffset, id)
	}
	if err != nil {
		return failed(err)
	}
	if err = database.SetDifficulties(ctx, tx, id, j.meta.Difficulties); err != nil {
		return failed(err)
	}
	audioSHA := j.audioHash
	if af != nil {
		audioSHA = af.sha
	}
	if _, err = tx.Exec(ctx, `UPDATE upload_requests SET status='done',chart_id=$3,tja_sha256=$4,audio_sha256=$5 WHERE user_id=$1 AND idempotency_key=$2`, j.user, j.key, id, tjaSHA, audioSHA); err != nil {
		return failed(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return failed(err)
	}
	// Shared files stay while another chart or playlist refers to them.
	var retired []int64
	for _, change := range []struct {
		before *int64
		after  *int64
	}{{previousTJA, &tjaFile}, {previousAudio, &audioFile}, {previousCover, coverFile}, {previousMovie, movieFile}} {
		if change.before != nil && (change.after == nil || *change.after != *change.before) {
			retired = append(retired, *change.before)
		}
	}
	if err = s.releaseFiles(ctx, retired); err != nil {
		log.Printf("replaced chart file cleanup pending: %v", err)
	}
	// The game's preview clip follows a new audio or range.
	if j.existingID == "" || af != nil || demoStart != current.DemoStart || demoEnd != current.DemoEnd {
		source := ""
		if af != nil {
			source = af.path
		}
		s.previewAfter(ctx, id, source)
	}
	chart, err := s.chart(ctx, id)
	if err != nil {
		return failed(err)
	}
	chart.FailedFiles = failures
	return uploadResult{chart: chart}
}

// finishUpload releases a rejected upload's receipt, so the same request can be sent again,
// and tells an uploader who did not wait what happened.
func (s *Server) finishUpload(j *uploadJob, result uploadResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data := map[string]any{"title": j.meta.Title, "status": "published"}
	chartID := result.chart.ID
	if j.existingID != "" {
		data["status"] = "updated"
	}
	if result.err != nil {
		if result.err.cause != nil {
			log.Printf("upload %s failed: %v", j.key, result.err.cause)
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM upload_requests WHERE user_id=$1 AND idempotency_key=$2 AND status='processing'`, j.user, j.key); err != nil {
			log.Printf("upload receipt cleanup failed: %v", err)
		}
		data["status"], data["code"], chartID = "failed", result.err.code, j.existingID
	} else if len(result.chart.FailedFiles) > 0 {
		data["failedFiles"] = result.chart.FailedFiles
	}
	if !j.async {
		return
	}
	if err := notify(ctx, s.DB, Notice{Recipient: j.user, Kind: "upload_result", ChartID: chartID, DedupeKey: "upload:" + j.key, Data: data}); err != nil {
		log.Printf("upload notification failed: %v", err)
	}
}

// FailInterruptedUploads runs once at startup. Staged files do not survive a restart, so
// uploads that were still being processed are reported as failed.
func (s *Server) FailInterruptedUploads(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `DELETE FROM upload_requests WHERE status='processing' RETURNING user_id,idempotency_key,title,COALESCE(chart_id,'')`)
	if err != nil {
		return err
	}
	type interrupted struct {
		user, key, title, chart string
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (v interrupted, e error) {
		e = row.Scan(&v.user, &v.key, &v.title, &v.chart)
		return
	})
	if err != nil {
		return err
	}
	for _, v := range items {
		if err = notify(ctx, s.DB, Notice{Recipient: v.user, Kind: "upload_result", ChartID: v.chart, DedupeKey: "upload:" + v.key,
			Data: map[string]any{"title": v.title, "status": "failed", "code": "UPLOAD_INTERRUPTED"}}); err != nil {
			return err
		}
	}
	return nil
}

// acceptUpload records an accepted upload's receipt and queues its job; the request then
// waits for the result, or with Prefer: respond-async is answered 202 at once. A retried
// request (same key and payload) is answered from its receipt instead. The caller has
// reserved a pending slot, which this returns when the upload is not queued, and owns the
// staging directory until accepted reports true.
func (s *Server) acceptUpload(w http.ResponseWriter, r *http.Request, j *uploadJob, digest string, existing *Chart) (accepted bool) {
	ctx := r.Context()
	defer func() {
		if !accepted {
			<-s.pending
		}
	}()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		internal(w, err)
		return false
	}
	defer tx.Rollback(ctx)
	var chartID *string
	if existing != nil {
		chartID = &existing.ID
	}
	tag, err := tx.Exec(ctx, `INSERT INTO upload_requests(user_id,idempotency_key,payload_digest,chart_id,status,title) VALUES($1,$2,$3,$4,'processing',$5) ON CONFLICT DO NOTHING`,
		j.user, j.key, digest, chartID, j.meta.Title)
	if err != nil {
		internal(w, err)
		return false
	}
	if tag.RowsAffected() == 0 {
		var recorded, status, tjaSHA, audioSHA string
		var recordedChart *string
		if err = tx.QueryRow(ctx, `SELECT payload_digest,status,chart_id,tja_sha256,audio_sha256 FROM upload_requests WHERE user_id=$1 AND idempotency_key=$2`, j.user, j.key).Scan(&recorded, &status, &recordedChart, &tjaSHA, &audioSHA); err != nil {
			internal(w, err)
			return false
		}
		if recorded != digest || (existing != nil && (recordedChart == nil || *recordedChart != existing.ID)) {
			problem(w, 409, "IDEMPOTENCY_CONFLICT", "此上传请求标识已用于其他内容，请刷新页面后重试")
			return false
		}
		if status == "processing" {
			respond(w, 202, map[string]string{"status": "processing"})
			return false
		}
		if recordedChart == nil {
			problem(w, 409, "CHART_REMOVED", "该次上传的谱面已删除，请重新选择文件")
			return false
		}
		c, err := s.chart(ctx, *recordedChart)
		if errors.Is(err, pgx.ErrNoRows) {
			problem(w, 409, "CHART_REMOVED", "该次上传的谱面已删除，请重新选择文件")
			return false
		}
		if err != nil {
			internal(w, err)
			return false
		}
		if c.TJAHash != tjaSHA || c.AudioHash != audioSHA {
			problem(w, 409, "CHART_FILES_CHANGED", "该次上传已被后续更新替换，请重新打开谱面页面")
			return false
		}
		respond(w, 200, c)
		return false
	}
	if err = tx.Commit(ctx); err != nil {
		internal(w, err)
		return false
	}
	s.enqueue(j)
	if j.async {
		respond(w, 202, map[string]string{"status": "processing"})
		return true
	}
	result := <-j.done
	if result.err != nil {
		if result.err.cause != nil {
			internal(w, result.err.cause)
		} else {
			problem(w, result.err.status, result.err.code, result.err.message)
		}
		return true
	}
	status := 201
	if existing != nil {
		status = 200
	}
	respond(w, status, result.chart)
	return true
}
