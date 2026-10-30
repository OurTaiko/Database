package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"ourtaiko.dev/sitekit/cover"
)

func (s *Server) getPlaylistCover(w http.ResponseWriter, r *http.Request) {
	var digest, key string
	id, valid := playlistID(r.PathValue("id"))
	e := pgx.ErrNoRows
	// The address of a private playlist's cover is its ID and current hash.
	if valid {
		e = s.DB.QueryRow(r.Context(), `SELECT cv.sha256,cv.storage_key FROM chart_resources cv JOIN playlists p ON p.cover_file=cv.id WHERE p.id=$1`, id).Scan(&digest, &key)
	}
	if errors.Is(e, pgx.ErrNoRows) || (e == nil && r.URL.Query().Get("v") != digest) {
		problem(w, 404, "COVER_NOT_FOUND", "封面不存在或已被替换")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	s.serveObject(w, r, key, "cover.webp", digest, "image/webp", "inline")
}

// setPlaylistCover points a playlist at its website cover and the game's copy
// of it, or at neither, and returns the files it pointed at before.
func setPlaylistCover(r *http.Request, tx pgx.Tx, id string, file, gameFile *int64) ([]*int64, error) {
	var previous []*int64
	e := tx.QueryRow(r.Context(), `SELECT ARRAY[cover_file,game_cover_file] FROM playlists WHERE id=$1`, id).Scan(&previous)
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE playlists SET cover_file=$2,game_cover_file=$3,updated_at=now() WHERE id=$1`, id, file, gameFile)
	}
	return previous, e
}

func (s *Server) replacePlaylistCover(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		problem(w, 503, "UPLOAD_BUSY", "当前正在处理其他上传，请稍后重试")
		return
	}
	encoded, ok := receiveCover(w, r)
	if !ok {
		return
	}
	game, e := cover.GameImage(encoded)
	if e != nil {
		internal(w, e)
		return
	}
	var previous []*int64
	// Ownership is checked after encoding, under the playlist's lock.
	_, ok = s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id string) (bool, error) {
		file, e := s.storeImage(r.Context(), tx, encoded)
		if e != nil {
			return false, e
		}
		gameFile, e := s.storeImageAs(r.Context(), tx, game, "jpg", "image/jpeg")
		if e != nil {
			return false, e
		}
		previous, e = setPlaylistCover(r, tx, id, &file, &gameFile)
		return true, e
	})
	if !ok {
		return
	}
	s.releaseCovers(r.Context(), previous)
	respond(w, 200, map[string]string{"coverHash": hash(string(encoded))})
}

func (s *Server) removePlaylistCover(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	var previous []*int64
	_, ok = s.ownPlaylist(w, r, u.User.ID, func(tx pgx.Tx, id string) (bool, error) {
		var e error
		previous, e = setPlaylistCover(r, tx, id, nil, nil)
		return true, e
	})
	if !ok {
		return
	}
	s.releaseCovers(r.Context(), previous)
	respond(w, 200, map[string]string{"coverHash": ""})
}
