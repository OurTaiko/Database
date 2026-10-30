package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type playlistRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Public      *bool   `json:"public"`
	CategoryID  *string `json:"categoryId"`
}

// normalize trims and validates the fields that are present.
func (body *playlistRequest) normalize(w http.ResponseWriter) (CategoryFlags, bool) {
	clean := func(text string, limit int, multiline bool) (string, bool) {
		text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
		for _, c := range text {
			if unicode.IsControl(c) && !(multiline && c == '\n') {
				return "", false
			}
		}
		return text, utf8.RuneCountInString(text) <= limit
	}
	if body.Name != nil {
		name, ok := clean(*body.Name, maxPlaylistName, false)
		if !ok || name == "" {
			problem(w, 400, "PLAYLIST_INVALID", "歌单名称需为 1–50 个字符")
			return 0, false
		}
		body.Name = &name
	}
	if body.Description != nil {
		text, ok := clean(*body.Description, maxPlaylistText, true)
		if !ok {
			problem(w, 400, "PLAYLIST_INVALID", "歌单简介不能超过 500 个字符")
			return 0, false
		}
		body.Description = &text
	}
	flag := CategoryVariety
	if body.CategoryID != nil {
		var ok bool
		if flag, ok = categoryFlag(*body.CategoryID); !ok {
			problem(w, 400, "CATEGORY_INVALID", "分类无效")
			return 0, false
		}
	}
	return flag, true
}

func (s *Server) respondPlaylist(w http.ResponseWriter, r *http.Request, status int, viewer string, id int64) {
	p, e := readPlaylist(s.DB.QueryRow(r.Context(), playlistSelect+` WHERE p.id=$2`, viewer, id))
	if e != nil {
		internal(w, e)
		return
	}
	items := []Playlist{p}
	s.playlistOwners(r.Context(), items)
	respond(w, status, items[0])
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	var body playlistRequest
	if !decode(w, r, &body) {
		return
	}
	if body.Name == nil {
		problem(w, 400, "PLAYLIST_INVALID", "歌单名称需为 1–50 个字符")
		return
	}
	flag, ok := body.normalize(w)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// Serialize one user's creations so the limit holds.
	var count int
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "playlists:"+u.User.ID); e == nil {
		e = tx.QueryRow(ctx, `SELECT count(*) FROM playlists WHERE owner_id=$1`, u.User.ID).Scan(&count)
	}
	if e != nil {
		internal(w, e)
		return
	}
	if count >= maxPlaylists {
		problem(w, 409, "PLAYLIST_LIMIT", "每位用户最多创建 100 个歌单")
		return
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO playlists(owner_id,name,description,is_public,category_flag) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		u.User.ID, *body.Name, description, body.Public != nil && *body.Public, flag).Scan(&id); e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	s.respondPlaylist(w, r, 201, u.User.ID, id)
}

// lockOwnPlaylist locks a playlist for the rest of tx and reports whether the
// user owns it. Others see a private playlist as missing.
func lockOwnPlaylist(w http.ResponseWriter, ctx context.Context, tx pgx.Tx, raw, user string) (int64, bool) {
	id, valid := playlistID(raw)
	var owner string
	var public bool
	e := pgx.ErrNoRows
	if valid {
		e = tx.QueryRow(ctx, `SELECT owner_id,is_public FROM playlists WHERE id=$1 FOR UPDATE`, id).Scan(&owner, &public)
	}
	switch {
	case e != nil && !errors.Is(e, pgx.ErrNoRows):
		internal(w, e)
	case e != nil || (owner != user && !public):
		problem(w, 404, "PLAYLIST_NOT_FOUND", "歌单不存在或未公开")
	case owner != user:
		problem(w, 403, "FORBIDDEN", "只能修改自己的歌单")
	default:
		return id, true
	}
	return 0, false
}

// ownPlaylist runs change in a transaction holding the user's playlist. change
// reports false after it has written its own error response.
func (s *Server) ownPlaylist(w http.ResponseWriter, r *http.Request, user string, change func(tx pgx.Tx, id int64) (bool, error)) (int64, bool) {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return 0, false
	}
	defer tx.Rollback(ctx)
	id, ok := lockOwnPlaylist(w, ctx, tx, r.PathValue("id"), user)
	if !ok {
		return 0, false
	}
	ok, e = change(tx, id)
	if e == nil && ok {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return 0, false
	}
	return id, ok
}

func (s *Server) editPlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	var body playlistRequest
	if !decode(w, r, &body) {
		return
	}
	flag, ok := body.normalize(w)
	if !ok {
		return
	}
	var category *CategoryFlags
	if body.CategoryID != nil {
		category = &flag
	}
	// An empty description clears it; only an absent field keeps the old value.
	id, ok := s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id int64) (bool, error) {
		_, e := tx.Exec(r.Context(), `UPDATE playlists SET name=COALESCE($2,name),description=COALESCE($3,description),
 is_public=COALESCE($4,is_public),category_flag=COALESCE($5,category_flag),updated_at=now() WHERE id=$1`,
			id, body.Name, body.Description, body.Public, category)
		return true, e
	})
	if !ok {
		return
	}
	s.respondPlaylist(w, r, 200, u.User.ID, id)
}

// deletePlaylist lets the owner delete a playlist, and an administrator a
// public one; the owner is then told, as on Fanmade.
func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	id, valid := playlistID(r.PathValue("id"))
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	var owner, name string
	var public bool
	var covers []*int64
	e = pgx.ErrNoRows
	if valid {
		e = tx.QueryRow(ctx, `SELECT owner_id,name,is_public,ARRAY[cover_file,game_cover_file] FROM playlists WHERE id=$1 FOR UPDATE`, id).Scan(&owner, &name, &public, &covers)
	}
	if errors.Is(e, pgx.ErrNoRows) || (e == nil && owner != u.User.ID && !public) {
		problem(w, 404, "PLAYLIST_NOT_FOUND", "歌单不存在或未公开")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	if owner != u.User.ID && !u.User.IsAdmin {
		problem(w, 403, "FORBIDDEN", "只能删除自己的歌单")
		return
	}
	if _, e = tx.Exec(ctx, `DELETE FROM playlists WHERE id=$1`, id); e == nil && owner != u.User.ID {
		// The owner learns of a removal, but not which administrator removed it.
		e = notify(ctx, tx, Notice{Recipient: owner, Kind: "playlist_removed", Data: map[string]any{"name": name}})
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	s.releaseCovers(ctx, covers)
	respond(w, 200, map[string]bool{"ok": true})
}

// releaseCovers deletes a playlist's former cover files unless something
// still refers to them; a failure leaves them for the next release.
func (s *Server) releaseCovers(ctx context.Context, covers []*int64) {
	ids := []int64{}
	for _, id := range covers {
		if id != nil {
			ids = append(ids, *id)
		}
	}
	if e := s.releaseFiles(context.WithoutCancel(ctx), ids); e != nil {
		log.Printf("playlist cover cleanup pending: %v", e)
	}
}

func (s *Server) addPlaylistItem(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	chart := r.PathValue("chart")
	added, count := false, 0
	_, ok = s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id int64) (bool, error) {
		// Holding the chart row FOR SHARE serializes the addition with a deletion.
		e := tx.QueryRow(ctx, `SELECT c.id FROM charts c WHERE c.id=$1 AND `+publishedChart+` FOR SHARE`, chart).Scan(&chart)
		if errors.Is(e, pgx.ErrNoRows) {
			problem(w, 404, "CHART_NOT_FOUND", "谱面不存在或已删除")
			return false, nil
		}
		var exists bool
		var last *int
		if e == nil {
			e = tx.QueryRow(ctx, `SELECT count(*),max(position),COALESCE(bool_or(chart_id=$2),false) FROM playlist_items WHERE playlist_id=$1`, id, chart).Scan(&count, &last, &exists)
		}
		if e != nil || exists {
			return true, e
		}
		if count >= maxPlaylistItems {
			problem(w, 409, "PLAYLIST_FULL", "每个歌单最多收录 500 首谱面")
			return false, nil
		}
		position := 0
		if last != nil {
			position = *last + 1
		}
		if _, e = tx.Exec(ctx, `INSERT INTO playlist_items(playlist_id,chart_id,position) VALUES($1,$2,$3)`, id, chart, position); e == nil {
			_, e = tx.Exec(ctx, `UPDATE playlists SET updated_at=now() WHERE id=$1`, id)
		}
		added, count = true, count+1
		return true, e
	})
	if !ok {
		return
	}
	status := 200
	if added {
		status = 201
	}
	respond(w, status, map[string]any{"added": added, "itemCount": count})
}

func (s *Server) removePlaylistItem(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	chart := r.PathValue("chart")
	_, ok = s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id int64) (bool, error) {
		tag, e := tx.Exec(ctx, `DELETE FROM playlist_items WHERE playlist_id=$1 AND chart_id=$2`, id, chart)
		if e == nil && tag.RowsAffected() > 0 {
			_, e = tx.Exec(ctx, `UPDATE playlists SET updated_at=now() WHERE id=$1`, id)
		}
		return true, e
	})
	if !ok {
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// orderPlaylist moves the listed charts to the front in the given order.
// Charts the request leaves out, such as ones hidden from the editor, keep
// their relative order behind them.
func (s *Server) orderPlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	var body struct {
		ChartIDs []string `json:"chartIds"`
	}
	if !decode(w, r, &body) {
		return
	}
	seen := map[string]bool{}
	for _, chart := range body.ChartIDs {
		seen[chart] = true
	}
	if len(body.ChartIDs) > maxPlaylistItems || len(seen) != len(body.ChartIDs) {
		problem(w, 400, "PLAYLIST_INVALID", "谱面顺序无效")
		return
	}
	ctx := r.Context()
	_, ok = s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id int64) (bool, error) {
		rows, e := tx.Query(ctx, `SELECT chart_id FROM playlist_items WHERE playlist_id=$1 ORDER BY position,added_at,chart_id`, id)
		if e != nil {
			return false, e
		}
		current, e := pgx.CollectRows(rows, pgx.RowTo[string])
		if e != nil {
			return false, e
		}
		order := slices.Clone(body.ChartIDs)
		for _, chart := range current {
			if !seen[chart] {
				order = append(order, chart)
			}
		}
		if len(order) != len(current) {
			problem(w, 409, "PLAYLIST_CHANGED", "歌单内容已变化，请刷新后重试")
			return false, nil
		}
		_, e = tx.Exec(ctx, `UPDATE playlist_items i SET position=listed.ord-1 FROM unnest($2::text[]) WITH ORDINALITY listed(chart_id,ord)
 WHERE i.playlist_id=$1 AND i.chart_id=listed.chart_id`, id, order)
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE playlists SET updated_at=now() WHERE id=$1`, id)
		}
		return true, e
	})
	if !ok {
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// favoritePlaylist keeps a public playlist of another user as a favorite.
func (s *Server) favoritePlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	id, valid := playlistID(r.PathValue("id"))
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// Holding the playlist FOR SHARE serializes the favorite with a deletion.
	var owner string
	e = pgx.ErrNoRows
	if valid {
		e = tx.QueryRow(ctx, `SELECT owner_id FROM playlists WHERE id=$1 AND is_public FOR SHARE`, id).Scan(&owner)
	}
	if errors.Is(e, pgx.ErrNoRows) {
		problem(w, 404, "PLAYLIST_NOT_FOUND", "歌单不存在或未公开")
		return
	}
	if e == nil && owner == u.User.ID {
		problem(w, 409, "OWN_PLAYLIST", "不能收藏自己的歌单")
		return
	}
	// Serialize one user's favorites so the limit holds.
	var count int
	var exists bool
	if e == nil {
		_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "playlist-favorites:"+u.User.ID)
	}
	if e == nil {
		e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(playlist_id=$2),false) FROM playlist_favorites WHERE user_id=$1`, u.User.ID, id).Scan(&count, &exists)
	}
	if e == nil && !exists {
		if count >= maxPlaylistFavorites {
			problem(w, 409, "FAVORITE_LIMIT", "最多收藏 200 个歌单")
			return
		}
		_, e = tx.Exec(ctx, `INSERT INTO playlist_favorites(playlist_id,user_id) VALUES($1,$2)`, id, u.User.ID)
	}
	respondFavorite(w, ctx, tx, id, true, e)
}

// unfavoritePlaylist also works on a playlist that has turned private.
func (s *Server) unfavoritePlaylist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	id, _ := playlistID(r.PathValue("id"))
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `DELETE FROM playlist_favorites WHERE playlist_id=$1 AND user_id=$2`, id, u.User.ID)
	respondFavorite(w, ctx, tx, id, false, e)
}

// respondFavorite commits a favorite change, unless e reports that it failed,
// and replies with the playlist's new count.
func respondFavorite(w http.ResponseWriter, ctx context.Context, tx pgx.Tx, id int64, favorited bool, e error) {
	var total int
	if e == nil {
		e = tx.QueryRow(ctx, `SELECT count(*) FROM playlist_favorites WHERE playlist_id=$1`, id).Scan(&total)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, map[string]any{"favorited": favorited, "favoriteCount": total})
}
