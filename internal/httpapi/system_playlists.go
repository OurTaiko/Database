package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// systemPlaylist is a playlist the server computes on request. Its ID is a
// word, which no stored playlist ID (a decimal number) can equal. There is no
// voting here, so of the Fanmade site's four only the two that need no votes
// exist (R035).
type systemPlaylist struct {
	id, name, description string
	category              CategoryFlags
	// query selects chart IDs from charts c in playlist order.
	query string
}

// Only charts of the game catalog are listed, so the website and the game show
// the same charts.
const systemChart = publishedChart + ` AND c.category_flags<>0`

// Names are English like the category titles; the website translates them by ID.
var systemPlaylists = []systemPlaylist{
	{"popular", "Popular This Week", "The 20 most played charts of the last 7 days.", CategoryGame,
		`SELECT c.id FROM charts c JOIN (SELECT d.chart_id,count(*) AS plays FROM scores sc JOIN chart_difficulties d ON d.id=sc.difficulty_id
 WHERE sc.submitted_at>now()-interval '7 days' GROUP BY d.chart_id) played ON played.chart_id=c.id
 WHERE ` + systemChart + ` ORDER BY played.plays DESC,c.created_at DESC,c.id LIMIT 20`},
	// The date seeds the draw, so everyone gets the same ten charts until midnight in China.
	{"daily", "Daily Challenge", "Ten charts drawn at random every day.", CategoryVariety,
		`SELECT c.id FROM charts c WHERE ` + systemChart + `
 ORDER BY md5(c.id::text||to_char(now() AT TIME ZONE 'Asia/Shanghai','YYYYMMDD')),c.id LIMIT 10`},
}

func systemPlaylistByID(id string) *systemPlaylist {
	for i := range systemPlaylists {
		if systemPlaylists[i].id == id {
			return &systemPlaylists[i]
		}
	}
	return nil
}

func (p *systemPlaylist) chartIDs(ctx context.Context, db playlistQuerier) ([]int64, error) {
	rows, e := db.Query(ctx, p.query)
	if e != nil {
		return nil, e
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (p *systemPlaylist) playlist(charts int) Playlist {
	category := categoryByFlag(p.category)
	return Playlist{ID: p.id, Name: p.name, Description: p.description, Public: true, System: true,
		CategoryID: category.ID, Genre: category.Genre, ChartCount: charts}
}

// listSystemPlaylists returns the system playlists that currently have charts.
func (s *Server) listSystemPlaylists(w http.ResponseWriter, r *http.Request) {
	items := []Playlist{}
	for i := range systemPlaylists {
		p := &systemPlaylists[i]
		ids, e := p.chartIDs(r.Context(), s.DB)
		if e != nil {
			internal(w, e)
			return
		}
		if len(ids) > 0 {
			items = append(items, p.playlist(len(ids)))
		}
	}
	respond(w, 200, map[string]any{"items": items})
}
