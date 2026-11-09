package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Co-creators chosen with a new upload are invited when the chart is published, as on
// Fanmade: also when the uploader did not wait, and for a song registered without files.
func TestUploadInvitesCollaborators(t *testing.T) {
	world := interactionFixture(t)
	c, s := world.c, world.s
	ctx := context.Background()
	uploader, alice, bob := testUsers["admin"].ID, testUsers["alice"].ID, testUsers["bob"].ID
	audio, err := os.ReadFile("../../kit/audio/testdata/vorbis.ogg")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	// Sends a form as the uploader: with files a TJA of that title (and its audio when
	// new), otherwise only the fields. collaborators is left out when empty.
	send := func(method, path, key, title, collaborators string, files, async bool, extra ...string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if files {
			out, _ := form.CreateFormFile("tja", "chart.tja")
			_, _ = out.Write([]byte("TITLE:" + title + "\nBPM:120\nWAVE:vorbis.ogg\nCOURSE:Oni\nLEVEL:5\n#START\n1000,\n#END\n"))
			if method == "POST" {
				out, _ = form.CreateFormFile("audio", "vorbis.ogg")
				_, _ = out.Write(audio)
			}
		} else {
			_ = form.WriteField("title", title)
		}
		if collaborators != "" {
			_ = form.WriteField("collaboratorIds", collaborators)
		}
		for i := 0; i+1 < len(extra); i += 2 {
			_ = form.WriteField(extra[i], extra[i+1])
		}
		_ = form.Close()
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Idempotency-Key", key)
		if async {
			r.Header.Set("Prefer", "respond-async")
		}
		// A fresh client address per request keeps the per-address write limit out of the way.
		requests++
		r.RemoteAddr = fmt.Sprintf("10.9.%d.%d:1234", requests>>8&255, requests&255)
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: c.tokens["admin"]})
		w := httptest.NewRecorder()
		c.handler.ServeHTTP(w, r)
		return w
	}
	ids := func(users ...string) string {
		raw, _ := json.Marshal(users)
		return string(raw)
	}
	count := func(sql string, args ...any) (n int) {
		t.Helper()
		if err := s.DB.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return
	}
	charts := count(`SELECT count(*) FROM charts`)

	// A selection that cannot be invited rejects the upload at once; nothing is queued.
	for _, tc := range []struct {
		field string
		want  int
		code  string
	}{
		{`"` + alice + `"`, 400, "USER_INVALID"},
		{ids("invalid"), 400, "USER_INVALID"},
		{ids(alice, uploader), 409, "ALREADY_OWNER"},
		{ids(alice, strings.Repeat("f", 32)), 404, "USER_NOT_FOUND"},
	} {
		for _, files := range []bool{true, false} {
			if w := send("POST", "/api/v1/charts", ID(), "Rejected", tc.field, files, true); w.Code != tc.want || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatal(tc.field, files, w.Code, w.Body.String())
			}
		}
	}
	if count(`SELECT count(*) FROM charts`) != charts || count(`SELECT (SELECT count(*) FROM upload_requests)+(SELECT count(*) FROM chart_collaborations)`) != 0 {
		t.Fatal("rejected selection stored data")
	}

	// The web uploader does not wait: both are invited once the chart is published.
	key := ID()
	if w := send("POST", "/api/v1/charts", key, "Together", ids(alice, bob, alice), true, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var chartID string
	for deadline := time.Now().Add(20 * time.Second); chartID == "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		_ = s.DB.QueryRow(ctx, `SELECT chart_id FROM notifications WHERE recipient_id=$1 AND kind='upload_result' AND dedupe_key=$2`, uploader, "upload:"+key).Scan(&chartID)
	}
	if chartID == "" {
		t.Fatal("upload was not published")
	}
	if n := count(`SELECT count(*) FROM chart_collaborations c JOIN notifications n ON n.collaboration_id=c.id AND n.recipient_id=c.recipient_id AND n.actor_id=$2 AND n.chart_id=c.chart_id AND n.kind='chart_collaboration' AND n.read_at IS NULL
 WHERE c.chart_id=$1 AND c.inviter_id=$2 AND c.status='pending' AND c.recipient_id IN ($3,$4)`, chartID, uploader, alice, bob); n != 2 {
		t.Fatal("invitations with notices", n)
	}
	// An invitation still grants nothing by itself.
	if published, err := s.chart(ctx, chartID); err != nil || len(published.OwnerIDs) != 1 || published.OwnerIDs[0] != uploader {
		t.Fatalf("%+v %v", published.OwnerIDs, err)
	}
	// Sending the finished request again returns the chart and invites nobody twice;
	// another selection under the same key is a different request.
	if w := send("POST", "/api/v1/charts", key, "Together", ids(bob, alice), true, true); w.Code != 200 || !strings.Contains(w.Body.String(), chartID) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("POST", "/api/v1/charts", key, "Together", ids(alice), true, true); w.Code != 409 || !strings.Contains(w.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatal(w.Code, w.Body.String())
	}
	if count(`SELECT count(*) FROM chart_collaborations`) != 2 || count(`SELECT count(*) FROM charts`) != charts+1 {
		t.Fatal("retry repeated the upload")
	}
	// The recipient answers from the notice exactly as for an invitation sent from the chart page.
	var notice string
	if err = s.DB.QueryRow(ctx, `SELECT id FROM notifications WHERE recipient_id=$1 AND kind='chart_collaboration'`, alice).Scan(&notice); err != nil {
		t.Fatal(err)
	}
	if code := c.do("alice", "POST", "/api/v1/me/notifications/"+notice+"/respond", `{"accept":true}`, nil); code != 200 {
		t.Fatal(code)
	}
	if published, err := s.chart(ctx, chartID); err != nil || len(published.OwnerIDs) != 2 || !published.hasOwner(alice) {
		t.Fatalf("%+v %v", published.OwnerIDs, err)
	}

	// A replacement invites nobody: its chart already has the co-creator dialog.
	if w := send("PUT", "/api/v1/charts/"+chartID+"/files", ID(), "Replaced", ids(bob), true, false, "confirmReset", "true"); w.Code != 400 || !strings.Contains(w.Body.String(), "UPLOAD_FILES_INVALID") {
		t.Fatal(w.Code, w.Body.String())
	}

	// A song registered without files invites the same way, once however often it is sent.
	key = ID()
	var song Chart
	w := send("POST", "/api/v1/charts", key, "Song only", ids(bob), false, false)
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &song) != nil || song.ID == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = send("POST", "/api/v1/charts", key, "Song only", ids(bob), false, false); w.Code != 200 || !strings.Contains(w.Body.String(), song.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = send("POST", "/api/v1/charts", key, "Song only", "", false, false); w.Code != 409 || !strings.Contains(w.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatal(w.Code, w.Body.String())
	}
	if n := count(`SELECT count(*) FROM chart_collaborations c JOIN notifications n ON n.collaboration_id=c.id WHERE c.chart_id=$1 AND c.recipient_id=$2 AND c.status='pending'`, song.ID, bob); n != 1 {
		t.Fatal("invitation of a registered song", n)
	}

	// Invitations sent with an upload count toward the day's limit like any other.
	if _, err = s.DB.Exec(ctx, `INSERT INTO chart_collaborations(id,chart_id,inviter_id,recipient_id,status,responded_at)
 SELECT md5(g::text),$1,$2,$3,'declined',now() FROM generate_series(1,$4::int) g`, chartID, uploader, bob, invitationsPerDay-4); err != nil {
		t.Fatal(err)
	}
	for _, files := range []bool{true, false} {
		if w = send("POST", "/api/v1/charts", ID(), "Too many", ids(alice, bob), files, false); w.Code != 429 || !strings.Contains(w.Body.String(), "RATE_LIMITED") {
			t.Fatal(files, w.Code, w.Body.String())
		}
	}
	// An uploader who waits gets the chart; the last invitation of the day went out with it.
	w = send("POST", "/api/v1/charts", ID(), "Last one", ids(bob), true, false)
	var last Chart
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &last) != nil || last.ID == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	if n := count(`SELECT count(*) FROM chart_collaborations WHERE chart_id=$1 AND recipient_id=$2 AND status='pending'`, last.ID, bob); n != 1 {
		t.Fatal("invitation of a waited upload", n)
	}
	if code := c.do("admin", "POST", "/api/v1/charts/"+last.ID+"/collaborations", fmt.Sprintf(`{"userId":%q}`, alice), nil); code != 429 {
		t.Fatal(code)
	}
	// Uploads without the field publish as before.
	if w = send("POST", "/api/v1/charts", ID(), "Alone", "", true, false); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = send("POST", "/api/v1/charts", ID(), "Nobody", "[]", false, false); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if count(`SELECT count(*) FROM charts`) != charts+5 || count(`SELECT count(*) FROM chart_collaborations WHERE status='pending'`) != 3 {
		t.Fatal("unexpected charts or invitations")
	}
}
