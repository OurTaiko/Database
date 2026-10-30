package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"ourtaiko.dev/database/api/internal/objectstore"
)

// GamePlaylist is a folder of the game's song select: the account's own and
// favorite playlists, and the system ones, as IDs of charts in the catalog.
type GamePlaylist struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Genre string `json:"genre"`
	// "owned", "favorite" or "system".
	Kind    string `json:"kind"`
	OwnerID string `json:"ownerId,omitempty"`
	Owner   string `json:"owner"`
	// Cover is the folder's JPEG at its CDN address; left out without one.
	Cover    *resourceLink `json:"cover,omitempty"`
	ChartIDs []string      `json:"chartIds"`
	// The stored game cover, resolved to Cover after the snapshot.
	coverKey, coverHash string
	coverSize           int64
}

// systemGamePlaylists are the catalog's folders: the system playlists that
// currently have charts.
func systemGamePlaylists(ctx context.Context, db playlistQuerier) ([]GamePlaylist, error) {
	items := []GamePlaylist{}
	for i := range systemPlaylists {
		p := &systemPlaylists[i]
		ids, e := p.chartIDs(ctx, db)
		if e != nil {
			return nil, e
		}
		if len(ids) > 0 {
			items = append(items, GamePlaylist{ID: p.id, Title: p.name, Genre: categoryByFlag(p.category).Genre, Kind: "system", ChartIDs: ids})
		}
	}
	return items, nil
}

// gamePlaylistList returns the signed-in account's own playlists, private ones
// included, then its favorites. Only charts of the catalog are listed, and
// empty playlists are left out. A client adds these to the catalog's folders.
func (s *Server) gamePlaylistList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, false)
	if !ok {
		return
	}
	rows, e := s.DB.Query(r.Context(), `SELECT p.id,p.name,p.category_flag,p.owner_id,p.owner_id=$1,COALESCE(gc.storage_key,''),COALESCE(gc.sha256,''),COALESCE(gc.byte_size,0),
 ARRAY(SELECT i.chart_id FROM playlist_items i JOIN charts c ON c.id=i.chart_id WHERE i.playlist_id=p.id AND `+systemChart+` ORDER BY i.position,i.added_at,i.chart_id)
 FROM playlists p LEFT JOIN playlist_favorites mine ON mine.playlist_id=p.id AND mine.user_id=$1
 LEFT JOIN chart_resources gc ON gc.id=p.game_cover_file
 WHERE p.owner_id=$1 OR (mine.user_id IS NOT NULL AND p.is_public)
 ORDER BY p.owner_id<>$1,CASE WHEN p.owner_id=$1 THEN p.updated_at ELSE mine.favorited_at END DESC,p.id`, u.User.ID)
	if e != nil {
		internal(w, e)
		return
	}
	items := []GamePlaylist{}
	for rows.Next() {
		p := GamePlaylist{Kind: "favorite"}
		var id int64
		var flag CategoryFlags
		var own bool
		if e = rows.Scan(&id, &p.Title, &flag, &p.OwnerID, &own, &p.coverKey, &p.coverHash, &p.coverSize, &p.ChartIDs); e != nil {
			rows.Close()
			internal(w, e)
			return
		}
		if own {
			p.Kind = "owned"
		}
		p.ID, p.Genre = strconv.FormatInt(id, 10), categoryByFlag(flag).Genre
		if len(p.ChartIDs) > 0 {
			items = append(items, p)
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		internal(w, e)
		return
	}
	if e = s.gamePlaylistDetails(r.Context(), items); e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, map[string]any{"playlists": items, "playlistVersion": 1})
}

// gamePlaylistDetails fills owner nicknames and, when S3 has a public address,
// cover links.
func (s *Server) gamePlaylistDetails(ctx context.Context, items []GamePlaylist) error {
	ids := []string{}
	remote, _ := s.Config.Objects.(*objectstore.S3)
	for i, p := range items {
		if p.OwnerID != "" {
			ids = append(ids, p.OwnerID)
		}
		if remote != nil && p.coverKey != "" {
			url, e := remote.PublicURL(p.coverKey)
			if e != nil {
				return e
			}
			if url != "" {
				items[i].Cover = &resourceLink{url, url, p.coverHash, p.coverSize, "image/jpeg"}
			}
		}
	}
	profiles := s.profiles(ctx, ids)
	for i := range items {
		if items[i].OwnerID != "" {
			items[i].Owner = profiles[items[i].OwnerID].Nickname
		}
	}
	return nil
}
