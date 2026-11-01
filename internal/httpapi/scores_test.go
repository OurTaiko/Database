package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// R056: adlib ([良, 可] of the Ad-Libs hit) and mines are optional: stored when sent, zero when
// not, and a zero value digests as if it were left out, so retries of older requests still match.
func TestScorePlayCounts(t *testing.T) {
	w := interactionFixture(t)
	c, s := w.c, w.s
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET status='published' WHERE id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	var oni int64
	if err := s.DB.QueryRow(ctx, `INSERT INTO chart_difficulties(chart_id,course,side,level) VALUES($1,'Oni','single',5) RETURNING id`, w.song).Scan(&oni); err != nil {
		t.Fatal(err)
	}
	// The game signs in with a game token; a local server without an account center has one for DEV_USER_ID.
	game := New(s.DB, Config{Storage: t.TempDir(), Origin: "http://localhost", DevUserID: testUsers["alice"].ID})
	handler, token := game.Handler(), game.devGameToken()
	n := 0
	send := func(method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		n++
		r.RemoteAddr = fmt.Sprintf("10.9.0.%d:1234", n)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	post := func(key, extra string) *httptest.ResponseRecorder {
		t.Helper()
		return send("POST", "/api/v1/game/scores", key, fmt.Sprintf(`{"difficultyId":%d,"good":5,"ok":1,"bad":0,"score":6000,"drumroll":2,"max_combo":6%s}`, oni, extra))
	}
	read := func(rec *httptest.ResponseRecorder) Score {
		t.Helper()
		var v Score
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err, rec.Body.String())
		}
		return v
	}

	if rec := post("play-counts-plain-0001", ""); rec.Code != 201 || read(rec).AdLib != [2]int64{0, 0} || read(rec).Mines != 0 || !strings.Contains(rec.Body.String(), `"adlib":[0,0]`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := post("play-counts-sent-00001", `,"adlib":[3,1],"mines":2`); rec.Code != 201 || read(rec).AdLib != [2]int64{3, 1} || read(rec).Mines != 2 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// The same request again is the stored score; another count under that key conflicts.
	if rec := post("play-counts-sent-00001", `,"adlib":[3,1],"mines":2`); rec.Code != 200 || read(rec).Mines != 2 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := post("play-counts-sent-00001", `,"adlib":[3,1],"mines":1`); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// Zeros are the same request as none.
	if rec := post("play-counts-plain-0001", `,"adlib":[0,0],"mines":0`); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	for _, extra := range []string{`,"adlib":[1]`, `,"adlib":[1,2,3]`, `,"adlib":[-1,0]`, `,"mines":-1`, `,"adlib":[1,2147483648]`} {
		if rec := post("play-counts-bad-000001", extra); rec.Code != 422 {
			t.Fatal(extra, rec.Code, rec.Body.String())
		}
	}
	if rec := post("play-counts-bad-000001", `,"adlib":"3,1"`); rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// The player's list and the leaderboard carry them too.
	var list struct{ Scores []Score }
	rec := send("GET", "/api/v1/game/scores", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	if code := rec.Code; code != 200 || len(list.Scores) != 2 || list.Scores[1].AdLib != [2]int64{3, 1} || list.Scores[1].Mines != 2 {
		t.Fatal(code, list)
	}
	var board leaderboardResponse
	if code := c.do("", "GET", w.chart(w.song)+"/leaderboard", "", &board); code != 200 || len(board.Items) != 1 || !strings.Contains(fmt.Sprint(board.Items[0].Score), "[0 0]") {
		t.Fatal(code, board)
	}
}
