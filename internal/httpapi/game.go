package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/sitekit/audio"
	"ourtaiko.dev/sitekit/objectstore"
	"ourtaiko.dev/sitekit/video"
)

// The game API follows the protocol OurTaikoPlay speaks to the Fanmade
// backend: chart blocks as slots of course × side (chartSlotVersion 1),
// resource manifests (resourceDownloadVersion 1) and scores with an input
// recording (scoreReplayVersion 1). Native routes use SSO game sessions as
// bearer tokens; browser cookies never authorize them.
func isGameRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/v1/game/") || strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// devGameToken stands for the SSO game token of DEV_USER_ID on a local server
// without an account center.
func (s *Server) devGameToken() string {
	h := sha256.Sum256([]byte("ourtaiko-database-dev-game:" + s.Config.DevUserID))
	return hex.EncodeToString(h[:])
}

func (s *Server) gameLogin(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &c) {
		return
	}
	var result struct {
		User    User   `json:"user"`
		Token   string `json:"accessToken"`
		Expires int    `json:"expiresIn"`
	}
	if s.Config.SSO == nil {
		if s.Config.DevUserID == "" {
			problem(w, 503, "LOGIN_UNAVAILABLE", "此服务器未接入账号中心")
			return
		}
		result.User, result.Token, result.Expires = devUser(s.Config.DevUserID), s.devGameToken(), 86400
	} else {
		e := s.Config.SSO.call(r.Context(), "game/login", c, &result)
		var remote *ssoError
		if errors.As(e, &remote) {
			switch remote.Status {
			case 401:
				problem(w, 401, "LOGIN_INVALID", "用户名或密码不正确")
				return
			case 429:
				w.Header().Set("Retry-After", "600")
				problem(w, 429, "RATE_LIMITED", "尝试次数过多，请稍后重试")
				return
			case 400:
				problem(w, 400, "REQUEST_INVALID", "登录信息格式不正确")
				return
			}
		}
		if e != nil {
			internal(w, e)
			return
		}
		if !validSubject(result.User.ID) || len(result.Token) != 64 {
			internal(w, errors.New("invalid SSO login response"))
			return
		}
		result.User.AvatarURL = s.Config.SSO.avatarURL(result.User)
	}
	if e := recordUserActivity(r.Context(), s.DB, result.User.ID, true); e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, result)
}

// gameCatalog is the complete song list for a game client: the categories,
// every published chart with files in at least one, and the system playlists, read in
// one snapshot. It is the same for everyone and ignores any session; an
// account's own playlists are GET /game/playlists.
func (s *Server) gameCatalog(w http.ResponseWriter, r *http.Request) {
	tx, err := s.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), chartSelect+` WHERE `+playableChart+` AND c.category_flags<>0 ORDER BY c.created_at DESC,c.id DESC`)
	if err != nil {
		internal(w, err)
		return
	}
	charts, err := readCharts(rows)
	if err != nil {
		internal(w, err)
		return
	}
	playlists, err := systemGamePlaylists(r.Context(), tx)
	if err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	counts := map[string]int{}
	for _, c := range charts {
		for _, id := range c.CategoryIDs {
			counts[id]++
		}
	}
	categories := make([]Category, 0, len(categoryCatalog))
	for _, entry := range categoryCatalog {
		category := entry.Category
		category.ChartCount = counts[category.ID]
		categories = append(categories, category)
	}
	s.chartNames(r.Context(), charts)
	body := map[string]any{
		"categories": categories, "charts": charts, "chartCount": len(charts), "playlists": playlists, "playlistVersion": 1,
		"chartSlotVersion": 1, "resourceDownloadVersion": 1, "scoreReplayVersion": 1, "danVersion": 1,
	}
	if s.Config.SSO != nil {
		body["sso"] = s.Config.SSO.GameAccounts()
	}
	respond(w, 200, body)
}

// gameSearch is the game's song picker: keyword, course and star level that
// hold for the same block (a course covers all its sides), optionally putting
// songs the player has not full-combo'd (unfc) or all-perfected (unperfect)
// yet first. A block's scores of replaced files count only while it has no
// current one. It returns every match, without pagination.
func (s *Server) gameSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > 200 {
		problem(w, 400, "QUERY_INVALID", "搜索内容过长")
		return
	}
	course := query.Get("course")
	switch course {
	case "", "Easy", "Normal", "Hard", "Oni", "Edit":
	default:
		problem(w, 400, "DIFFICULTY_INVALID", "难度必须为 Easy / Normal / Hard / Oni / Edit")
		return
	}
	level := -1
	if raw := query.Get("level"); raw != "" {
		var e error
		if level, e = strconv.Atoi(raw); e != nil || level < 1 || level > 10 {
			problem(w, 400, "QUERY_INVALID", "星数必须在 1 到 10 之间")
			return
		}
	}
	order := query.Get("order")
	switch order {
	case "", "default", "unfc", "unperfect":
	default:
		problem(w, 400, "QUERY_INVALID", "顺序必须为 default / unfc / unperfect")
		return
	}
	// Only a game session personalizes the order; guests keep the default.
	userID := ""
	if order == "unfc" || order == "unperfect" {
		if u, e := s.current(r); e == nil {
			userID = u.User.ID
		} else if !errors.Is(e, pgx.ErrNoRows) {
			internal(w, e)
			return
		}
	}
	const matching = `SELECT 1 FROM chart_difficulties d WHERE d.chart_id=c.id AND ($3='' OR d.course=$3) AND ($4::int=-1 OR d.level=$4)`
	rows, e := s.DB.Query(r.Context(), chartSelect+` WHERE `+playableChart+` AND c.category_flags<>0 AND `+keywordClause("$1", "$2")+` AND EXISTS(`+matching+`)
	 ORDER BY ($5::text<>'' AND EXISTS(`+matching+`
	 AND NOT EXISTS(SELECT 1 FROM scores sc WHERE sc.user_id=$5 AND sc.difficulty_id=d.id
	 AND (sc.is_latest OR NOT EXISTS(SELECT 1 FROM scores cur WHERE cur.user_id=$5 AND cur.difficulty_id=d.id AND cur.is_latest))
	 AND sc.bad=0 AND (sc.good>0 OR sc.ok>0) AND ($6::text='unfc' OR sc.ok=0)))) DESC, c.created_at DESC,c.id DESC`,
		q, keywordNumber(q), course, level, userID, order)
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
	respond(w, 200, map[string]any{"items": items, "total": len(items)})
}

// resourceLink tells a client where to download one stored file and how to
// verify it.
type resourceLink struct {
	URL         string `json:"url"`
	HeadURL     string `json:"headUrl"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

// resourceLinks lists where a client downloads a chart's current files: the
// CDN address when S3 has one, else this API's own file routes under
// APP_ORIGIN. API credentials are never needed for these links.
func (s *Server) resourceLinks(w http.ResponseWriter, r *http.Request) {
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
	link := func(key, route, digest, media string, size int64) (resourceLink, error) {
		target := ""
		if remote, ok := s.Config.Objects.(*objectstore.S3); ok {
			var e error
			if target, e = remote.PublicURL(key); e != nil {
				return resourceLink{}, e
			}
		}
		if target == "" {
			target = s.Config.Origin + "/api/v1/charts/" + c.ID + "/" + route
		}
		return resourceLink{target, target, digest, size, media}, nil
	}
	tjaLink, e := link(c.TJAKey, "tja", c.TJAHash, "application/octet-stream", c.TJASize)
	if e != nil {
		internal(w, e)
		return
	}
	audioLink, e := link(c.AudioKey, "audio", c.AudioHash, audio.MediaType(c.AudioName), c.AudioSize)
	if e != nil {
		internal(w, e)
		return
	}
	resources := map[string]resourceLink{"tja": tjaLink, "audio": audioLink}
	// The song-select preview clip, as Fanmade lists it; a chart waiting for the backfill has none.
	var previewKey, previewHash string
	var previewSize int64
	e = s.DB.QueryRow(r.Context(), `SELECT r.storage_key,r.sha256,r.byte_size FROM charts c JOIN chart_resources r ON r.id=c.preview_file WHERE c.id=$1`, c.ID).Scan(&previewKey, &previewHash, &previewSize)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		internal(w, e)
		return
	}
	if e == nil {
		if resources["preview"], e = link(previewKey, "preview", previewHash, "audio/ogg", previewSize); e != nil {
			internal(w, e)
			return
		}
	}
	if c.MovieKey != "" {
		if resources["movie"], e = link(c.MovieKey, "movie", c.MovieHash, video.MediaType, c.MovieSize); e != nil {
			internal(w, e)
			return
		}
	}
	respond(w, 200, map[string]any{"chartId": c.ID, "urlsExpire": false, "resources": resources})
}
