package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testReplayV1 = `{"version":1,"audio_offset_ms":-20,"visual_offset_ms":10,"inputs":[[-15.5,0],[1234.5,1]]}`
const testReplayV2 = `{"version":2,"audio_offset_ms":-20,"visual_offset_ms":10,"inputs":[[-15.5,0],[1234.5,1]],"options":{"speed":25,"inverse":true,"display":false,"random":2,"flipped":[0,3,7]}}`

// A recording of version 2 carries the play options; version 1 has none.
func TestNormalizeScoreReplayOptions(t *testing.T) {
	for _, raw := range []string{testReplayV1, testReplayV2} {
		if normalized := normalizeScoreReplay(json.RawMessage(raw)); string(normalized) != raw {
			t.Fatalf("recording changed: %s", normalized)
		}
	}
	for name, raw := range map[string]string{
		"version 2 without options": strings.Replace(testReplayV1, `"version":1`, `"version":2`, 1),
		"options on version 1":      strings.Replace(testReplayV2, `"version":2`, `"version":1`, 1),
		"version 3":                 strings.Replace(testReplayV2, `"version":2`, `"version":3`, 1),
		"speed range":               strings.Replace(testReplayV2, `"speed":25`, `"speed":0`, 1),
		"random range":              strings.Replace(testReplayV2, `"random":2`, `"random":3`, 1),
		"missing display":           strings.Replace(testReplayV2, `"display":false,`, "", 1),
		"unordered flipped":         strings.Replace(testReplayV2, `[0,3,7]`, `[3,0]`, 1),
		"unknown option":            strings.Replace(testReplayV2, `"random":2`, `"random":2,"neiro":1`, 1),
	} {
		if normalizeScoreReplay(json.RawMessage(raw)) != nil {
			t.Fatal(name, "must normalize to SQL NULL")
		}
	}
}

// Scores say whether their recording was kept, and it is read back with the play by anyone.
func TestScoreReplayIsReadBack(t *testing.T) {
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
	game := New(s.DB, Config{Storage: t.TempDir(), Origin: "http://localhost", DevUserID: testUsers["alice"].ID})
	handler, token := game.Handler(), game.devGameToken()
	post := func(key string, score int, extra string) Score {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/game/scores", strings.NewReader(fmt.Sprintf(`{"difficultyId":%d,"good":5,"ok":1,"bad":0,"score":%d,"drumroll":2,"max_combo":6%s}`, oni, score, extra)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		var v Score
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
			t.Fatal(rec.Code, rec.Body.String())
		}
		return v
	}
	kept := post("replay-read-kept-00001", 6000, `,"replay_data":`+testReplayV2)
	plain := post("replay-read-plain-0001", 5000, "")
	if !kept.HasReplay || plain.HasReplay {
		t.Fatal("receipts must say whether the recording was kept", kept.HasReplay, plain.HasReplay)
	}
	var read struct {
		Score    Score           `json:"score"`
		Nickname string          `json:"nickname"`
		Replay   json.RawMessage `json:"replay"`
	}
	var want, got any
	if code := c.do("", "GET", "/api/v1/scores/"+kept.ID+"/replay", "", &read); code != 200 || read.Score.ID != kept.ID || !read.Score.HasReplay || read.Score.Course != "Oni" ||
		json.Unmarshal(read.Replay, &got) != nil || json.Unmarshal([]byte(testReplayV2), &want) != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(code, read.Score, string(read.Replay))
	}
	if code := c.do("", "GET", "/api/v1/scores/"+plain.ID+"/replay", "", nil); code != 404 {
		t.Fatal("a play without a recording has none", code)
	}
	var board struct {
		Items []Score `json:"items"`
	}
	if code := c.do("", "GET", "/api/v1/charts/"+w.song+"/leaderboard", "", &board); code != 200 || len(board.Items) != 1 || board.Items[0].ID != kept.ID || !board.Items[0].HasReplay {
		t.Fatal(code, board)
	}
	var plays struct {
		Items []Score `json:"items"`
	}
	if code := c.do("", "GET", "/api/v1/users/"+testUsers["alice"].ID+"/scores?view=all", "", &plays); code != 200 || len(plays.Items) != 2 {
		t.Fatal(code, plays)
	}
	for _, play := range plays.Items {
		if play.HasReplay != (play.ID == kept.ID) {
			t.Fatal("the score list must mark the plays with a recording", play.ID, play.HasReplay)
		}
	}
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET status='hidden' WHERE id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	if code := c.do("", "GET", "/api/v1/scores/"+kept.ID+"/replay", "", nil); code != 404 {
		t.Fatal("an unpublished chart's recordings are not read", code)
	}
}
