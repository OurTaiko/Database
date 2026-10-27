package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/audio"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/tja"
)

// Text fields a replacement may carry; omitted ones keep the chart's values.
var replacementFields = append(append([]string{}, uploadFields...), "confirmReset")

// replaceFiles replaces a chart's TJA and optionally its audio and cover,
// keeping its ID, uploader and creation time: the contract of Fanmade's
// PUT /charts/{id}/files. The audio is kept when none is sent. Scores are
// judged block by block (SetDifficulties): a block the new TJA no longer has
// goes with its scores; a remaining block whose TJAParser content hash changed
// keeps its scores but they stop being the latest (trigger
// chart_difficulties_retire_scores), and an unchanged block is left alone, so
// header-only edits such as titles touch no score. A new audio retires the
// scores of every block (trigger charts_retire_scores).
// A song registered without files (R038) gets its first TJA and audio here;
// both are then required, and there is nothing to confirm.
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
	defer os.RemoveAll(dir)
	files, fields, ok := receiveUploadForm(w, r, dir, replacementFields)
	if !ok {
		return
	}
	tf, af, cf := files["tja"], files["audio"], files["cover"]
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
	invalid := func(issue *tja.Issue) {
		respond(w, 422, map[string]any{"code": issue.Code, "message": issue.Message, "errors": []*tja.Issue{issue}, "requestId": w.Header().Get("X-Request-ID"), "validationVersion": tja.Version})
	}
	raw, e := os.ReadFile(tf.path)
	if e != nil {
		internal(w, e)
		return
	}
	encoding := fields["encoding"]
	if encoding == "" {
		encoding = tja.DetectEncoding(raw)
	}
	data, issue := tja.NormalizeUTF8(raw, encoding)
	if issue != nil {
		invalid(issue)
		return
	}
	// A kept audio is already identified by the stored file, so the TJA's
	// WAVE need not name it; a new audio must be the one the TJA names.
	waveCheck := ""
	if af != nil {
		waveCheck = af.name
	}
	meta, issue := tja.Parse(data, waveCheck)
	if issue != nil {
		invalid(issue)
		return
	}
	duration, wave := c.Duration, c.Wave
	var audioExtension string
	if af != nil {
		if duration, e = audio.Validate(r.Context(), af.path, af.name); e != nil {
			log.Printf("request=%s audio rejected: %v", w.Header().Get("X-Request-ID"), e)
			problem(w, 422, "AUDIO_INVALID", "音频无法解码，或不是单音轨的 OGG Vorbis / MP3，或时长超过 20 分钟")
			return
		}
		audioExtension = fileExtension(af.name)
		wave = af.sha + "." + audioExtension
	}
	var coverData []byte
	if cf != nil {
		source, e := os.ReadFile(cf.path)
		if e != nil {
			internal(w, e)
			return
		}
		if coverData, ok = encodeCover(w, r, cf.name, source); !ok {
			return
		}
	}
	// R007: the stored TJA names its audio by content hash.
	meta.Wave = wave
	data, rewritten := tja.SetWave(data, wave)
	if !rewritten {
		invalid(&tja.Issue{Code: "TJA_WAVE_MISSING", Message: "TJA 缺少 WAVE 音频引用"})
		return
	}
	tf.path = filepath.Join(dir, "tja.normalized")
	if e = os.WriteFile(tf.path, data, 0600); e != nil {
		internal(w, e)
		return
	}
	tf.sha, tf.size = hash(string(data)), int64(len(data))
	demoStart := meta.DemoStart
	if demoStart < 0 || demoStart >= duration {
		demoStart = 0
	}
	audioSHA, coverSHA := c.AudioHash, ""
	if af != nil {
		audioSHA = af.sha
	}
	if coverData != nil {
		coverSHA = hash(string(coverData))
	}
	// Hashes only what was sent, so a retry is answered before the files are
	// judged again, and the receipt's file hashes tell whether a later
	// replacement has superseded it.
	payload := hash(strings.Join([]string{"replace", r.PathValue("id"), tf.sha, audioSHA, coverSHA, fields["description"], fields["categoryIds"], fields["nijiiroId"], fields["officialId"]}, "\n"))

	ctx := context.WithoutCancel(r.Context())
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `INSERT INTO upload_requests(user_id,idempotency_key,payload_digest,chart_id,tja_sha256,audio_sha256) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, u.User.ID, key, payload, c.ID, tf.sha, audioSHA)
	if e != nil {
		internal(w, e)
		return
	}
	if tag.RowsAffected() == 0 {
		var recorded, recordedTJA, recordedAudio string
		var recordedChart *int64
		if e = tx.QueryRow(ctx, `SELECT payload_digest,chart_id,tja_sha256,audio_sha256 FROM upload_requests WHERE user_id=$1 AND idempotency_key=$2`, u.User.ID, key).Scan(&recorded, &recordedChart, &recordedTJA, &recordedAudio); e != nil {
			internal(w, e)
			return
		}
		if recorded != payload || recordedChart == nil || *recordedChart != c.ID {
			problem(w, 409, "IDEMPOTENCY_KEY_REUSED", "此上传请求标识已用于其他内容，请刷新页面后重试")
			return
		}
		current, e := s.chart(ctx, c.ID)
		if e != nil {
			internal(w, e)
			return
		}
		if current.TJAHash != recordedTJA || current.AudioHash != recordedAudio {
			problem(w, 409, "CHART_FILES_CHANGED", "该次替换已被后续更新替换，请重新打开谱面页面")
			return
		}
		respond(w, 200, current)
		return
	}
	// Serialize with other replacements, deletion and score submissions.
	var previousTJA, previousAudio, previousCover *int64
	e = tx.QueryRow(ctx, `SELECT tja_file,audio_file,cover_file FROM charts c WHERE c.id=$1 AND `+publishedChart+` FOR UPDATE`, c.ID).Scan(&previousTJA, &previousAudio, &previousCover)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	tjaFile, e := s.storeFile(ctx, tx, tf.path, tf.name, "tja", "application/octet-stream", tf.sha, tf.size)
	if e != nil {
		internal(w, e)
		return
	}
	var audioFile int64
	if af != nil {
		if audioFile, e = s.storeFile(ctx, tx, af.path, af.name, audioExtension, audio.MediaType(af.name), af.sha, af.size); e != nil {
			internal(w, e)
			return
		}
	} else if previousAudio != nil {
		audioFile = *previousAudio
	} else {
		// The chart lost its files between the read and the lock.
		problem(w, 409, "CHART_FILES_CHANGED", "该乐曲的文件已变化，请重新打开谱面页面")
		return
	}
	coverFile := previousCover
	if coverData != nil {
		id, e := s.storeImage(ctx, tx, coverData)
		if e != nil {
			internal(w, e)
			return
		}
		coverFile = &id
	}
	titles, _ := json.Marshal(meta.TitleTranslations)
	subtitles, _ := json.Marshal(meta.SubtitleTranslations)
	// Names and their translations come from the new TJA again, as on Fanmade.
	if _, e = tx.Exec(ctx, `UPDATE charts SET tja_file=$2,audio_file=$3,cover_file=$4,description=$5,category_flags=$6,nijiiro_id=$7,official_id=$8,
 title=$9,subtitle=$10,bpm=$11,offset_seconds=$12,demo_start=$13,demo_end=$14,duration=$15,title_translations=$16,subtitle_translations=$17 WHERE id=$1`,
		c.ID, tjaFile, audioFile, coverFile, description, flags, nijiiroID, officialID,
		meta.Title, meta.Subtitle, meta.BPM, meta.Offset, demoStart, demoStart+15, duration, titles, subtitles); e != nil {
		internal(w, e)
		return
	}
	if e = database.SetDifficulties(ctx, tx, c.ID, meta.Difficulties); e != nil {
		internal(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		internal(w, e)
		return
	}
	// Shared files stay while another chart or playlist refers to them.
	var retired []int64
	if previousTJA != nil && tjaFile != *previousTJA {
		retired = append(retired, *previousTJA)
	}
	if previousAudio != nil && audioFile != *previousAudio {
		retired = append(retired, *previousAudio)
	}
	if previousCover != nil && (coverFile == nil || *coverFile != *previousCover) {
		retired = append(retired, *previousCover)
	}
	if e = s.releaseFiles(ctx, retired); e != nil {
		log.Printf("replaced chart file cleanup pending: %v", e)
	}
	updated, e := s.chart(ctx, c.ID)
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, updated)
}
