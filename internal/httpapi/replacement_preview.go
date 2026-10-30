package httpapi

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"ourtaiko.dev/sitekit/audio"
	"ourtaiko.dev/sitekit/tja"
)

// replacementBlock is one course×side slot of a replacement as it would land:
// "same" keeps its content hash, "changed" retires its scores from the
// leaderboard, "removed" deletes the block with its scores, "added" is new.
// Scores counts the slot's current leaderboard scores.
type replacementBlock struct {
	Course string `json:"course"`
	Side   string `json:"side"`
	Change string `json:"change"`
	Scores int    `json:"scores"`
}

// previewReplacement reads a new TJA for a chart as replaceFiles would,
// without storing anything, so the uploader can review what changes before
// sending the audio: Fanmade's POST /charts/{id}/files/preview. Previous is
// the stored file's own reading (null for a song without files, or one that
// can no longer be read), which tells site edits apart from changes in the file.
func (s *Server) previewReplacement(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.managed(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, tja.MaxTJA+64*1024)
	if err := r.ParseMultipartForm(tja.MaxTJA + 64*1024); err != nil {
		uploadReadError(w, err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("tja")
	if err != nil || !tja.SafeFilename(header.Filename) || !strings.EqualFold(filepath.Ext(header.Filename), ".tja") {
		problem(w, 400, "UPLOAD_FILES_INVALID", "请选择 TJA 文件")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, tja.MaxTJA+1))
	if err != nil {
		internal(w, err)
		return
	}
	invalid := func(issue *tja.Issue) {
		respond(w, 422, map[string]any{"code": issue.Code, "message": issue.Message, "errors": []*tja.Issue{issue}, "validationVersion": tja.Version})
	}
	data, issue := tja.NormalizeUTF8(raw)
	if issue != nil {
		invalid(issue)
		return
	}
	// As replaceFiles checks WAVE: against new audio, or not at all when the stored audio stays.
	waveCheck := r.FormValue("audioName")
	if waveCheck != "" && (!tja.SafeFilename(waveCheck) || audio.MediaType(waveCheck) == "") {
		problem(w, 400, "UPLOAD_FILES_INVALID", "音频文件名无效")
		return
	}
	meta, issue := tja.Parse(data, waveCheck)
	if issue != nil {
		invalid(issue)
		return
	}
	var previous *tja.Metadata
	if c.HasFiles {
		if in, err := s.Config.Objects.Open(r.Context(), c.TJAKey); err == nil {
			stored, err := io.ReadAll(io.LimitReader(in, tja.MaxTJA+1))
			in.Close()
			if parsed, issue := tja.Parse(stored, ""); err == nil && issue == nil {
				previous = &parsed
			}
		}
	}
	rows, err := s.DB.Query(r.Context(), `SELECT d.course,d.side,COALESCE(d.content_hash,''),count(sc.difficulty_id) FILTER (WHERE sc.is_latest)
 FROM chart_difficulties d LEFT JOIN scores sc ON sc.difficulty_id=d.id WHERE d.chart_id=$1 GROUP BY d.id ORDER BY d.id`, c.ID)
	if err != nil {
		internal(w, err)
		return
	}
	type stored struct {
		hash   string
		scores int
	}
	current := map[[2]string]stored{}
	var order [][2]string
	for rows.Next() {
		var slot [2]string
		var v stored
		if err = rows.Scan(&slot[0], &slot[1], &v.hash, &v.scores); err != nil {
			rows.Close()
			internal(w, err)
			return
		}
		current[slot] = v
		order = append(order, slot)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	blocks := []replacementBlock{}
	for _, d := range meta.Difficulties {
		slot := [2]string{d.Course, d.Side}
		old, found := current[slot]
		block := replacementBlock{Course: d.Course, Side: d.Side, Change: "added"}
		if found {
			block.Change, block.Scores = "changed", old.scores
			if old.hash == d.Hash {
				block.Change = "same"
			}
			delete(current, slot)
		}
		blocks = append(blocks, block)
	}
	for _, slot := range order {
		if old, found := current[slot]; found {
			blocks = append(blocks, replacementBlock{Course: slot[0], Side: slot[1], Change: "removed", Scores: old.scores})
		}
	}
	respond(w, 200, map[string]any{"metadata": meta, "previous": previous, "blocks": blocks})
}
