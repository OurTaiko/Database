package httpapi

import "testing"

// The game's catalog names the board its folder for this site wears (the orange 4).
func TestGameCatalogNamesItsGenreFrame(t *testing.T) {
	w := interactionFixture(t)
	var body struct {
		GenreFrame *int `json:"genreFrame"`
	}
	if code := w.c.do("", "GET", "/api/v1/game/catalog", "", &body); code != 200 {
		t.Fatal("catalog", code)
	}
	if body.GenreFrame == nil || *body.GenreFrame != 4 {
		t.Fatal("genreFrame", body.GenreFrame)
	}
}
