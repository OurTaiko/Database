package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"ourtaiko.dev/sitekit/audio"
)

// Text fields a replacement may carry; omitted ones keep the chart's values.
var replacementFields = append(append([]string{}, uploadFields...), "confirmReset", "keepTitles")

// previewRange is the preview range of an upload: the one sent, which must lie
// within the audio, or else the TJA's DEMOSTART (0 when outside the audio) for
// 15 seconds. It reports false for a range outside the audio.
func previewRange(fields map[string]string, fileStart, duration float64) (float64, float64, bool) {
	start := fileStart
	if start < 0 || start >= duration {
		start = 0
	}
	end := start + 15
	number := func(field string) float64 {
		n, err := strconv.ParseFloat(strings.TrimSpace(fields[field]), 64)
		if err != nil {
			return math.NaN()
		}
		return n
	}
	if _, sent := fields["demoStart"]; sent {
		start = number("demoStart")
		end = start + 15
	}
	if _, sent := fields["demoEnd"]; sent {
		end = number("demoEnd")
	}
	return start, end, audio.ValidPreviewRange(start, end, duration)
}

// replaceFiles replaces a chart's TJA and optionally its audio, cover and background movie,
// keeping its ID, uploader and creation time: the contract of Fanmade's PUT
// /charts/{id}/files, processed in the background the same way (upload_job.go). The audio
// is kept when none is sent, and the movie while the new TJA still declares one. Scores are
// judged block by block (SetDifficulties): a block the new TJA no longer has goes with its
// scores; a remaining block whose TJAParser content hash changed keeps its scores but they
// stop being the latest (trigger chart_difficulties_retire_scores), and an unchanged block
// is left alone, so header-only edits such as titles touch no score. A new audio retires
// the scores of every block (trigger charts_retire_scores).
// A song registered without files (R038) gets its first TJA and audio here; both are then
// required, and there is nothing to confirm.
func (s *Server) replaceFiles(w http.ResponseWriter, r *http.Request) {
	u, c, ok := s.managed(w, r)
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !keyPattern.MatchString(key) {
		problem(w, 400, "IDEMPOTENCY_KEY_INVALID", "需要有效的上传请求标识")
		return
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		problem(w, 503, "UPLOAD_BUSY", "当前正在处理其他上传，请稍后重试")
		return
	}
	dir, e := os.MkdirTemp(s.Config.Storage, "staging-")
	if e != nil {
		internal(w, e)
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			os.RemoveAll(dir)
		}
	}()
	files, fields, ok := receiveUploadForm(w, r, dir, replacementFields)
	if !ok {
		return
	}
	tf, af, cf, mv := files["tja"], files["audio"], files["cover"], files["movie"]
	if tf == nil || (!c.HasFiles && af == nil) {
		if c.HasFiles {
			problem(w, 400, "UPLOAD_FILES_INVALID", "必须选择新的 TJA，音频可沿用当前文件")
		} else {
			problem(w, 400, "UPLOAD_FILES_INVALID", "该乐曲尚无文件，必须同时选择 TJA 和一个 OGG 或 MP3 音频")
		}
		return
	}
	if c.HasFiles && fields["confirmReset"] != "true" {
		problem(w, 400, "REPLACEMENT_CONFIRMATION_REQUIRED", "替换需要确认：消失的谱面块连同成绩删除，其余旧成绩不再计入最新版本")
		return
	}
	description := c.Description
	if value, sent := fields["description"]; sent {
		if !validDescription(value) {
			problem(w, 422, "DESCRIPTION_INVALID", "说明包含无效字符")
			return
		}
		description = value
	}
	flags, _ := encodeCategories(c.CategoryIDs)
	if value, sent := fields["categoryIds"]; sent {
		ids, e := categorySelection(json.RawMessage(value))
		if e != nil {
			problem(w, 422, "CATEGORIES_INVALID", "分类必须为已有分类 ID 的数组")
			return
		}
		flags, _ = encodeCategories(ids)
	}
	nijiiroID, officialID := c.NijiiroID, c.OfficialID
	if _, sentNijiiro := fields["nijiiroId"]; sentNijiiro {
		if nijiiroID, _, ok = identifiers(fields["nijiiroId"], ""); !ok {
			problem(w, 422, "IDENTIFIER_INVALID", "Nijiiro ID 必须为正整数")
			return
		}
	}
	if _, sentOfficial := fields["officialId"]; sentOfficial {
		if _, officialID, ok = identifiers("", fields["officialId"]); !ok {
			problem(w, 422, "IDENTIFIER_INVALID", "官方 ID 只能包含字母、数字、点、下划线和连字符，最长 64 个字符")
			return
		}
	}
	// A kept audio is already identified by the stored file, so the TJA's WAVE need not name
	// it; a new audio must be the one the TJA names.
	waveCheck := ""
	if af != nil {
		waveCheck = af.name
	}
	data, meta, ok := readUploadTJA(w, tf, fields["encoding"], waveCheck)
	if !ok {
		return
	}
	keepMovie, ok := checkMovie(w, meta, mv, &c)
	if !ok {
		return
	}
	if !reservePending(w, s) {
		return
	}
	// Hashes only what was sent, so a retry is answered before the files are judged again.
	parts := []string{"replace", c.ID, tf.name, tf.sha, fields["description"], fields["categoryIds"], fields["nijiiroId"], fields["officialId"], fields["keepTitles"], fields["demoStart"], fields["demoEnd"]}
	for _, f := range []*stagedFile{af, cf, mv} {
		if f != nil {
			parts = append(parts, f.name, f.sha)
		}
	}
	job := &uploadJob{
		user: u.User.ID, key: key, dir: dir, existingID: c.ID, audioHash: c.AudioHash, movieHash: c.MovieHash,
		tja: tf, audio: af, cover: cf, movie: mv, normalized: data, meta: meta,
		description: description, flags: flags, nijiiroID: nijiiroID, officialID: officialID, fields: fields,
		keepMovie: keepMovie, keepTitles: fields["keepTitles"] == "true",
		async: strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "respond-async"), done: make(chan uploadResult, 1),
	}
	accepted = s.acceptUpload(w, r, job, hash(strings.Join(parts, "\n")), &c)
}
