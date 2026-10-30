package httpapi

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/tja"
	"ourtaiko.dev/sitekit/audio"
	"ourtaiko.dev/sitekit/objectstore"
	"ourtaiko.dev/sitekit/video"
)

// publishedFile resolves one file role of a published chart without the
// uploader lookup a full chart read makes.
func (s *Server) publishedFile(w http.ResponseWriter, r *http.Request, column string) (key, name, digest string, ok bool) {
	id, ok := chartID(w, r)
	if !ok {
		return
	}
	err := s.DB.QueryRow(r.Context(), `SELECT f.storage_key,f.original_filename,f.sha256 FROM charts c JOIN chart_resources f ON f.id=c.`+column+` WHERE c.id=$1 AND `+publishedChart, id).Scan(&key, &name, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "RESOURCE_NOT_FOUND", "谱面或资源不存在")
		return "", "", "", false
	}
	if err != nil {
		internal(w, err)
		return "", "", "", false
	}
	return key, name, digest, true
}

// serveObject sends a stored file, or redirects to its CDN address when S3
// has a public base URL. Objects are content-addressed, but the chart-ID
// address is revalidated so deletion and replacement take effect.
func (s *Server) serveObject(w http.ResponseWriter, r *http.Request, key, name, digest, contentType, disposition string) {
	if remote, ok := s.Config.Objects.(*objectstore.S3); ok {
		target, err := remote.PublicURL(key)
		if err != nil {
			internal(w, err)
			return
		}
		if target != "" {
			if disposition == "attachment" {
				u, _ := url.Parse(target)
				q := u.Query()
				q.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
				u.RawQuery = q.Encode()
				target = u.String()
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
	}
	f, err := s.Config.Objects.Open(r.Context(), key)
	if err != nil {
		internal(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("Cache-Control", "public, no-cache, no-transform")
	// Seekable file: GET/HEAD, ranges and conditional requests come from the standard library.
	http.ServeContent(w, r, name, time.Time{}, f)
}

func (s *Server) downloadTJA(w http.ResponseWriter, r *http.Request) {
	if key, name, digest, ok := s.publishedFile(w, r, "tja_file"); ok {
		s.serveObject(w, r, key, name, digest, "application/octet-stream", "attachment")
	}
}

func (s *Server) streamAudio(w http.ResponseWriter, r *http.Request) {
	if key, name, digest, ok := s.publishedFile(w, r, "audio_file"); ok {
		w.Header().Set("X-Accel-Buffering", "no")
		s.serveObject(w, r, key, name, digest, audio.MediaType(name), "inline")
	}
}

// streamMovie sends the chart's background movie (BGMOVIE), as Fanmade's /movie.
func (s *Server) streamMovie(w http.ResponseWriter, r *http.Request) {
	if key, _, digest, ok := s.publishedFile(w, r, "movie_file"); ok {
		s.serveObject(w, r, key, digest+"."+video.Extension, digest, video.MediaType, "inline")
	}
}

// streamPreview sends the game's song-select preview clip.
func (s *Server) streamPreview(w http.ResponseWriter, r *http.Request) {
	if key, _, digest, ok := s.publishedFile(w, r, "preview_file"); ok {
		s.serveObject(w, r, key, "preview.ogg", digest, "audio/ogg", "inline")
	}
}

func (s *Server) getCover(w http.ResponseWriter, r *http.Request) {
	key, _, digest, ok := s.publishedFile(w, r, "cover_file")
	if !ok {
		return
	}
	if requested := r.URL.Query().Get("v"); requested != "" && requested != digest {
		problem(w, 404, "COVER_NOT_FOUND", "该封面已被替换")
		return
	}
	s.serveObject(w, r, key, "cover.webp", digest, "image/webp", "inline")
}

// downloadPackage builds a ZIP per request from the stored TJA, with the
// database's title and subtitle in its headers, and the stored audio. The
// audio keeps its content-addressed name, which is what the stored TJA's WAVE
// declares (R007).
func (s *Server) downloadPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := chartID(w, r)
	if !ok {
		return
	}
	c, e := readChart(s.DB.QueryRow(r.Context(), chartSelect+` WHERE c.id=$1 AND `+publishedChart, id))
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	if !c.HasFiles {
		problem(w, 404, "CHART_FILES_MISSING", "该乐曲尚未上传谱面文件")
		return
	}
	tf, e := s.Config.Objects.Open(r.Context(), c.TJAKey)
	if e != nil {
		internal(w, e)
		return
	}
	stored, e := io.ReadAll(io.LimitReader(tf, tja.MaxTJA+1))
	tf.Close()
	if e != nil {
		internal(w, e)
		return
	}
	// The package names the chart as the database does (R042).
	chartText := tja.WithTitles(stored, c.Title, c.Subtitle, c.TitleTranslations, c.SubtitleTranslations)
	af, e := s.Config.Objects.Open(r.Context(), c.AudioKey)
	if e != nil {
		internal(w, e)
		return
	}
	defer af.Close()
	entries := []struct {
		name string
		file io.Reader
	}{{c.TJAName, bytes.NewReader(chartText)}, {c.Wave, af}}
	// The movie goes in under the name the stored TJA declares, as on Fanmade.
	if c.MovieKey != "" {
		mf, e := s.Config.Objects.Open(r.Context(), c.MovieKey)
		if e != nil {
			internal(w, e)
			return
		}
		defer mf.Close()
		entries = append(entries, struct {
			name string
			file io.Reader
		}{tja.MovieName(stored), mf})
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": strings.TrimSuffix(c.TJAName, filepath.Ext(c.TJAName)) + ".zip"}))
	z := zip.NewWriter(w)
	for _, f := range entries {
		dst, e := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store})
		if e != nil {
			return
		}
		if _, e = io.Copy(dst, f.file); e != nil {
			return
		}
	}
	z.Close()
}
