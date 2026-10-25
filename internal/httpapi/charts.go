package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/database/api/internal/constants"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/tja"
)

type Chart struct {
	ID int64 `json:"id"`
	// NijiiroID and OfficialID are null when unknown.
	NijiiroID  *int32  `json:"nijiiroId"`
	OfficialID *string `json:"officialId"`
	UploaderID string  `json:"uploaderId"`
	Uploader   string  `json:"uploader"`
	// UploaderAvatarURL is empty when the uploader has no avatar or SSO is unavailable.
	UploaderAvatarURL string `json:"uploaderAvatarUrl"`
	Description       string `json:"description"`
	// Votes and comments; MyVote is only filled for the signed-in detail view.
	Score        int       `json:"score"`
	Upvotes      int       `json:"upvotes"`
	Downvotes    int       `json:"downvotes"`
	CommentCount int       `json:"commentCount"`
	MyVote       int       `json:"myVote"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdateAt     time.Time `json:"updateAt"`
	Duration     float64   `json:"duration"`
	DemoEnd      float64   `json:"demoEnd"`
	CategoryIDs  []string  `json:"categoryIds"`
	TJAName      string    `json:"tjaName"`
	AudioName    string    `json:"audioName"`
	// The stored file's hash: object key and download check, not the chart's identity (that is
	// each block's chartHash). Not in the JSON; the resource manifest carries it as sha256.
	TJAHash   string `json:"-"`
	TJASize   int64  `json:"tjaSize"`
	AudioHash string `json:"audioHash"`
	AudioSize int64  `json:"audioSize"`
	CoverHash string `json:"coverHash,omitempty"`
	// HasFiles is false for a song registered without a TJA and audio (R038):
	// it has no blocks, no duration, and the file fields are empty.
	HasFiles bool   `json:"hasFiles"`
	TJAKey   string `json:"-"`
	AudioKey string `json:"-"`
	CoverKey string `json:"-"`
	tja.Metadata
}

const publishedChart = `c.status='published'`

// playableChart is a published chart with files: what the game can play.
const chartScoreSQL = `COALESCE((SELECT upvotes-downvotes FROM chart_stats WHERE chart_id=c.id),0)`

const playableChart = publishedChart + ` AND c.tja_file IS NOT NULL`

const chartSelect = `SELECT c.id,c.nijiiro_id,c.official_id,c.uploader_id,c.description,c.created_at,c.update_at,c.duration,c.demo_end,c.category_flags,
 COALESCE(tf.original_filename,''),COALESCE(af.original_filename,''),COALESCE(tf.sha256,''),COALESCE(tf.byte_size,0),COALESCE(af.sha256,''),COALESCE(af.byte_size,0),COALESCE(tf.storage_key,''),COALESCE(af.storage_key,''),COALESCE(cf.sha256,''),COALESCE(cf.storage_key,''),
 c.title,c.subtitle,c.bpm,c.offset_seconds,c.demo_start,COALESCE(af.sha256||'.'||af.extension,''),c.title_translations,c.subtitle_translations,
 (SELECT COALESCE(jsonb_agg(jsonb_build_object('id',d.id,'course',d.course,'side',d.side,'level',d.level,'maker',d.maker,'branching',d.branching,'chartHash',d.content_hash)
   ORDER BY array_position(ARRAY['Easy','Normal','Hard','Oni','Edit'],d.course),array_position(ARRAY['single','p1','p2'],d.side)),'[]'::jsonb)
   FROM chart_difficulties d WHERE d.chart_id=c.id),
 COALESCE((SELECT upvotes FROM chart_stats WHERE chart_id=c.id),0),COALESCE((SELECT downvotes FROM chart_stats WHERE chart_id=c.id),0),COALESCE((SELECT comment_count FROM chart_stats WHERE chart_id=c.id),0)
 FROM charts c LEFT JOIN chart_resources tf ON tf.id=c.tja_file LEFT JOIN chart_resources af ON af.id=c.audio_file LEFT JOIN chart_resources cf ON cf.id=c.cover_file `

func readChart(row pgx.Row) (Chart, error) {
	var c Chart
	var difficulties []byte
	var flags CategoryFlags
	e := row.Scan(&c.ID, &c.NijiiroID, &c.OfficialID, &c.UploaderID, &c.Description, &c.CreatedAt, &c.UpdateAt, &c.Duration, &c.DemoEnd, &flags,
		&c.TJAName, &c.AudioName, &c.TJAHash, &c.TJASize, &c.AudioHash, &c.AudioSize, &c.TJAKey, &c.AudioKey, &c.CoverHash, &c.CoverKey,
		&c.Title, &c.Subtitle, &c.BPM, &c.Offset, &c.DemoStart, &c.Wave, &c.TitleTranslations, &c.SubtitleTranslations, &difficulties,
		&c.Upvotes, &c.Downvotes, &c.CommentCount)
	if e == nil {
		c.Score = c.Upvotes - c.Downvotes
		c.HasFiles = c.TJAKey != ""
		c.CategoryIDs = flags.IDs()
		e = json.Unmarshal(difficulties, &c.Difficulties)
		c.Maker = difficultyMakers(c.Difficulties)
	}
	return c, e
}

// Keep the first occurrence in block order, so credits never shuffle between reads.
func difficultyMakers(difficulties []tja.Difficulty) string {
	seen := map[string]bool{}
	names := []string{}
	for _, d := range difficulties {
		name := strings.TrimSpace(d.Maker)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return strings.Join(names, " | ")
}

func readCharts(rows pgx.Rows) ([]Chart, error) {
	defer rows.Close()
	items := []Chart{}
	for rows.Next() {
		c, e := readChart(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// chartID parses the {id} path value; chart IDs are positive bigints.
func chartID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id <= 0 {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return 0, false
	}
	return id, true
}

func (s *Server) chart(ctx context.Context, id int64) (Chart, error) {
	c, e := readChart(s.DB.QueryRow(ctx, chartSelect+` WHERE c.id=$1 AND `+publishedChart, id))
	if e == nil {
		items := []Chart{c}
		s.chartNames(ctx, items)
		c = items[0]
	}
	return c, e
}

// Public catalogs stay available during an identity-provider outage.
func (s *Server) chartNames(ctx context.Context, charts []Chart) {
	ids, seen := []string{}, map[string]bool{}
	for _, c := range charts {
		if c.UploaderID != database.SystemUploaderID && !seen[c.UploaderID] {
			seen[c.UploaderID] = true
			ids = append(ids, c.UploaderID)
		}
	}
	profiles := map[string]PublicProfile{}
	if s.Config.SSO != nil && len(ids) > 0 {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if found, e := s.Config.SSO.Profiles(ctx, ids); e == nil {
			profiles = found
		}
	}
	for i := range charts {
		switch id := charts[i].UploaderID; {
		case id == database.SystemUploaderID:
			charts[i].Uploader = "ESE (TJADB)"
		case s.Config.SSO == nil && id == s.Config.DevUserID:
			charts[i].Uploader = devUser(id).Nickname
		default:
			p, ok := profiles[id]
			if !ok {
				p.Nickname = "未知用户"
			}
			charts[i].Uploader, charts[i].UploaderAvatarURL = p.Nickname, p.AvatarURL
		}
	}
}

const pageSize = 24

// keywordClause matches a chart whose title, subtitle, any translation,
// official ID or any block's maker contains the keyword q; number is
// keywordNumber(q). An empty keyword matches every chart.
func keywordClause(q, number string) string {
	return `(` + q + `='' OR c.title ILIKE '%'||` + q + `||'%' OR c.subtitle ILIKE '%'||` + q + `||'%' OR c.official_id ILIKE '%'||` + q + `||'%'
 OR c.nijiiro_id=` + number + ` OR c.id=` + number + `
 OR EXISTS(SELECT 1 FROM chart_difficulties dm WHERE dm.chart_id=c.id AND dm.maker ILIKE '%'||` + q + `||'%')
 OR EXISTS(SELECT 1 FROM jsonb_each_text(c.title_translations) t WHERE t.value ILIKE '%'||` + q + `||'%')
 OR EXISTS(SELECT 1 FROM jsonb_each_text(c.subtitle_translations) t WHERE t.value ILIKE '%'||` + q + `||'%'))`
}

// keywordNumber lets a number also find the chart with that Nijiiro ID or
// database ID; -1 matches none.
func keywordNumber(q string) int64 {
	if n, e := strconv.ParseInt(q, 10, 64); e == nil && n > 0 {
		return n
	}
	return -1
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	uploader := r.URL.Query().Get("uploader")
	if uploader != "" && !validSubject(uploader) {
		problem(w, 400, "QUERY_INVALID", "用户标识无效")
		return
	}
	s.search(w, r, uploader)
}
func (s *Server) mine(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, false)
	if !ok {
		return
	}
	s.search(w, r, u.User.ID)
}

// search pages the chart list. Filters: keyword, uploader, category, a
// course and star level that must hold for the same block (any side),
// whether the chart has a Nijiiro ID, and whether it has files.
func (s *Server) search(w http.ResponseWriter, r *http.Request, uploader string) {
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > 200 {
		problem(w, 400, "QUERY_INVALID", "搜索内容过长")
		return
	}
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		problem(w, 400, "QUERY_INVALID", "页码超出范围")
		return
	}
	var flag CategoryFlags
	if id := query.Get("category"); id != "" {
		var ok bool
		if flag, ok = categoryFlag(id); !ok {
			problem(w, 400, "QUERY_INVALID", "分类不存在")
			return
		}
	}
	course := query.Get("course")
	switch course {
	case "", "Easy", "Normal", "Hard", "Oni", "Edit":
	default:
		problem(w, 400, "QUERY_INVALID", "难度必须为 Easy / Normal / Hard / Oni / Edit")
		return
	}
	level := 0
	if raw := query.Get("level"); raw != "" {
		var e error
		if level, e = strconv.Atoi(raw); e != nil || level < 1 || level > 10 {
			problem(w, 400, "QUERY_INVALID", "星数必须在 1 到 10 之间")
			return
		}
	}
	mapped := query.Get("nijiiro")
	if mapped != "" && mapped != "yes" && mapped != "no" {
		problem(w, 400, "QUERY_INVALID", "nijiiro 必须为 yes / no")
		return
	}
	files := query.Get("files")
	if files != "" && files != "yes" && files != "no" {
		problem(w, 400, "QUERY_INVALID", "files 必须为 yes / no")
		return
	}
	// The star level a level sort ranks by: the filtered course's, else the hardest.
	const stars = `(SELECT max(d.level) FROM chart_difficulties d WHERE d.chart_id=c.id AND ($4='' OR d.course=$4))`
	ordering := ` ORDER BY c.created_at DESC,c.id DESC`
	switch query.Get("order") {
	case "", "default":
	case "nijiiro":
		ordering = ` ORDER BY c.nijiiro_id ASC NULLS LAST,c.id ASC`
	case "title":
		ordering = ` ORDER BY lower(c.title) ASC,c.id ASC`
	case "bpm":
		ordering = ` ORDER BY c.bpm DESC,c.id ASC`
	case "level":
		ordering = ` ORDER BY ` + stars + ` DESC NULLS LAST,c.id ASC`
	case "updated":
		ordering = ` ORDER BY c.update_at DESC,c.id DESC`
	// As on Fanmade. Reddit's "hot": the vote score counts logarithmically
	// against age, so every tenfold score keeps a chart as high as one 12.5 hours newer.
	case "hot":
		ordering = ` ORDER BY sign(` + chartScoreSQL + `)*log(greatest(abs(` + chartScoreSQL + `),1))+extract(epoch FROM c.created_at)/45000 DESC,c.created_at DESC,c.id DESC`
	case "top":
		ordering = ` ORDER BY ` + chartScoreSQL + ` DESC,c.created_at DESC,c.id DESC`
	case "comments":
		ordering = ` ORDER BY COALESCE((SELECT comment_count FROM chart_stats WHERE chart_id=c.id),0) DESC,c.created_at DESC,c.id DESC`
	default:
		problem(w, 400, "QUERY_INVALID", "顺序必须为 default / nijiiro / title / bpm / level / updated / hot / top / comments")
		return
	}
	number := keywordNumber(q)
	where := ` WHERE ` + publishedChart + ` AND ` + keywordClause("$1", "$7") + `
 AND ($2='' OR c.uploader_id=$2)
 AND ($3::int=0 OR (c.category_flags & $3::int)<>0)
 AND (($4='' AND $5::int=0) OR EXISTS(SELECT 1 FROM chart_difficulties d
   WHERE d.chart_id=c.id AND ($4='' OR d.course=$4) AND ($5::int=0 OR d.level=$5)))
 AND ($6='' OR (c.nijiiro_id IS NOT NULL)=($6='yes'))
 AND ($8='' OR (c.tja_file IS NOT NULL)=($8='yes'))`
	args := []any{q, uploader, int(flag), course, level, mapped, number, files}
	var total int
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM charts c`+where, args...).Scan(&total); e != nil {
		internal(w, e)
		return
	}
	rows, e := s.DB.Query(r.Context(), chartSelect+where+ordering+` LIMIT $9 OFFSET $10`, append(args, pageSize, (page-1)*pageSize)...)
	if e != nil {
		internal(w, e)
		return
	}
	items, e := readCharts(rows)
	if e != nil {
		internal(w, e)
		return
	}
	s.chartNames(r.Context(), items)
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := chartID(w, r)
	if !ok {
		return
	}
	c, e := s.chart(r.Context(), id)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	// The difficulty constants, for the chart page only (lists and the game catalog go without).
	ids := make([]int64, len(c.Difficulties))
	for i, d := range c.Difficulties {
		ids[i] = d.ID
	}
	shown, e := constants.ForBlocks(r.Context(), s.DB, ids)
	if e != nil {
		internal(w, e)
		return
	}
	for i := range c.Difficulties {
		c.Difficulties[i].Constants = shown[c.Difficulties[i].ID]
	}
	if viewer := s.viewer(r); viewer != "" {
		e = s.DB.QueryRow(r.Context(), `SELECT value FROM chart_votes WHERE chart_id=$1 AND user_id=$2`, c.ID, viewer).Scan(&c.MyVote)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			internal(w, e)
			return
		}
	}
	respond(w, 200, c)
}

// canManage: the uploader while holding the upload permission, or an administrator.
func canManage(u User, c Chart) bool {
	return u.IsAdmin || (u.CanUpload && u.ID == c.UploaderID)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	id, ok := chartID(w, r)
	if !ok {
		return
	}
	c, e := s.chart(r.Context(), id)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	if !canManage(u.User, c) {
		problem(w, 403, "FORBIDDEN", "只有上传者或管理员可以删除谱面")
		return
	}
	files, e := s.deleteChart(r.Context(), id)
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "CHART_NOT_FOUND", "谱面不存在")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	// Shared files stay while another chart refers to them.
	if e = s.releaseFiles(context.WithoutCancel(r.Context()), files); e != nil {
		log.Printf("deleted chart file cleanup pending: %v", e)
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// deleteChart removes a chart's scores and row, returning the file records it
// referenced.
func (s *Server) deleteChart(ctx context.Context, id int64) ([]int64, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var files []*int64
	if e = tx.QueryRow(ctx, `SELECT ARRAY[tja_file,audio_file,cover_file,preview_file] FROM charts WHERE id=$1 FOR UPDATE`, id).Scan(&files); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM scores WHERE difficulty_id IN (SELECT id FROM chart_difficulties WHERE chart_id=$1)`, id); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM charts WHERE id=$1`, id); e != nil {
		return nil, e
	}
	ids := []int64{}
	for _, file := range files {
		if file != nil {
			ids = append(ids, *file)
		}
	}
	return ids, tx.Commit(ctx)
}
