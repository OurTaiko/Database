package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/sitekit/tja"
	"ourtaiko.dev/sitekit/video"
)

// replaceMovie sets a chart's background movie apart from its other files, as replaceCover
// sets its cover (as on Fanmade). The multipart body holds one "movie" file and optionally
// "movieOffset" (seconds; the current offset when left out). The movie is transcoded by the
// upload worker; with Prefer: respond-async the answer is 202 and the result arrives as an
// upload_result notification. A song registered without files has no TJA to name it.
func (s *Server) replaceMovie(w http.ResponseWriter, r *http.Request) {
	u, c, ok := s.managed(w, r)
	if !ok {
		return
	}
	if !c.HasFiles {
		problem(w, 422, "METADATA_LOCKED", "没有谱面文件的乐曲不能设置背景视频")
		return
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		problem(w, 503, "UPLOAD_BUSY", "当前正在处理其他上传，请稍后重试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, video.MaxBytes+64*1024)
	mr, err := r.MultipartReader()
	if err != nil {
		problem(w, 400, "UPLOAD_FILES_INVALID", "请选择一个 MP4、WebM 或 MOV 视频")
		return
	}
	dir, err := os.MkdirTemp(s.Config.Storage, "staging-movie-")
	if err != nil {
		internal(w, err)
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			os.RemoveAll(dir)
		}
	}()
	var movie *stagedFile
	offset, offsetSent := c.MovieOffset, false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			uploadReadError(w, err)
			return
		}
		disposition, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		field, name := params["name"], params["filename"]
		switch {
		case err != nil || disposition != "form-data":
			problem(w, 400, "UPLOAD_FILES_INVALID", "上传字段格式错误")
			return
		case field == "movie":
			if movie != nil || !tja.SafeFilename(name) || !video.ValidExtension(name) {
				problem(w, 400, "UPLOAD_FILES_INVALID", "只能上传一个 MP4、WebM 或 MOV 视频，文件名不能包含路径")
				return
			}
			movie = &stagedFile{name: name, path: filepath.Join(dir, "movie")}
			out, err := os.OpenFile(movie.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				internal(w, err)
				return
			}
			h := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(part, video.MaxBytes+1))
			closeErr := out.Close()
			if copyErr != nil {
				uploadReadError(w, copyErr)
				return
			}
			if closeErr != nil {
				internal(w, closeErr)
				return
			}
			if n == 0 || n > video.MaxBytes {
				problem(w, 413, "FILE_TOO_LARGE", "视频为空或超过 64 MiB")
				return
			}
			movie.size, movie.sha = n, hex.EncodeToString(h.Sum(nil))
		case field == "movieOffset" && name == "" && !offsetSent:
			data, err := io.ReadAll(io.LimitReader(part, 65))
			if err != nil {
				uploadReadError(w, err)
				return
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err != nil || !video.ValidOffset(n) {
				problem(w, 422, "TJA_MOVIE_OFFSET_INVALID", "MOVIEOFFSET 必须为秒数，绝对值不超过 1200")
				return
			}
			offset, offsetSent = n, true
		default:
			problem(w, 400, "UPLOAD_FILES_INVALID", "包含不支持或重复的上传字段")
			return
		}
		part.Close()
	}
	if movie == nil {
		problem(w, 400, "UPLOAD_FILES_INVALID", "请选择一个 MP4、WebM 或 MOV 视频")
		return
	}
	select {
	case s.pending <- struct{}{}:
	default:
		problem(w, 503, "UPLOAD_BUSY", "当前排队处理的上传较多，请稍后重试")
		return
	}
	job := &uploadJob{
		user: u.User.ID, key: ID(), dir: dir, existingID: c.ID, movie: movie, movieOnly: true,
		meta:  tja.Metadata{Title: c.Title, MovieOffset: offset},
		async: strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "respond-async"), done: make(chan uploadResult, 1),
	}
	accepted = true
	s.enqueue(job)
	if job.async {
		respond(w, 202, map[string]string{"status": "processing"})
		return
	}
	result := <-job.done
	if result.err != nil {
		if result.err.cause != nil {
			internal(w, result.err.cause)
		} else {
			problem(w, result.err.status, result.err.code, result.err.message)
		}
		return
	}
	respond(w, 200, result.chart)
}

// processMovie transcodes a job's movie and makes it the chart's, unlike an upload rejecting
// the job when the movie cannot be used.
func (s *Server) processMovie(ctx context.Context, j *uploadJob) uploadResult {
	moviePath := filepath.Join(j.dir, "movie."+video.Extension)
	limited, cancel := context.WithTimeout(ctx, movieTimeout)
	err := video.Transcode(limited, j.movie.path, moviePath)
	cancel()
	switch {
	case errors.Is(err, video.ErrInvalid):
		return rejected(422, "MOVIE_INVALID", "视频无法解码，请使用有效的 MP4、WebM 或 MOV（最长 20 分钟）")
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		return rejected(422, "MOVIE_TOO_SLOW", "视频转码超时，请缩短视频或降低分辨率后重试")
	case err != nil:
		return failed(err)
	}
	movieSHA, movieSize, err := fileDigest(moviePath)
	if err != nil {
		return failed(err)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return failed(err)
	}
	defer tx.Rollback(context.Background())
	// Serialize with replacements, deletion and metadata edits.
	var previousMovie *int64
	err = tx.QueryRow(ctx, `SELECT c.movie_file FROM charts c WHERE c.id=$1 AND c.tja_file IS NOT NULL AND `+publishedChart+` FOR UPDATE`, j.existingID).Scan(&previousMovie)
	if errors.Is(err, pgx.ErrNoRows) {
		return rejected(404, "CHART_NOT_FOUND", "谱面不存在或已删除")
	}
	if err != nil {
		return failed(err)
	}
	movieFile, err := s.storeFile(ctx, tx, moviePath, "movie."+video.Extension, video.Extension, video.MediaType, movieSHA, movieSize)
	if err != nil {
		return failed(err)
	}
	previousTJA, err := s.rewriteMovie(ctx, tx, j.existingID, movieSHA, &movieFile, j.meta.MovieOffset)
	if err != nil {
		return failed(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return failed(err)
	}
	retired := previousTJA
	if previousMovie != nil && *previousMovie != movieFile {
		retired = append(retired, *previousMovie)
	}
	if err = s.releaseFiles(ctx, retired); err != nil {
		log.Printf("replaced movie cleanup pending: %v", err)
	}
	chart, err := s.chart(ctx, j.existingID)
	if err != nil {
		return failed(err)
	}
	return uploadResult{chart: chart}
}

// rewriteMovie stores the chart's TJA with BGMOVIE naming the movie movieSHA and MOVIEOFFSET
// set to offset, then points the chart at that TJA, the movie (movieFile, or the current one
// when nil) and the offset. Players read both headers from the TJA; its chart blocks, and so
// the scores, are kept. tx must hold the chart's row lock. It returns the replaced TJA file,
// for the caller to release once tx commits.
func (s *Server) rewriteMovie(ctx context.Context, tx pgx.Tx, chartID, movieSHA string, movieFile *int64, offset float64) ([]int64, error) {
	var previous int64
	var key, name string
	if err := tx.QueryRow(ctx, `SELECT r.id,r.storage_key,r.original_filename FROM charts c JOIN chart_resources r ON r.id=c.tja_file WHERE c.id=$1`, chartID).Scan(&previous, &key, &name); err != nil {
		return nil, err
	}
	f, err := s.Config.Objects.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, tja.MaxTJA+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	// Only the two headers change, so a file stored under older rules is not judged again.
	stored, ok := tja.SetMovieHeaders(data, movieSHA+"."+video.Extension, offset)
	if !ok || len(stored) > tja.MaxTJA || tja.MovieName(stored) != movieSHA+"."+video.Extension {
		return nil, fmt.Errorf("stored TJA of %s cannot take movie headers", chartID)
	}
	staged, err := os.CreateTemp(s.Config.Storage, "staging-tja-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged.Name())
	_, err = staged.Write(stored)
	if err = errors.Join(err, staged.Close()); err != nil {
		return nil, err
	}
	tjaFile, err := s.storeFile(ctx, tx, staged.Name(), name, "tja", "application/octet-stream", hash(string(stored)), int64(len(stored)))
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE charts SET tja_file=$2,movie_file=COALESCE($3,movie_file),movie_offset=$4 WHERE id=$1`, chartID, tjaFile, movieFile, offset); err != nil {
		return nil, err
	}
	if tjaFile == previous {
		return nil, nil
	}
	return []int64{previous}, nil
}
