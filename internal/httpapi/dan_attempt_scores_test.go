package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// A 段位 attempt keeps the scores of its songs (024 and 025, as Fanmade 056 and 057): the game
// sends them with it, or for an older game they are looked for by time.
func TestDanAttemptNamesItsScores(t *testing.T) {
	w := interactionFixture(t)
	s := w.s
	ctx := context.Background()
	const course = "dddddddddddddddddddddddddddddddd"
	var oni int64
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET status='published' WHERE id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, `INSERT INTO chart_difficulties(chart_id,course,side,level) VALUES($1,'Oni','single',5) RETURNING id`, w.song).Scan(&oni); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO dan_courses(id,owner_id,title,exams,code) VALUES($1,$2,'{"ja":"初段"}','[]','DAN001')`, course, testUsers["alice"].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO dan_course_songs(course_id,position,chart_id,course,side) VALUES($1,0,$2,'Oni','single')`, course, w.song); err != nil {
		t.Fatal(err)
	}
	game := New(s.DB, Config{Storage: t.TempDir(), Origin: "http://localhost", DevUserID: testUsers["alice"].ID})
	handler, token := game.Handler(), game.devGameToken()
	n := 0
	send := func(path, key, body string, status int, out any) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		n++
		r.RemoteAddr = fmt.Sprintf("10.8.0.%d:1234", n)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != status {
			t.Fatal(path, key, rec.Code, rec.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatal(err, rec.Body.String())
			}
		}
	}
	play := func(key string, points int) string {
		t.Helper()
		var score Score
		send("/api/v1/game/scores", key, fmt.Sprintf(`{"difficultyId":%d,"good":5,"ok":0,"bad":0,"score":%d,"drumroll":0,"max_combo":5}`, oni, points), 201, &score)
		return score.ID
	}
	type reply struct {
		Attempt DanAttempt
		Scores  []*Score
	}
	post := func(key, body string, status int) reply {
		t.Helper()
		var out reply
		send("/api/v1/game/dan-attempts", key, body, status, &out)
		return out
	}
	attempt := func(key, extra string, status int) string {
		t.Helper()
		out := post(key, `{"courseId":"`+course+`","verdict":1,"songsPlayed":1`+extra+`}`, status)
		if status != 200 {
			return ""
		}
		if out.Attempt.SongsPlayed != 1 || len(out.Attempt.ScoreIDs) != 1 {
			t.Fatal("attempt", out.Attempt)
		}
		if out.Attempt.ScoreIDs[0] == nil {
			return "-"
		}
		return *out.Attempt.ScoreIDs[0]
	}
	// The game sends the songs' scores with the attempt: saved first, and the attempt has their ids.
	var blockHash string
	if err := s.DB.QueryRow(ctx, `UPDATE chart_difficulties SET content_hash='v2:'||repeat('a',64) WHERE id=$1 RETURNING content_hash`, oni).Scan(&blockHash); err != nil {
		t.Fatal(err)
	}
	sent := func(points int, extra string) string {
		return fmt.Sprintf(`{"difficultyId":%d,"good":5,"ok":0,"bad":0,"score":%d,"drumroll":0,"max_combo":5%s}`, oni, points, extra)
	}
	together := `{"courseId":"` + course + `","verdict":1,"scores":[` + sent(1000, `,"chartHash":"`+blockHash+`","replay_data":`+testReplayV2) + `]}`
	made := post("dan-attempt-key-0001", together, 200)
	if len(made.Scores) != 1 || made.Scores[0] == nil || made.Scores[0].Score != 1000 || !made.Scores[0].HasReplay || len(made.Attempt.ScoreIDs) != 1 || *made.Attempt.ScoreIDs[0] != made.Scores[0].ID {
		t.Fatal("the attempt and its score", made)
	}
	first := made.Scores[0].ID
	if again := post("dan-attempt-key-0001", together, 200); *again.Attempt.ScoreIDs[0] != first || again.Scores[0].ID != first {
		t.Fatal("retry", again)
	}
	post("dan-attempt-key-0001", strings.Replace(together, `"score":1000`, `"score":1001`, 1), 409)
	post("dan-attempt-key-0002", `{"courseId":"`+course+`","verdict":1,"scores":[{"difficultyId":`+fmt.Sprint(oni)+`}]}`, 422)
	// Played on other files, or no score for the song: left out, the attempt kept.
	if out := post("dan-attempt-key-0002", `{"courseId":"`+course+`","verdict":0,"scores":[`+sent(5, `,"chartHash":"v2:stale"`)+`,null]}`, 200); len(out.Attempt.ScoreIDs) != 2 || out.Attempt.ScoreIDs[0] != nil || out.Attempt.ScoreIDs[1] != nil || out.Scores[0] != nil {
		t.Fatal("left out", out)
	}
	var count int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM scores`).Scan(&count); err != nil || count != 1 {
		t.Fatal("one score, once", err, count)
	}
	// An older game uploads the scores on their own, the attempt between them: they are looked for
	// by time, when the attempt comes and as a score follows it. Attempts are minutes apart in
	// play; the rows so far are moved back to say so.
	age := func() {
		t.Helper()
		if _, err := s.DB.Exec(ctx, `UPDATE dan_attempts SET submitted_at=submitted_at-interval '10 minutes'`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(ctx, `UPDATE scores SET submitted_at=submitted_at-interval '10 minutes'`); err != nil {
			t.Fatal(err)
		}
	}
	age()
	second := play("dan-score-key-0002", 2000)
	if got := attempt("dan-attempt-key-0003", "", 200); got != second {
		t.Fatal("found by time", got, second)
	}
	age()
	if got := attempt("dan-attempt-key-0004", "", 200); got != "-" {
		t.Fatal("the score of the attempt before is not taken again", got)
	}
	age()
	if got := attempt("dan-attempt-key-0005", "", 200); got != "-" {
		t.Fatal("before its score", got)
	}
	third := play("dan-score-key-0003", 3000)
	if got := attempt("dan-attempt-key-0005", "", 200); got != third {
		t.Fatal("the score that follows the attempt joins it", got, third)
	}
	// Looking again over the attempts whose scores came on their own (migration 025 did so over
	// those made before) finds the same.
	if _, err := s.DB.Exec(ctx, `UPDATE dan_attempts SET songs_played = dan_attempt_scores(id) WHERE right(idempotency_key,1) IN ('3','4','5')`); err != nil {
		t.Fatal(err)
	}
	var filled string
	if err := s.DB.QueryRow(ctx, `SELECT string_agg(right(idempotency_key,1)||'='||array_to_string(songs_played,',','-'),' ' ORDER BY idempotency_key) FROM dan_attempts`).Scan(&filled); err != nil ||
		filled != "1="+first+" 2=-,- 3="+second+" 4=- 5="+third {
		t.Fatal("filled in", err, filled)
	}
}
