package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Playlists follow the Fanmade site's (R035): user playlists that are private
// or public, ordered, with one category and an optional cover; favorites of
// other users' public playlists; and system playlists the server computes.
const (
	maxPlaylists         = 100
	maxPlaylistItems     = 500
	maxPlaylistFavorites = 200
	maxPlaylistName      = 50
	maxPlaylistText      = 500
	playlistPageSize     = 12
)

// PlaylistOwner is the public profile of a playlist's owner.
type PlaylistOwner struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

// Playlist is an ordered list of charts. A stored playlist's ID is its row ID
// in decimal; a system playlist's is a word, and it has no owner and cannot
// be edited or kept as a favorite.
type Playlist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
	System      bool   `json:"system"`
	// The category's genre selects the folder artwork in the game.
	CategoryID    string         `json:"categoryId"`
	Genre         string         `json:"genre"`
	OwnerID       string         `json:"ownerId,omitempty"`
	Owner         *PlaylistOwner `json:"owner"`
	CoverHash     string         `json:"coverHash,omitempty"`
	ChartCount    int            `json:"chartCount"`
	FavoriteCount int            `json:"favoriteCount"`
	// Favorited is only filled for a signed-in viewer.
	Favorited bool       `json:"favorited"`
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
	// Only in the viewer's own list when it asks about one chart.
	ContainsChart *bool `json:"containsChart,omitempty"`
	// Only in the detail response, in playlist order.
	Charts []Chart `json:"charts,omitempty"`
}

type PlaylistPage struct {
	Items    []Playlist `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

type MyPlaylists struct {
	Owned        []Playlist `json:"owned"`
	Favorites    []Playlist `json:"favorites"`
	MaxPlaylists int        `json:"maxPlaylists"`
	MaxItems     int        `json:"maxItems"`
}

type playlistQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// playlistID parses a stored playlist's ID.
func playlistID(raw string) (string, bool) {
	return raw, validID(raw)
}

func categoryByFlag(flag CategoryFlags) Category {
	for _, category := range categoryCatalog {
		if category.Flag == flag {
			return category.Category
		}
	}
	return Category{}
}

// viewer is the signed-in user by the website cookie alone, without asking the
// account center; empty for a guest.
func (s *Server) viewer(r *http.Request) string {
	v, e := s.localSession(r)
	if e != nil {
		return ""
	}
	return v.User.ID
}

// playlistChartCount counts the charts of playlist p that are still published.
const playlistChartCount = `(SELECT count(*) FROM playlist_items i JOIN charts c ON c.id=i.chart_id WHERE i.playlist_id=p.id AND ` + publishedChart + `)`

// $1 is the viewer, empty for a guest.
const playlistSelect = `SELECT p.id,p.owner_id,p.name,p.description,p.is_public,p.category_flag,
 COALESCE((SELECT sha256 FROM chart_resources WHERE id=p.cover_file),''),p.created_at,p.updated_at,` + playlistChartCount + `,
 (SELECT count(*) FROM playlist_favorites f WHERE f.playlist_id=p.id),
 EXISTS(SELECT 1 FROM playlist_favorites f WHERE f.playlist_id=p.id AND f.user_id=$1)
 FROM playlists p `

func readPlaylist(row pgx.Row) (Playlist, error) {
	var p Playlist
	var flag CategoryFlags
	var created, updated time.Time
	e := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.Public, &flag, &p.CoverHash, &created, &updated, &p.ChartCount, &p.FavoriteCount, &p.Favorited)
	category := categoryByFlag(flag)
	p.CategoryID, p.Genre, p.CreatedAt, p.UpdatedAt = category.ID, category.Genre, &created, &updated
	return p, e
}

func readPlaylists(ctx context.Context, db playlistQuerier, sql string, args ...any) ([]Playlist, error) {
	rows, e := db.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []Playlist{}
	for rows.Next() {
		p, e := readPlaylist(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// profiles looks up public profiles; the account center being down leaves
// them unknown rather than failing the request.
func (s *Server) profiles(ctx context.Context, ids []string) map[string]PublicProfile {
	found := map[string]PublicProfile{}
	if s.Config.SSO == nil {
		for _, id := range ids {
			if id == s.Config.DevUserID {
				found[id] = PublicProfile{Nickname: devUser(id).Nickname}
			}
		}
		return found
	}
	if len(ids) > 0 {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if result, e := s.Config.SSO.Profiles(ctx, ids); e == nil {
			found = result
		}
	}
	return found
}

// playlistOwners fills the owners' public profiles; call it after the database
// work is done, because it contacts SSO.
func (s *Server) playlistOwners(ctx context.Context, lists ...[]Playlist) {
	ids, seen := []string{}, map[string]bool{}
	for _, items := range lists {
		for _, p := range items {
			if p.OwnerID != "" && !seen[p.OwnerID] {
				seen[p.OwnerID] = true
				ids = append(ids, p.OwnerID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	profiles := s.profiles(ctx, ids)
	for _, items := range lists {
		for i := range items {
			if id := items[i].OwnerID; id != "" {
				p, ok := profiles[id]
				if !ok {
					p.Nickname = "未知用户"
				}
				items[i].Owner = &PlaylistOwner{ID: id, Nickname: p.Nickname, AvatarURL: p.AvatarURL}
			}
		}
	}
}

func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q, owner := strings.TrimSpace(query.Get("q")), query.Get("owner")
	if utf8.RuneCountInString(q) > 100 || (owner != "" && !validSubject(owner)) {
		problem(w, 400, "QUERY_INVALID", "搜索条件无效")
		return
	}
	page := 1
	if raw := query.Get("page"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 10000 {
			problem(w, 400, "QUERY_INVALID", "页码无效")
			return
		}
		page = n
	}
	order := `p.updated_at DESC,p.id`
	switch query.Get("sort") {
	case "", "updated":
	case "newest":
		order = `p.created_at DESC,p.id`
	case "favorites":
		order = `(SELECT count(*) FROM playlist_favorites f WHERE f.playlist_id=p.id) DESC,p.updated_at DESC,p.id`
	default:
		problem(w, 400, "QUERY_INVALID", "顺序必须为 updated / newest / favorites")
		return
	}
	// The plaza leaves out playlists without a published chart; one user's list shows them all.
	const filter = ` WHERE p.is_public AND ($2='' OR position(lower($2) in lower(p.name||' '||p.description))>0)
 AND (CASE WHEN $3='' THEN ` + playlistChartCount + `>0 ELSE p.owner_id=$3 END)`
	tx, e := s.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	result := PlaylistPage{Page: page, PageSize: playlistPageSize}
	viewer := s.viewer(r)
	listing := playlistSelect + filter
	if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM (`+listing+`) listed`, viewer, q, owner).Scan(&result.Total); e == nil {
		result.Items, e = readPlaylists(r.Context(), tx, listing+` ORDER BY `+order+` LIMIT $4 OFFSET $5`, viewer, q, owner, playlistPageSize, (page-1)*playlistPageSize)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		internal(w, e)
		return
	}
	s.playlistOwners(r.Context(), result.Items)
	respond(w, 200, result)
}

func (s *Server) myPlaylists(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	result := MyPlaylists{MaxPlaylists: maxPlaylists, MaxItems: maxPlaylistItems}
	result.Owned, e = readPlaylists(ctx, tx, playlistSelect+` WHERE p.owner_id=$1 ORDER BY p.updated_at DESC,p.id`, u.User.ID)
	if e == nil {
		result.Favorites, e = readPlaylists(ctx, tx, playlistSelect+` JOIN playlist_favorites mine ON mine.playlist_id=p.id
 WHERE mine.user_id=$1 AND p.is_public ORDER BY mine.favorited_at DESC,p.id`, u.User.ID)
	}
	if raw := r.URL.Query().Get("chart"); e == nil && raw != "" {
		chart := raw
		var rows pgx.Rows
		if rows, e = tx.Query(ctx, `SELECT i.playlist_id::text FROM playlist_items i JOIN playlists p ON p.id=i.playlist_id WHERE p.owner_id=$1 AND i.chart_id=$2`, u.User.ID, chart); e == nil {
			var containing []string
			if containing, e = pgx.CollectRows(rows, pgx.RowTo[string]); e == nil {
				for i := range result.Owned {
					contains := slices.Contains(containing, result.Owned[i].ID)
					result.Owned[i].ContainsChart = &contains
				}
			}
		}
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	s.playlistOwners(ctx, result.Owned, result.Favorites)
	respond(w, 200, result)
}

// playlistCharts reads published charts in the order of ids.
func (s *Server) playlistCharts(ctx context.Context, ids []string) ([]Chart, error) {
	rows, e := s.DB.Query(ctx, chartSelect+` JOIN unnest($1::text[]) WITH ORDINALITY listed(id,ord) ON listed.id=c.id WHERE `+publishedChart+` ORDER BY listed.ord`, ids)
	if e != nil {
		return nil, e
	}
	charts, e := readCharts(rows)
	if e != nil {
		return nil, e
	}
	s.chartNames(ctx, charts)
	return charts, nil
}

// storedPlaylistSongs reads a playlist the requester may see and its chart
// IDs in order. It reports false after writing the error response.
func (s *Server) storedPlaylistSongs(w http.ResponseWriter, r *http.Request, raw string) (Playlist, []string, bool) {
	ctx := r.Context()
	id, ok := playlistID(raw)
	p, e := Playlist{}, pgx.ErrNoRows
	if ok {
		p, e = readPlaylist(s.DB.QueryRow(ctx, playlistSelect+` WHERE p.id=$2`, s.viewer(r), id))
	}
	if e == nil && !p.Public {
		// A private playlist needs a verified session, not just the cookie.
		u, ue := s.current(r)
		if ue != nil && !errors.Is(ue, pgx.ErrNoRows) {
			internal(w, ue)
			return p, nil, false
		}
		if ue != nil || u.User.ID != p.OwnerID {
			e = pgx.ErrNoRows
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PLAYLIST_NOT_FOUND", "歌单不存在或未公开")
		return p, nil, false
	}
	var ids []string
	if e == nil {
		var rows pgx.Rows
		if rows, e = s.DB.Query(ctx, `SELECT chart_id FROM playlist_items WHERE playlist_id=$1 ORDER BY position,added_at,chart_id`, id); e == nil {
			ids, e = pgx.CollectRows(rows, pgx.RowTo[string])
		}
	}
	if e != nil {
		internal(w, e)
		return p, nil, false
	}
	return p, ids, true
}

func (s *Server) playlistDetail(w http.ResponseWriter, r *http.Request) {
	var p Playlist
	var ids []string
	if system := systemPlaylistByID(r.PathValue("id")); system != nil {
		var e error
		if ids, e = system.chartIDs(r.Context(), s.DB); e != nil {
			internal(w, e)
			return
		}
		p = system.playlist(len(ids))
	} else {
		var ok bool
		if p, ids, ok = s.storedPlaylistSongs(w, r, r.PathValue("id")); !ok {
			return
		}
	}
	charts, e := s.playlistCharts(r.Context(), ids)
	if e != nil {
		internal(w, e)
		return
	}
	p.Charts, p.ChartCount = charts, len(charts)
	items := []Playlist{p}
	s.playlistOwners(r.Context(), items)
	respond(w, 200, items[0])
}
