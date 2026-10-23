package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
)

type Category struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Genre string `json:"genre"`
	// Published charts in the category; a chart may be in several.
	ChartCount int `json:"chartCount"`
}

// CategoryFlags are persisted in charts.category_flags as one byte. Never
// renumber or reuse a bit.
type CategoryFlags int16

const (
	CategoryNamco    CategoryFlags = 0x01
	CategoryPop      CategoryFlags = 0x02
	CategoryGame     CategoryFlags = 0x04
	CategoryAnime    CategoryFlags = 0x08
	CategoryVocaloid CategoryFlags = 0x10
	CategoryVariety  CategoryFlags = 0x20
	CategoryKids     CategoryFlags = 0x40
	CategoryClassics CategoryFlags = 0x80
)

// Catalog order is the order of the bits. Genre is the game's box.def #GENRE.
var categoryCatalog = [...]struct {
	Flag CategoryFlags
	Category
}{
	{CategoryNamco, Category{ID: "namco", Title: "Namco", Genre: "ナムコオリジナル"}},
	{CategoryPop, Category{ID: "pop", Title: "Pop", Genre: "J-POP"}},
	{CategoryGame, Category{ID: "game", Title: "Game", Genre: "ゲームミュージック"}},
	{CategoryAnime, Category{ID: "anime", Title: "Anime", Genre: "アニメ"}},
	{CategoryVocaloid, Category{ID: "vocaloid", Title: "Vocaloid", Genre: "VOCALOID"}},
	{CategoryVariety, Category{ID: "variety", Title: "Variety", Genre: "バラエティー"}},
	{CategoryKids, Category{ID: "kids", Title: "Kids", Genre: "どうよう"}},
	{CategoryClassics, Category{ID: "classics", Title: "Classics", Genre: "クラシック"}},
}

func categoryFlag(id string) (CategoryFlags, bool) {
	for _, category := range categoryCatalog {
		if category.ID == id {
			return category.Flag, true
		}
	}
	return 0, false
}

func encodeCategories(ids []string) (CategoryFlags, error) {
	var flags CategoryFlags
	for _, id := range ids {
		flag, ok := categoryFlag(id)
		if !ok {
			return 0, errors.New("invalid category ID")
		}
		flags |= flag
	}
	return flags, nil
}

// IDs lists the set categories in catalog (bit) order.
func (flags CategoryFlags) IDs() []string {
	ids := []string{}
	for _, category := range categoryCatalog {
		if flags&category.Flag != 0 {
			ids = append(ids, category.ID)
		}
	}
	return ids
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	counts := make([]int, len(categoryCatalog))
	rows, err := s.DB.Query(r.Context(), `SELECT c.category_flags,count(*) FROM charts c WHERE `+publishedChart+` GROUP BY 1`)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var flags CategoryFlags
		var n int
		if err = rows.Scan(&flags, &n); err != nil {
			internal(w, err)
			return
		}
		total += n
		for i, category := range categoryCatalog {
			if flags&category.Flag != 0 {
				counts[i] += n
			}
		}
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	items := make([]Category, 0, len(categoryCatalog))
	for i, category := range categoryCatalog {
		category.ChartCount = counts[i]
		items = append(items, category.Category)
	}
	respond(w, 200, map[string]any{"items": items, "chartCount": total})
}

// categorySelection reads a JSON array of category IDs. Omitted, null or empty
// means unclassified (category_flags 0).
func categorySelection(raw json.RawMessage) ([]string, error) {
	var ids []string
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ids); err != nil {
			return nil, err
		}
	}
	sort.Strings(ids)
	unique := ids[:0]
	for _, id := range ids {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	if _, err := encodeCategories(unique); err != nil {
		return nil, err
	}
	return unique, nil
}
