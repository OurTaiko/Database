package httpapi

import (
	"context"
	"testing"
)

// Liked Songs and Recently Acclaimed, as on Fanmade: Liked Songs is the viewer's own and
// does not exist for a guest; Recently Acclaimed lists recent charts voted above zero.
func TestSystemPlaylists(t *testing.T) {
	w := interactionFixture(t)
	c, ctx := w.c, context.Background()
	// System playlists list charts of the game catalog only: with a TJA and a category.
	var tja, audio int64
	digest := ID() + ID()
	err := w.s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1||'.tja','a.tja',$1,1,'tja') RETURNING id`, digest).Scan(&tja)
	if err == nil {
		err = w.s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1||'.ogg','a.ogg',$1,1,'ogg') RETURNING id`, digest).Scan(&audio)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.DB.Exec(ctx, `UPDATE charts SET tja_file=$1,audio_file=$2,duration=10,category_flags=1`, tja, audio); err != nil {
		t.Fatal(err)
	}
	if code := c.do("alice", "PUT", w.chart(w.song)+"/vote", `{"value":1}`, nil); code != 200 {
		t.Fatal("vote", code)
	}
	charts := func(user, id string) []string {
		t.Helper()
		var p Playlist
		if code := c.do(user, "GET", "/api/v1/playlists/"+id, "", &p); code != 200 || !p.System {
			t.Fatal(id, user, code)
		}
		ids := []string{}
		for _, chart := range p.Charts {
			ids = append(ids, chart.ID)
		}
		return ids
	}
	if code := c.do("", "GET", "/api/v1/playlists/liked", "", nil); code != 401 {
		t.Fatal("guest Liked Songs", code)
	}
	if got := charts("alice", "liked"); len(got) != 1 || got[0] != w.song {
		t.Fatal("alice's Liked Songs", got)
	}
	if got := charts("bob", "liked"); len(got) != 0 {
		t.Fatal("bob's Liked Songs", got)
	}
	if got := charts("", "acclaimed"); len(got) != 1 || got[0] != w.song {
		t.Fatal("Recently Acclaimed", got)
	}
	listed := func(user string) map[string]bool {
		t.Helper()
		var list struct{ Items []Playlist }
		if code := c.do(user, "GET", "/api/v1/playlists/system", "", &list); code != 200 {
			t.Fatal(code)
		}
		ids := map[string]bool{}
		for _, p := range list.Items {
			ids[p.ID] = true
		}
		return ids
	}
	if guest := listed(""); guest["liked"] || !guest["acclaimed"] || !guest["daily"] {
		t.Fatal("guest's system playlists", guest)
	}
	if alice := listed("alice"); !alice["liked"] || !alice["acclaimed"] {
		t.Fatal("alice's system playlists", alice)
	}
}
