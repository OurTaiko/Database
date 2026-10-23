package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/audio"
	"ourtaiko.dev/database/api/internal/cover"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/tja"
)

type stagedFile struct {
	name, path, sha string
	size            int64
}

var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)
var officialIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func uploadReadError(w http.ResponseWriter, e error) {
	var tooLarge *http.MaxBytesError
	if errors.As(e, &tooLarge) {
		problem(w, 413, "FILE_TOO_LARGE", "文件超过上传大小限制")
		return
	}
	problem(w, 400, "UPLOAD_FILES_INVALID", "上传内容不完整，请重试")
}

// uploader requires a signed-in user holding the upload permission bit.
func (s *Server) uploader(w http.ResponseWriter, r *http.Request) (session, bool) {
	u, ok := s.required(w, r, true)
	if !ok {
		return u, false
	}
	if !u.User.CanUpload {
		problem(w, 403, "UPLOAD_FORBIDDEN", "此账号没有上传权限")
		return u, false
	}
	return u, true
}

// identifiers reads the optional Nijiiro and official IDs; empty means none.
func identifiers(nijiiro, official string) (*int32, *string, bool) {
	var nijiiroID *int32
	var officialID *string
	if nijiiro = strings.TrimSpace(nijiiro); nijiiro != "" {
		n, e := strconv.ParseInt(nijiiro, 10, 32)
		if e != nil || n < 1 {
			return nil, nil, false
		}
		id := int32(n)
		nijiiroID = &id
	}
	if official = strings.TrimSpace(official); official != "" {
		if !officialIDPattern.MatchString(official) {
			return nil, nil, false
		}
		officialID = &official
	}
	return nijiiroID, officialID, true
}

// Text fields a chart submission may carry besides its files. title,
// subtitle and bpm describe a song registered without files (R038); with a
// TJA they are ignored, as the file is the source of those.
var uploadFields = []string{"encoding", "description", "categoryIds", "nijiiroId", "officialId", "title", "subtitle", "bpm"}

// receiveUploadForm stages the multipart files tja, audio and cover of a
// submission in dir and collects the text fields named in allowed. On a bad
// request it has answered and returns false.
func receiveUploadForm(w http.ResponseWriter, r *http.Request, dir string, allowed []string) (map[string]*stagedFile, map[string]string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 112*1024*1024)
	mr, e := r.MultipartReader()
	if e != nil {
		problem(w, 400, "UPLOAD_FILES_INVALID", "请选择 TJA 和 OGG 或 MP3 音频文件")
		return nil, nil, false
	}
	files := map[string]*stagedFile{}
	fields := map[string]string{}
	for {
		part, e := mr.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			uploadReadError(w, e)
			return nil, nil, false
		}
		disposition, params, e := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if e != nil || disposition != "form-data" {
			problem(w, 400, "UPLOAD_FILES_INVALID", "上传字段格式错误")
			return nil, nil, false
		}
		field, name := params["name"], params["filename"]
		switch {
		case field == "tja" || field == "audio" || field == "cover":
			validExtension := strings.EqualFold(filepath.Ext(name), ".tja")
			maxBytes := int64(tja.MaxTJA)
			if field == "audio" {
				validExtension = audio.MediaType(name) != ""
				maxBytes = tja.MaxAudio
			}
			if field == "cover" {
				validExtension = cover.ValidExtension(name)
				maxBytes = cover.MaxBytes
			}
			if files[field] != nil || !tja.SafeFilename(name) || !validExtension {
				problem(w, 400, "UPLOAD_FILES_INVALID", "文件类型、数量或文件名无效；封面仅支持 JPG / PNG / WebP，文件名不能包含路径")
				return nil, nil, false
			}
			f := &stagedFile{name: name, path: filepath.Join(dir, field)}
			out, e := os.OpenFile(f.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				internal(w, e)
				return nil, nil, false
			}
			h := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(part, maxBytes+1))
			closeErr := out.Close()
			if copyErr != nil {
				uploadReadError(w, copyErr)
				return nil, nil, false
			}
			if closeErr != nil {
				internal(w, closeErr)
				return nil, nil, false
			}
			if n == 0 || n > maxBytes {
				problem(w, 413, "FILE_TOO_LARGE", "文件为空或超过上传大小限制")
				return nil, nil, false
			}
			f.size, f.sha = n, hex.EncodeToString(h.Sum(nil))
			files[field] = f
		case slices.Contains(allowed, field):
			if _, exists := fields[field]; exists || name != "" {
				problem(w, 400, "UPLOAD_FILES_INVALID", "上传字段重复或格式错误")
				return nil, nil, false
			}
			data, e := io.ReadAll(io.LimitReader(part, 4001))
			if e != nil {
				uploadReadError(w, e)
				return nil, nil, false
			}
			if len(data) > 4000 {
				problem(w, 400, "DESCRIPTION_TOO_LONG", "说明不能超过 4000 字节")
				return nil, nil, false
			}
			fields[field] = string(data)
		default:
			problem(w, 400, "UPLOAD_FILES_INVALID", "包含不支持的上传字段")
			return nil, nil, false
		}
		part.Close()
	}
	return files, fields, true
}

// upload publishes a new chart from a TJA, its audio and an optional cover.
// Everything is validated before any object is written. A form with neither
// a TJA nor an audio registers a song without files (createSong).
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	u, ok := s.uploader(w, r)
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
	files, fields, ok := receiveUploadForm(w, r, dir, uploadFields)
	if !ok {
		return
	}
	categoryIDs, e := categorySelection(json.RawMessage(fields["categoryIds"]))
	if e != nil {
		problem(w, 422, "CATEGORIES_INVALID", "分类必须为已有分类 ID 的数组")
		return
	}
	flags, _ := encodeCategories(categoryIDs)
	nijiiroID, officialID, ok := identifiers(fields["nijiiroId"], fields["officialId"])
	if !ok {
		problem(w, 422, "IDENTIFIER_INVALID", "Nijiiro ID 必须为正整数；官方 ID 只能包含字母、数字、点、下划线和连字符，最长 64 个字符")
		return
	}
	description := fields["description"]
	if !validDescription(description) {
		problem(w, 422, "DESCRIPTION_INVALID", "说明包含无效字符")
		return
	}
	tf, af, cf := files["tja"], files["audio"], files["cover"]
	if tf == nil && af == nil {
		s.createSong(w, r, u, key, fields, cf, categoryIDs, flags, nijiiroID, officialID)
		return
	}
	if tf == nil || af == nil {
		problem(w, 400, "UPLOAD_FILES_INVALID", "必须同时选择 TJA 和一个 OGG 或 MP3 音频，或两者都不选以仅登记乐曲")
		return
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
	meta, issue := tja.Parse(data, af.name)
	if issue != nil {
		invalid(issue)
		return
	}
	duration, e := audio.Validate(r.Context(), af.path, af.name)
	if e != nil {
		log.Printf("request=%s audio rejected: %v", w.Header().Get("X-Request-ID"), e)
		problem(w, 422, "AUDIO_INVALID", "音频无法解码，或不是单音轨的 OGG Vorbis / MP3，或时长超过 20 分钟")
		return
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
	// R007: the stored TJA names its audio by content hash; hash the TJA after
	// that rewrite, never before.
	extension := fileExtension(af.name)
	meta.Wave = af.sha + "." + extension
	data, rewritten := tja.SetWave(data, meta.Wave)
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
	coverSHA := ""
	if coverData != nil {
		coverSHA = hash(string(coverData))
	}
	payload := hash(strings.Join([]string{tf.sha, af.sha, coverSHA, description, strings.Join(categoryIDs, ","), fields["nijiiroId"], fields["officialId"]}, "\n"))

	ctx := context.WithoutCancel(r.Context())
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// A retried request returns the chart it already created.
	tag, e := tx.Exec(ctx, `INSERT INTO upload_requests(user_id,idempotency_key,payload_digest,tja_sha256,audio_sha256) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, u.User.ID, key, payload, tf.sha, af.sha)
	if e != nil {
		internal(w, e)
		return
	}
	if tag.RowsAffected() == 0 {
		var recorded string
		var existing *int64
		if e = tx.QueryRow(ctx, `SELECT payload_digest,chart_id FROM upload_requests WHERE user_id=$1 AND idempotency_key=$2`, u.User.ID, key).Scan(&recorded, &existing); e != nil {
			internal(w, e)
			return
		}
		if recorded != payload || existing == nil {
			problem(w, 409, "IDEMPOTENCY_KEY_REUSED", "此上传请求标识已用于其他内容，请刷新页面后重试")
			return
		}
		c, e := s.chart(ctx, *existing)
		if e != nil {
			internal(w, e)
			return
		}
		respond(w, 200, c)
		return
	}
	tjaFile, e := s.storeFile(ctx, tx, tf.path, tf.name, "tja", "application/octet-stream", tf.sha, tf.size)
	if e != nil {
		internal(w, e)
		return
	}
	audioFile, e := s.storeFile(ctx, tx, af.path, af.name, extension, audio.MediaType(af.name), af.sha, af.size)
	if e != nil {
		internal(w, e)
		return
	}
	var coverFile *int64
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
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO charts(nijiiro_id,official_id,uploader_id,tja_file,audio_file,cover_file,description,title,subtitle,bpm,offset_seconds,demo_start,duration,title_translations,subtitle_translations,category_flags,demo_end)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
		nijiiroID, officialID, u.User.ID, tjaFile, audioFile, coverFile, description, meta.Title, meta.Subtitle, meta.BPM, meta.Offset, demoStart, duration, titles, subtitles, flags, demoStart+15).Scan(&id)
	if e == nil {
		e = database.SetDifficulties(ctx, tx, id, meta.Difficulties)
	}
	if e != nil {
		internal(w, e)
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE upload_requests SET chart_id=$3 WHERE user_id=$1 AND idempotency_key=$2`, u.User.ID, key, id); e != nil {
		internal(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		internal(w, e)
		return
	}
	c, e := s.chart(ctx, id)
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 201, c)
}

func validDescription(s string) bool {
	if len(s) > 4000 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func encodeCover(w http.ResponseWriter, r *http.Request, name string, data []byte) ([]byte, bool) {
	encoded, err := cover.Encode(r.Context(), name, data)
	if errors.Is(err, cover.ErrInvalid) {
		problem(w, 422, "COVER_INVALID", "封面必须是有效的 JPG、PNG 或 WebP，最大 8 MiB、1600 万像素，单边不超过 8192 像素")
		return nil, false
	}
	if err != nil {
		internal(w, err)
		return nil, false
	}
	return encoded, true
}

// storeImage stores an encoded WebP cover and returns its file ID.
func (s *Server) storeImage(ctx context.Context, tx pgx.Tx, data []byte) (int64, error) {
	return s.storeImageAs(ctx, tx, data, "webp", "image/webp")
}

// storeImageAs stores image bytes as cover.{extension} and returns the file ID.
func (s *Server) storeImageAs(ctx context.Context, tx pgx.Tx, data []byte, extension, media string) (int64, error) {
	tmp, e := os.CreateTemp(s.Config.Storage, "cover-*."+extension)
	if e != nil {
		return 0, e
	}
	defer os.Remove(tmp.Name())
	_, e = tmp.Write(data)
	if e = errors.Join(e, tmp.Close()); e != nil {
		return 0, e
	}
	return s.storeFile(ctx, tx, tmp.Name(), "cover."+extension, extension, media, hash(string(data)), int64(len(data)))
}

// managed loads a published chart the signed-in user may edit.
func (s *Server) managed(w http.ResponseWriter, r *http.Request) (session, Chart, bool) {
	u, ok := s.required(w, r, true)
	if !ok {
		return u, Chart{}, false
	}
	id, ok := chartID(w, r)
	if !ok {
		return u, Chart{}, false
	}
	c, e := s.chart(r.Context(), id)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return u, c, false
	}
	if e != nil {
		internal(w, e)
		return u, c, false
	}
	if !canManage(u.User, c) {
		problem(w, 403, "FORBIDDEN", "只有上传者或管理员可以修改谱面")
		return u, c, false
	}
	return u, c, true
}

// receiveCover reads the single multipart file field "cover" and encodes it
// as the stored WebP. It reports false after writing the error response.
func receiveCover(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, cover.MaxBytes+64*1024)
	mr, err := r.MultipartReader()
	if err != nil {
		problem(w, 400, "COVER_INVALID", "请选择封面文件")
		return nil, false
	}
	part, err := mr.NextPart()
	if err != nil {
		uploadReadError(w, err)
		return nil, false
	}
	disposition, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	name := params["filename"]
	if err != nil || disposition != "form-data" || params["name"] != "cover" || !tja.SafeFilename(name) || !cover.ValidExtension(name) {
		problem(w, 400, "COVER_INVALID", "只能上传一个 JPG、PNG 或 WebP 封面")
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(part, cover.MaxBytes+1))
	if err != nil {
		uploadReadError(w, err)
		return nil, false
	}
	if len(data) > cover.MaxBytes {
		problem(w, 413, "FILE_TOO_LARGE", "封面不能超过 8 MiB")
		return nil, false
	}
	encoded, ok := encodeCover(w, r, name, data)
	if !ok {
		return nil, false
	}
	return encoded, true
}

func (s *Server) replaceCover(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.managed(w, r)
	if !ok {
		return
	}
	encoded, ok := receiveCover(w, r)
	if !ok {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var previous *int64
	err = tx.QueryRow(ctx, `SELECT cover_file FROM charts c WHERE c.id=$1 AND `+publishedChart+` FOR UPDATE`, c.ID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	file, err := s.storeImage(ctx, tx, encoded)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE charts SET cover_file=$2 WHERE id=$1`, c.ID, file)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		internal(w, err)
		return
	}
	if previous != nil && *previous != file {
		if err = s.releaseFiles(ctx, []int64{*previous}); err != nil {
			log.Printf("replaced cover cleanup pending: %v", err)
		}
	}
	respond(w, 200, map[string]string{"coverHash": hash(string(encoded))})
}

type metadataPatch struct {
	// Title, Subtitle and BPM are the TJA's for a chart with files and can be
	// changed only on a song registered without them (R038); null BPM means
	// unknown.
	Title       json.RawMessage `json:"title"`
	Subtitle    json.RawMessage `json:"subtitle"`
	BPM         json.RawMessage `json:"bpm"`
	Description json.RawMessage `json:"description"`
	CategoryIDs json.RawMessage `json:"categoryIds"`
	// null clears an identifier.
	NijiiroID            json.RawMessage `json:"nijiiroId"`
	OfficialID           json.RawMessage `json:"officialId"`
	TitleTranslations    json.RawMessage `json:"titleTranslations"`
	SubtitleTranslations json.RawMessage `json:"subtitleTranslations"`
}

func validDisplayText(s string) bool {
	if len(s) > 500 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// patchTranslations merges locale → text into dst; null or "" removes a
// locale. The TJA's own title and subtitle are not editable here.
func patchTranslations(raw json.RawMessage, dst map[string]string) bool {
	if len(raw) == 0 {
		return true
	}
	var patch map[string]*string
	if json.Unmarshal(raw, &patch) != nil {
		return false
	}
	for locale, value := range patch {
		if locale != "en" && locale != "ja" && locale != "zh" && locale != "ko" {
			return false
		}
		if value == nil || strings.TrimSpace(*value) == "" {
			delete(dst, locale)
			continue
		}
		if !validDisplayText(*value) {
			return false
		}
		dst[locale] = strings.TrimSpace(*value)
	}
	return true
}

// editMetadata changes the database's own description of a chart; the stored
// TJA and audio are untouched. Omitted fields keep their value. The title and
// subtitle can be changed at any time (R042): pages, packages and the game use
// these, and put them into the TJA headers they hand out. The BPM is the TJA's
// and can be set only on a song registered without files.
func (s *Server) editMetadata(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.managed(w, r)
	if !ok {
		return
	}
	var patch metadataPatch
	if !decode(w, r, &patch) {
		return
	}
	isNull := func(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
	title, subtitle, bpm := c.Title, c.Subtitle, c.BPM
	if len(patch.BPM) > 0 && c.HasFiles {
		problem(w, 422, "METADATA_LOCKED", "有谱面文件时，BPM 来自 TJA，不能在此修改")
		return
	}
	if len(patch.Title) > 0 || len(patch.Subtitle) > 0 || len(patch.BPM) > 0 {
		if len(patch.Title) > 0 {
			if json.Unmarshal(patch.Title, &title) != nil || isNull(patch.Title) {
				problem(w, 422, "METADATA_INVALID", "曲名必须为文本")
				return
			}
			title = strings.TrimSpace(title)
		}
		if len(patch.Subtitle) > 0 {
			if json.Unmarshal(patch.Subtitle, &subtitle) != nil || isNull(patch.Subtitle) {
				problem(w, 422, "METADATA_INVALID", "副标题必须为文本")
				return
			}
			subtitle = strings.TrimSpace(subtitle)
		}
		if title == "" || !validDisplayText(title) || !validDisplayText(subtitle) {
			problem(w, 422, "METADATA_INVALID", "曲名不能为空；曲名和副标题不能超过 500 字节或包含控制字符")
			return
		}
		if len(patch.BPM) > 0 {
			bpm = 0
			if !isNull(patch.BPM) && (json.Unmarshal(patch.BPM, &bpm) != nil || bpm <= 0 || !validBPM(bpm)) {
				problem(w, 422, "METADATA_INVALID", "BPM 必须为正数或 null")
				return
			}
		}
	}
	description := c.Description
	if len(patch.Description) > 0 {
		if json.Unmarshal(patch.Description, &description) != nil || isNull(patch.Description) || !validDescription(description) {
			problem(w, 422, "DESCRIPTION_INVALID", "说明必须为不超过 4000 字节的文本")
			return
		}
	}
	flags, _ := encodeCategories(c.CategoryIDs)
	if len(patch.CategoryIDs) > 0 {
		ids, e := categorySelection(patch.CategoryIDs)
		if e != nil {
			problem(w, 422, "CATEGORIES_INVALID", "分类必须为已有分类 ID 的数组")
			return
		}
		flags, _ = encodeCategories(ids)
	}
	nijiiroID, officialID := c.NijiiroID, c.OfficialID
	if len(patch.NijiiroID) > 0 {
		nijiiroID = nil
		if !isNull(patch.NijiiroID) {
			var n int32
			if json.Unmarshal(patch.NijiiroID, &n) != nil || n < 1 {
				problem(w, 422, "IDENTIFIER_INVALID", "Nijiiro ID 必须为正整数或 null")
				return
			}
			nijiiroID = &n
		}
	}
	if len(patch.OfficialID) > 0 {
		officialID = nil
		if !isNull(patch.OfficialID) {
			var value string
			if json.Unmarshal(patch.OfficialID, &value) != nil || (value != "" && !officialIDPattern.MatchString(value)) {
				problem(w, 422, "IDENTIFIER_INVALID", "官方 ID 只能包含字母、数字、点、下划线和连字符，最长 64 个字符")
				return
			}
			if value != "" {
				officialID = &value
			}
		}
	}
	if !patchTranslations(patch.TitleTranslations, c.TitleTranslations) || !patchTranslations(patch.SubtitleTranslations, c.SubtitleTranslations) {
		problem(w, 422, "TRANSLATIONS_INVALID", "译名必须为 en / ja / zh / ko 到不超过 500 字节文本的映射")
		return
	}
	titles, _ := json.Marshal(c.TitleTranslations)
	subtitles, _ := json.Marshal(c.SubtitleTranslations)
	// update_at is refreshed by the charts trigger only when something changed.
	tag, e := s.DB.Exec(r.Context(), `UPDATE charts c SET description=$2,category_flags=$3,nijiiro_id=$4,official_id=$5,title_translations=$6,subtitle_translations=$7,title=$8,subtitle=$9,bpm=$10 WHERE c.id=$1 AND `+publishedChart,
		c.ID, description, flags, nijiiroID, officialID, titles, subtitles, title, subtitle, bpm)
	if e != nil {
		internal(w, e)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return
	}
	updated, e := s.chart(r.Context(), c.ID)
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, updated)
}
