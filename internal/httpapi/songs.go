package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// validBPM accepts the BPM an uploader gives a song without files: positive
// and finite, or 0 for unknown.
func validBPM(n float64) bool {
	return n >= 0 && n <= 10000 && !math.IsNaN(n)
}

// songBPM reads the optional BPM of a song without files; empty means
// unknown, stored as 0.
func songBPM(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	n, e := strconv.ParseFloat(raw, 64)
	if e != nil || n <= 0 || !validBPM(n) {
		return 0, false
	}
	return n, true
}

// createSong registers a chart that has no TJA and no audio yet (R038): a
// song that is known but whose files are not at hand. It keeps only what the
// uploader says of it: title, subtitle, an optional BPM, the identifiers,
// categories, notes and a cover. Blocks, duration and the files come later
// with PUT /charts/{id}/files. Called by upload once the form is read; the
// category and identifier fields are already validated.
func (s *Server) createSong(w http.ResponseWriter, r *http.Request, u session, key string, fields map[string]string, cf *stagedFile, categoryIDs []string, flags CategoryFlags, nijiiroID *int32, officialID *string) {
	title := strings.TrimSpace(fields["title"])
	subtitle := strings.TrimSpace(fields["subtitle"])
	if title == "" || !validDisplayText(title) || !validDisplayText(subtitle) {
		problem(w, 422, "METADATA_INVALID", "仅登记乐曲时必须填写曲名；曲名和副标题不能超过 500 字节或包含控制字符")
		return
	}
	bpm, ok := songBPM(fields["bpm"])
	if !ok {
		problem(w, 422, "METADATA_INVALID", "BPM 必须为正数，或留空表示未知")
		return
	}
	description := fields["description"]
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
	coverSHA := ""
	if coverData != nil {
		coverSHA = hash(string(coverData))
	}
	payload := hash(strings.Join([]string{"song", title, subtitle, fields["bpm"], coverSHA, description, strings.Join(categoryIDs, ","), fields["nijiiroId"], fields["officialId"]}, "\n"))

	ctx := context.WithoutCancel(r.Context())
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// A retried request returns the chart it already created.
	tag, e := tx.Exec(ctx, `INSERT INTO upload_requests(user_id,idempotency_key,payload_digest) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, u.User.ID, key, payload)
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
	var coverFile *int64
	if coverData != nil {
		id, e := s.storeImage(ctx, tx, coverData)
		if e != nil {
			internal(w, e)
			return
		}
		coverFile = &id
	}
	empty, _ := json.Marshal(map[string]string{})
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO charts(nijiiro_id,official_id,uploader_id,cover_file,description,title,subtitle,bpm,offset_seconds,demo_start,duration,title_translations,subtitle_translations,category_flags,demo_end)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,0,0,0,$9,$9,$10,15) RETURNING id`,
		nijiiroID, officialID, u.User.ID, coverFile, description, title, subtitle, bpm, empty, flags).Scan(&id)
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
