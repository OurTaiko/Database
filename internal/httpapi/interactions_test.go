package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Votes, comments, notifications, leaderboards and user pages, ported from
// Fanmade's tests (charts here have numeric IDs and owner_ids as on Fanmade).
type interactionWorld struct {
	c           testClient
	s           *Server
	song, other string
}

func (w interactionWorld) chart(id string) string {
	return "/api/v1/charts/" + id
}

// interactionFixture: owner (with the upload permission) uploaded "Song", two
// days old, and "Other"; both are registered charts without files.
func interactionFixture(t *testing.T) interactionWorld {
	t.Helper()
	pool := testDB(t)
	s := New(pool, Config{Storage: t.TempDir(), Origin: "http://localhost", SSO: fakeSSO(t)})
	c := newTestClient(t, s)
	ctx := context.Background()
	w := interactionWorld{c: c, s: s}
	owner := testUsers["owner"].ID
	if _, err := pool.Exec(ctx, `UPDATE users SET permission_flags=1 WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	err := pool.QueryRow(ctx, `INSERT INTO charts(id,owner_ids,title,bpm,duration,demo_end,created_at) VALUES($2,ARRAY[$1::text],'Song',120,0,0,now()-interval '2 days') RETURNING id`, owner, ID()).Scan(&w.song)
	if err == nil {
		err = pool.QueryRow(ctx, `INSERT INTO charts(id,owner_ids,title,bpm,duration,demo_end) VALUES($2,ARRAY[$1::text],'Other',120,0,0) RETURNING id`, owner, ID()).Scan(&w.other)
	}
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// GET /charts/{id}: by=number finds the chart's number, by=id its ID; without by the form
// decides (R051).
func TestChartDetailBy(t *testing.T) {
	w := interactionFixture(t)
	var byID Chart
	if code := w.c.do("", "GET", w.chart(w.song)+"?by=id", "", &byID); code != 200 || byID.ID != w.song || byID.NumberID <= 0 {
		t.Fatal("by=id", code, byID.ID, byID.NumberID)
	}
	number := w.chart(strconv.FormatInt(byID.NumberID, 10))
	for _, path := range []string{number, number + "?by=number"} {
		var c Chart
		if code := w.c.do("", "GET", path, "", &c); code != 200 || c.ID != w.song || c.NumberID != byID.NumberID {
			t.Fatal("by number", path, code, c.ID)
		}
	}
	for path, want := range map[string]int{
		w.chart(w.song):                200, // without by, the form tells
		w.chart(w.song) + "?by=number": 404,
		number + "?by=id":              404, // a number is not an ID
		w.chart("0"):                   404,
		w.chart("01"):                  404,
		w.chart("99999999"):            404,
		w.chart(w.song) + "?by=title":  400,
		w.chart(w.song) + "?by=id&x=1": 200,
	} {
		if code := w.c.do("", "GET", path, "", nil); code != want {
			t.Fatal(path, code, "want", want)
		}
	}
	// A new chart takes the next number.
	var other Chart
	w.c.do("", "GET", w.chart(w.other)+"?by=id", "", &other)
	if other.NumberID != byID.NumberID+1 {
		t.Fatal("numbers", byID.NumberID, other.NumberID)
	}
}

func TestChartVotes(t *testing.T) {
	w := interactionFixture(t)
	c, song := w.c, w.chart(w.song)
	var v VoteResult
	if code := c.do("", "PUT", song+"/vote", `{"value":1}`, nil); code != 401 {
		t.Fatal("guest vote", code)
	}
	for _, body := range []string{`{"value":2}`, `{}`, `{"value":"1"}`} {
		if code := c.do("alice", "PUT", song+"/vote", body, nil); code != 400 {
			t.Fatal("invalid vote accepted", body, code)
		}
	}
	for _, path := range []string{"/api/v1/charts/999999/vote", "/api/v1/charts/song/vote"} {
		if code := c.do("alice", "PUT", path, `{"value":1}`, nil); code != 404 {
			t.Fatal("missing chart", path, code)
		}
	}
	c.do("alice", "PUT", song+"/vote", `{"value":1}`, &v)
	c.do("alice", "PUT", song+"/vote", `{"value":1}`, &v)
	c.do("bob", "PUT", song+"/vote", `{"value":1}`, &v)
	if v.Score != 2 || *v.Upvotes != 2 || v.MyVote != 1 {
		t.Fatalf("repeated vote counted twice: %+v", v)
	}
	c.do("bob", "PUT", song+"/vote", `{"value":-1}`, &v)
	if v.Score != 0 || *v.Upvotes != 1 || *v.Downvotes != 1 || v.MyVote != -1 {
		t.Fatalf("changed vote: %+v", v)
	}
	c.do("bob", "PUT", song+"/vote", `{"value":0}`, &v)
	if v.Score != 1 || *v.Downvotes != 0 || v.MyVote != 0 {
		t.Fatalf("withdrawn vote: %+v", v)
	}
	var chart Chart
	c.do("alice", "GET", song+"?by=id", "", &chart)
	if chart.Score != 1 || chart.MyVote != 1 || chart.Upvotes != 1 {
		t.Fatalf("detail: %+v %+v", chart.Score, chart.MyVote)
	}
	c.do("", "GET", song+"?by=id", "", &chart)
	if chart.MyVote != 0 || chart.Score != 1 {
		t.Fatal("guest detail", chart.MyVote)
	}
	// "top" puts the voted older chart first; the default order is newest first.
	for order, first := range map[string]string{"": w.other, "top": w.song, "hot": w.other, "comments": w.other} {
		var list struct{ Items []Chart }
		if code := c.do("", "GET", "/api/v1/charts?order="+order, "", &list); code != 200 || len(list.Items) != 2 || list.Items[0].ID != first {
			t.Fatal("order", order, code, list.Items)
		}
	}
}

func TestCommentThreads(t *testing.T) {
	w := interactionFixture(t)
	c, s, song := w.c, w.s, w.chart(w.song)
	ctx := context.Background()
	post := func(user, chart, body string, parent string) Comment {
		t.Helper()
		payload := map[string]any{"body": body}
		if parent != "" {
			payload["parentId"] = parent
		}
		raw, _ := json.Marshal(payload)
		var out Comment
		if code := c.do(user, "POST", chart+"/comments", string(raw), &out); code != 201 {
			t.Fatal("post", body, code)
		}
		return out
	}
	if code := c.do("", "POST", song+"/comments", `{"body":"hi"}`, nil); code != 401 {
		t.Fatal("guest comment", code)
	}
	for _, body := range []string{`{"body":"   "}`, `{"body":"a\u0000b"}`, `{"body":"` + strings.Repeat("长", 10001) + `"}`} {
		if code := c.do("alice", "POST", song+"/comments", body, nil); code != 400 {
			t.Fatal("invalid body accepted", len(body), code)
		}
	}
	long := post("alice", song, strings.Repeat("长", 10000), "")
	if long.Score != 1 || long.MyVote != 1 || long.Author != "alice" || long.ChartID != w.song {
		t.Fatalf("own comment not self-upvoted: %+v", long)
	}
	top := post("alice", song, " First\r\nline ", "")
	if top.Body != "First\nline" || top.Depth != 0 {
		t.Fatalf("body not normalized: %q", top.Body)
	}
	reply := post("bob", song, "reply", top.ID)
	deep := post("owner", song, "deeper", reply.ID)
	if deep.Depth != 2 || *deep.ParentID != reply.ID {
		t.Fatalf("nested reply: %+v", deep)
	}
	if code := c.do("bob", "POST", w.chart(w.other)+"/comments", `{"body":"x","parentId":"`+top.ID+`"}`, nil); code != 404 {
		t.Fatal("reply across charts", code)
	}
	// Bob downvotes the long comment so "best" ranks the first comment ahead.
	var v VoteResult
	c.do("bob", "PUT", "/api/v1/comments/"+long.ID+"/vote", `{"value":-1}`, &v)
	if v.Upvotes != nil {
		t.Fatal("comment vote published separate counts")
	}
	c.do("owner", "PUT", "/api/v1/comments/"+top.ID+"/vote", `{"value":1}`, &v)
	if v.Score != 2 || v.MyVote != 1 {
		t.Fatalf("comment vote: %+v", v)
	}
	var page CommentPage
	c.do("bob", "GET", song+"/comments", "", &page)
	if page.Total != 2 || page.CommentCount != 4 || len(page.Items) != 2 || page.Items[0].ID != top.ID {
		t.Fatalf("best page: %+v", page)
	}
	if got := page.Items[0]; len(got.Replies) != 1 || got.Replies[0].ID != reply.ID || len(got.Replies[0].Replies) != 1 || got.Replies[0].Replies[0].Author != "owner" || got.Replies[0].MyVote != 1 {
		t.Fatalf("tree: %+v", got.Replies)
	}
	if page.Items[1].MyVote != -1 {
		t.Fatal("viewer's downvote not reported")
	}
	for sort, first := range map[string]string{"new": top.ID, "old": long.ID, "top": top.ID, "controversial": long.ID} {
		c.do("", "GET", song+"/comments?sort="+sort, "", &page)
		if page.Items[0].ID != first {
			t.Fatal("sort", sort)
		}
	}
	if code := c.do("", "GET", song+"/comments?sort=hot", "", nil); code != 400 {
		t.Fatal("unknown sort", code)
	}
	var chart Chart
	c.do("", "GET", song+"?by=id", "", &chart)
	if chart.CommentCount != 4 {
		t.Fatal("chart comment count", chart.CommentCount)
	}

	// Permalink of the middle reply contains its subtree only.
	var thread struct{ Item Comment }
	c.do("", "GET", "/api/v1/comments/"+reply.ID, "", &thread)
	if thread.Item.ID != reply.ID || thread.Item.ChartTitle != "Song" || len(thread.Item.Replies) != 1 || thread.Item.Replies[0].ID != deep.ID {
		t.Fatalf("permalink: %+v", thread.Item)
	}

	// Editing: only the author, and the edit is marked.
	if code := c.do("alice", "PATCH", "/api/v1/comments/"+reply.ID, `{"body":"hijack"}`, nil); code != 403 {
		t.Fatal("edit by another user", code)
	}
	var edited Comment
	c.do("bob", "PATCH", "/api/v1/comments/"+reply.ID, `{"body":"reply, edited"}`, &edited)
	if edited.Body != "reply, edited" || edited.EditedAt == nil {
		t.Fatalf("edit: %+v", edited)
	}

	// Notifications: alice got bob's reply; owner got alice's top-level comments.
	var unread map[string]int
	c.do("alice", "GET", "/api/v1/me/notifications/unread", "", &unread)
	if unread["count"] != 1 {
		t.Fatal("alice unread", unread)
	}
	var notes NotificationPage
	c.do("owner", "GET", "/api/v1/me/notifications", "", &notes)
	if notes.Total != 2 || notes.Unread != 2 || notes.Items[0].Kind != "chart_comment" || notes.Items[0].Read ||
		notes.Items[0].Actor == nil || notes.Items[0].Actor.Nickname != "alice" || notes.Items[0].Chart.Title != "Song" || notes.Items[0].Comment.ID != top.ID {
		t.Fatalf("owner notifications: %+v", notes)
	}
	if code := c.do("owner", "POST", "/api/v1/me/notifications/read", `{}`, nil); code != 200 {
		t.Fatal("mark all read", code)
	}
	c.do("owner", "GET", "/api/v1/me/notifications/unread", "", &unread)
	if unread["count"] != 0 {
		t.Fatal("still unread", unread)
	}

	// Deleting a comment with replies keeps an anonymous placeholder.
	if code := c.do("alice", "DELETE", "/api/v1/comments/"+reply.ID, "", nil); code != 403 {
		t.Fatal("delete by another user", code)
	}
	c.do("bob", "DELETE", "/api/v1/comments/"+reply.ID, "", nil)
	c.do("", "GET", song+"/comments?sort=new", "", &page)
	placeholder := page.Items[0].Replies[0]
	if !placeholder.Deleted || placeholder.Body != "" || placeholder.AuthorID != "" || placeholder.Author != "" || len(placeholder.Replies) != 1 {
		t.Fatalf("placeholder: %+v", placeholder)
	}
	if code := c.do("alice", "POST", song+"/comments", `{"body":"x","parentId":"`+reply.ID+`"}`, nil); code != 409 {
		t.Fatal("reply to deleted comment", code)
	}
	if code := c.do("alice", "PUT", "/api/v1/comments/"+reply.ID+"/vote", `{"value":1}`, nil); code != 409 {
		t.Fatal("vote on deleted comment", code)
	}
	c.do("alice", "GET", "/api/v1/me/notifications/unread", "", &unread)
	if unread["count"] != 0 {
		t.Fatal("notice of a deleted reply", unread)
	}
	// An administrator removes the last reply; the empty placeholder above goes too.
	var removed map[string]bool
	c.do("admin", "DELETE", "/api/v1/comments/"+deep.ID, "", &removed)
	if !removed["purged"] {
		t.Fatal("leaf not purged")
	}
	var n int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM comments WHERE id=ANY($1)`, []string{reply.ID, deep.ID}).Scan(&n); err != nil || n != 0 {
		t.Fatal("placeholder chain kept", n, err)
	}
	c.do("", "GET", song+"/comments", "", &page)
	if page.CommentCount != 2 || page.Items[0].ReplyCount != 0 {
		t.Fatalf("counters after purge: %+v", page)
	}
	// Removal by an administrator keeps replies under a "removed" placeholder.
	post("bob", song, "under", long.ID)
	c.do("admin", "DELETE", "/api/v1/comments/"+long.ID, "", nil)
	c.do("alice", "GET", "/api/v1/me/notifications?filter=unread", "", &notes)
	if notes.Total != 2 || notes.Items[0].Kind != "comment_removed" || notes.Items[0].Actor != nil || notes.Items[0].Chart.ID != w.song || notes.Items[1].Kind != "comment_reply" {
		t.Fatalf("removal notice: %+v", notes)
	}
	c.do("", "GET", song+"/comments?sort=old", "", &page)
	if !page.Items[0].Removed || page.Items[0].Body != "" {
		t.Fatalf("removed placeholder: %+v", page.Items[0])
	}

	// History lists live comments only. Karma keeps every vote from others: the
	// owner's upvote on the live comment and bob's downvote on the removed one.
	alice := testUsers["alice"].ID
	var history CommentList
	c.do("", "GET", "/api/v1/users/"+alice+"/comments", "", &history)
	if history.Total != 1 || history.Items[0].ID != top.ID || history.Items[0].ChartTitle != "Song" {
		t.Fatalf("history: %+v", history)
	}
	var space UserSpace
	c.do("", "GET", "/api/v1/users/"+alice, "", &space)
	if space.User.CommentCount != 1 || space.User.Karma != 0 || space.User.Nickname == nil || *space.User.Nickname != "alice" {
		t.Fatalf("profile: %+v", space.User)
	}

	// Deleting the chart cascades through comments and votes.
	if code := c.do("owner", "DELETE", song, "", nil); code != 200 {
		t.Fatal("delete chart", code)
	}
	for _, table := range []string{"comments", "comment_votes", "chart_votes", "chart_stats"} {
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, "left after chart deletion", n, err)
		}
	}
}

func TestCommentBurstLimit(t *testing.T) {
	w := interactionFixture(t)
	for i := 0; i < commentBurst; i++ {
		if code := w.c.do("alice", "POST", w.chart(w.song)+"/comments", `{"body":"spam"}`, nil); code != 201 {
			t.Fatal("comment", i, code)
		}
	}
	if code := w.c.do("alice", "POST", w.chart(w.song)+"/comments", `{"body":"spam"}`, nil); code != 429 {
		t.Fatal("burst not limited", code)
	}
}

// Concurrent votes on the same chart and comment neither deadlock nor drift.
func TestConcurrentVotes(t *testing.T) {
	w := interactionFixture(t)
	c := w.c
	var comment Comment
	if code := c.do("owner", "POST", w.chart(w.song)+"/comments", `{"body":"vote here"}`, &comment); code != 201 {
		t.Fatal("comment", code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 64)
	for _, user := range []string{"owner", "alice", "bob", "admin"} {
		for _, path := range []string{w.chart(w.song) + "/vote", "/api/v1/comments/" + comment.ID + "/vote"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, value := range []string{"1", "-1", "0", "-1"} {
					codes <- c.do(user, "PUT", path, `{"value":`+value+`}`, nil)
				}
			}()
		}
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatal("concurrent vote", code)
		}
	}
	var up, down, cup, cdown int
	ctx := context.Background()
	if err := w.s.DB.QueryRow(ctx, `SELECT upvotes,downvotes FROM chart_stats WHERE chart_id=$1`, w.song).Scan(&up, &down); err != nil || up != 0 || down != 4 {
		t.Fatal("chart tally", up, down, err)
	}
	if err := w.s.DB.QueryRow(ctx, `SELECT upvotes,downvotes FROM comments WHERE id=$1`, comment.ID).Scan(&cup, &cdown); err != nil || cup != 0 || cdown != 4 {
		t.Fatal("comment tally", cup, cdown, err)
	}
}

func TestNotificationCenter(t *testing.T) {
	w := interactionFixture(t)
	c, s, song := w.c, w.s, w.chart(w.song)
	ctx := context.Background()
	var comment Comment
	c.do("alice", "POST", song+"/comments", `{"body":"hello"}`, &comment)
	var notes NotificationPage
	c.do("owner", "GET", "/api/v1/me/notifications", "", &notes)
	if len(notes.Items) != 1 {
		t.Fatal("no notice", notes)
	}
	path := "/api/v1/me/notifications/" + notes.Items[0].ID
	// Only the recipient can see or change a notice.
	if code := c.do("bob", "PATCH", path, `{"read":true}`, nil); code != 404 {
		t.Fatal("foreign mark", code)
	}
	if code := c.do("bob", "DELETE", path, "", nil); code != 404 {
		t.Fatal("foreign dismiss", code)
	}
	c.do("owner", "PATCH", path, `{"read":true}`, nil)
	c.do("owner", "GET", "/api/v1/me/notifications?filter=unread", "", &notes)
	if notes.Total != 0 || notes.Unread != 0 {
		t.Fatal("read notice listed as unread", notes)
	}
	c.do("owner", "PATCH", path, `{"read":false}`, nil)
	c.do("owner", "GET", "/api/v1/me/notifications?filter=unread", "", &notes)
	if notes.Total != 1 {
		t.Fatal("marked unread again", notes)
	}
	if code := c.do("owner", "DELETE", path, "", nil); code != 200 {
		t.Fatal("dismiss", code)
	}
	c.do("owner", "GET", "/api/v1/me/notifications", "", &notes)
	if notes.Total != 0 {
		t.Fatal("dismissed notice listed", notes)
	}

	// Muting a kind stops new notices of that kind only.
	if code := c.do("owner", "PUT", "/api/v1/me/notification-settings", `{"nope":false}`, nil); code != 400 {
		t.Fatal("unknown kind accepted", code)
	}
	var settings struct{ Items []notificationSetting }
	c.do("owner", "PUT", "/api/v1/me/notification-settings", `{"chart_comment":false}`, &settings)
	if len(settings.Items) != len(notificationKinds) || settings.Items[1].Kind != "chart_comment" || settings.Items[1].Enabled || !settings.Items[0].Enabled {
		t.Fatalf("settings: %+v", settings)
	}
	c.do("bob", "POST", song+"/comments", `{"body":"muted"}`, nil)
	c.do("owner", "GET", "/api/v1/me/notifications", "", &notes)
	if notes.Total != 0 {
		t.Fatal("muted kind delivered", notes)
	}
	c.do("owner", "PUT", "/api/v1/me/notification-settings", `{"chart_comment":true}`, &settings)
	if !settings.Items[1].Enabled {
		t.Fatal("not re-enabled")
	}

	// The fifth upvote (the author's own counts as on Reddit) notifies once,
	// even when a vote is withdrawn and cast again; it names no voter.
	if _, err := s.DB.Exec(ctx, `INSERT INTO users(id) VALUES('77777777777777777777777777777777')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO comment_votes(comment_id,user_id,value) VALUES($1,'77777777777777777777777777777777',1)`, comment.ID); err != nil {
		t.Fatal(err)
	}
	for _, vote := range []struct{ user, value string }{{"owner", "1"}, {"bob", "1"}, {"admin", "1"}, {"bob", "0"}, {"bob", "1"}} {
		if code := c.do(vote.user, "PUT", "/api/v1/comments/"+comment.ID+"/vote", `{"value":`+vote.value+`}`, nil); code != 200 {
			t.Fatal("vote", code)
		}
	}
	c.do("alice", "GET", "/api/v1/me/notifications", "", &notes)
	if notes.Total != 1 || notes.Items[0].Kind != "comment_upvotes" || notes.Items[0].Data["upvotes"] != float64(5) || notes.Items[0].Comment.Excerpt != "hello" || notes.Items[0].Actor != nil {
		t.Fatalf("milestone: %+v", notes)
	}

	// Hiding the chart hides its notices; deleting the chart removes them.
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET status='hidden' WHERE id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	var unread map[string]int
	c.do("alice", "GET", "/api/v1/me/notifications/unread", "", &unread)
	if unread["count"] != 0 {
		t.Fatal("notice of a hidden chart", unread)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE charts SET status='published' WHERE id=$1`, w.song); err != nil {
		t.Fatal(err)
	}
	c.do("owner", "DELETE", song, "", nil)
	var n int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&n); err != nil || n != 0 {
		t.Fatal("notices left after chart deletion", n, err)
	}
}

func TestNotificationPrune(t *testing.T) {
	w := interactionFixture(t)
	ctx := context.Background()
	alice := testUsers["alice"].ID
	_, err := w.s.DB.Exec(ctx, `INSERT INTO notifications(id,recipient_id,kind,created_at,read_at) VALUES
	 ('old-read',$1,'chart_upvotes',now()-interval '100 days',now()-interval '91 days'),
	 ('recent-read',$1,'chart_upvotes',now()-interval '100 days',now()-interval '10 days'),
	 ('old-unread',$1,'chart_upvotes',now()-interval '400 days',NULL),
	 ('unread',$1,'chart_upvotes',now()-interval '200 days',NULL)`, alice)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.s.PruneNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := w.s.DB.Query(ctx, `SELECT id FROM notifications ORDER BY id`)
	kept, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || strings.Join(kept, ",") != "recent-read,unread" {
		t.Fatal("pruned", kept, err)
	}
}

// A 段位 course takes votes and comments like a chart: its own tallies, the
// maker's notifications, and the comment showing the course it belongs to.
func TestDanInteractions(t *testing.T) {
	w := interactionFixture(t)
	c, s := w.c, w.s
	ctx := context.Background()
	danID := ID()
	if _, err := s.DB.Exec(ctx, `INSERT INTO dan_courses(id,owner_id,title,code,exams) VALUES($2,$1,'{"ja":"初段","en":"1st Dan"}','SHODAN','[]')`, testUsers["owner"].ID, danID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/dans/" + danID
	var v VoteResult
	if code := c.do("", "PUT", path+"/vote", `{"value":1}`, nil); code != 401 {
		t.Fatal("guest vote", code)
	}
	for _, missing := range []string{"999999", "abc"} {
		if code := c.do("alice", "PUT", "/api/v1/dans/"+missing+"/vote", `{"value":1}`, nil); code != 404 {
			t.Fatal("missing course", missing, code)
		}
	}
	c.do("alice", "PUT", path+"/vote", `{"value":1}`, &v)
	c.do("bob", "PUT", path+"/vote", `{"value":-1}`, &v)
	if v.Score != 0 || *v.Upvotes != 1 || *v.Downvotes != 1 || v.MyVote != -1 {
		t.Fatalf("tally: %+v", v)
	}
	c.do("bob", "PUT", path+"/vote", `{"value":0}`, &v)
	var course DanCourse
	c.do("alice", "GET", path, "", &course)
	if course.Score != 1 || course.Upvotes != 1 || course.MyVote != 1 || course.CommentCount != 0 {
		t.Fatalf("detail: %+v", course)
	}
	var top, reply Comment
	if code := c.do("alice", "POST", path+"/comments", `{"body":"良い段位"}`, &top); code != 201 || top.DanID != danID || top.ChartID != "" {
		t.Fatalf("top comment %d %+v", code, top)
	}
	if code := c.do("bob", "POST", path+"/comments", `{"body":"同感","parentId":"`+top.ID+`"}`, &reply); code != 201 || reply.Depth != 1 {
		t.Fatalf("reply %d %+v", code, reply)
	}
	// A chart's comment cannot be replied to from a course, nor the reverse.
	var chartTop Comment
	c.do("alice", "POST", w.chart(w.song)+"/comments", `{"body":"song"}`, &chartTop)
	if code := c.do("bob", "POST", path+"/comments", `{"body":"x","parentId":"`+chartTop.ID+`"}`, nil); code != 404 {
		t.Fatal("cross-subject reply", code)
	}
	if code := c.do("bob", "POST", w.chart(w.song)+"/comments", `{"body":"x","parentId":"`+top.ID+`"}`, nil); code != 404 {
		t.Fatal("cross-subject reply to chart", code)
	}
	var page CommentPage
	if code := c.do("", "GET", path+"/comments", "", &page); code != 200 || page.Total != 1 || page.CommentCount != 2 || len(page.Items[0].Replies) != 1 {
		t.Fatalf("page %d %+v", code, page)
	}
	var thread struct{ Item Comment }
	if code := c.do("", "GET", "/api/v1/comments/"+reply.ID, "", &thread); code != 200 || thread.Item.DanTitle["ja"] != "初段" || thread.Item.DanID != danID {
		t.Fatalf("thread %d %+v", code, thread.Item)
	}
	var inbox NotificationPage
	c.do("owner", "GET", "/api/v1/me/notifications", "", &inbox)
	if len(inbox.Items) != 2 || inbox.Items[1].Kind != "dan_comment" || inbox.Items[1].Dan == nil || inbox.Items[1].Dan.Title["ja"] != "初段" || inbox.Items[1].Chart != nil {
		t.Fatalf("owner inbox %+v", inbox.Items)
	}
	var space UserSpace
	c.do("", "GET", "/api/v1/users/"+testUsers["owner"].ID, "", &space)
	if space.User.Karma != 1 {
		t.Fatal("karma", space.User.Karma)
	}
	// A deleted course hides its comments and notices.
	if _, err := s.DB.Exec(ctx, `UPDATE dan_courses SET status='deleted' WHERE id=$1`, danID); err != nil {
		t.Fatal(err)
	}
	if code := c.do("", "GET", path+"/comments", "", nil); code != 404 {
		t.Fatal("deleted course comments", code)
	}
	c.do("owner", "GET", "/api/v1/me/notifications", "", &inbox)
	if len(inbox.Items) != 1 || inbox.Items[0].Kind != "chart_comment" {
		t.Fatalf("notices after deletion %+v", inbox.Items)
	}
}

func TestLeaderboard(t *testing.T) {
	w := interactionFixture(t)
	c, s := w.c, w.s
	ctx := context.Background()
	slot := func(chart string, course string) int64 {
		t.Helper()
		var id int64
		if err := s.DB.QueryRow(ctx, `INSERT INTO chart_difficulties(chart_id,course,side,level) VALUES($1,$2,'single',5)
		 ON CONFLICT(chart_id,course,side) DO UPDATE SET level=EXCLUDED.level RETURNING id`, chart, course).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	oni, hard, otherOni := slot(w.song, "Oni"), slot(w.song, "Hard"), slot(w.other, "Oni")
	for i := 0; i < 23; i++ {
		if _, err := s.DB.Exec(ctx, `INSERT INTO users(id) VALUES($1)`, fmt.Sprintf("%032x", i+1000)); err != nil {
			t.Fatal(err)
		}
	}
	add := func(id, user string, difficulty, points int64, date string) {
		t.Helper()
		if _, err := s.DB.Exec(ctx, `INSERT INTO scores(id,user_id,difficulty_id,good,ok,bad,score,drumroll,max_combo,payload_digest,submitted_at) VALUES($1,$2,$3,300,2,1,$4,55,300,repeat('a',64),$5::timestamptz)`, id, user, difficulty, points, date); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 23; i++ {
		points := int64(800000 - i)
		if i < 2 {
			points = 900000
		}
		add(fmt.Sprintf("score%02d", i), fmt.Sprintf("%032x", i+1000), oni, points, "2026-01-01T00:00:00Z")
	}
	// A player's lower and later plays do not rank twice.
	add("lower", fmt.Sprintf("%032x", 1000), oni, 840000, "2025-12-01T00:00:00Z")
	add("elsewhere", fmt.Sprintf("%032x", 1000), otherOni, 1300000, "2025-12-01T00:00:00Z")
	path := w.chart(w.song) + "/leaderboard"
	var v leaderboardResponse
	if code := c.do("", "GET", path, "", &v); code != 200 || v.Course != "Oni" || v.Side != "single" || v.Total != 23 || len(v.Items) != 20 {
		t.Fatalf("summary %d %+v", code, v)
	}
	if v.Items[0].ID != "score00" || v.Items[0].Rank != 1 || v.Items[1].Rank != 1 || v.Items[2].Rank != 3 || v.Items[0].Nickname != "未知用户" {
		t.Fatalf("ties: %+v", v.Items[:3])
	}
	c.do("", "GET", path+"?page=2&difficultyId="+strconv.FormatInt(oni, 10), "", &v)
	if len(v.Items) != 3 || v.Items[0].Rank != 21 {
		t.Fatalf("page 2: %+v", v)
	}
	if code := c.do("", "GET", path+"?difficultyId="+strconv.FormatInt(hard, 10), "", &v); code != 200 || v.Course != "Hard" || v.Total != 0 || v.Items == nil {
		t.Fatalf("empty block %d %+v", code, v)
	}
	for query, want := range map[string]int{"?difficultyId=" + strconv.FormatInt(otherOni, 10): 404, "?difficultyId=Oni": 400, "?page=0": 400} {
		if code := c.do("", "GET", path+query, "", nil); code != want {
			t.Fatal(query, code)
		}
	}
	// Scores of replaced files stay stored but leave the ranking.
	if _, err := s.DB.Exec(ctx, `UPDATE scores SET is_latest=false WHERE id='score00'`); err != nil {
		t.Fatal(err)
	}
	c.do("", "GET", path, "", &v)
	if v.Items[0].ID != "score01" || v.Items[1].Rank != 2 {
		t.Fatalf("retired score ranked: %+v", v.Items[:2])
	}
	if code := c.do("", "GET", "/api/v1/charts/999999/leaderboard", "", nil); code != 404 {
		t.Fatal("missing chart", code)
	}
}

func TestUserDirectory(t *testing.T) {
	w := interactionFixture(t)
	c := w.c
	var dir UserDirectory
	if code := c.do("", "GET", "/api/v1/users", "", &dir); code != 200 || dir.Total != 4 || !dir.ProfilesAvailable {
		t.Fatalf("directory %d %+v", code, dir)
	}
	for _, u := range dir.Items {
		if u.Nickname == nil {
			t.Fatal("nickname missing", u.ID)
		}
		if u.ID == testUsers["owner"].ID && u.ChartCount != 2 {
			t.Fatal("chart count", u.ChartCount)
		}
	}
	if c.do("", "GET", "/api/v1/users?q=ali", "", &dir); dir.Total != 1 || *dir.Items[0].Nickname != "alice" {
		t.Fatalf("search %+v", dir)
	}
	for path, want := range map[string]int{"/api/v1/users/ffffffffffffffffffffffffffffffff": 404, "/api/v1/users/nope": 404, "/api/v1/users?sort=karma": 400} {
		if code := c.do("", "GET", path, "", nil); code != want {
			t.Fatal(path, code)
		}
	}
}

// An administrator deleting someone's public playlist tells its owner.
func TestPlaylistRemovedNotice(t *testing.T) {
	w := interactionFixture(t)
	c := w.c
	var p struct{ ID string }
	if code := c.do("alice", "POST", "/api/v1/playlists", `{"name":"Mix","public":true}`, &p); code != 201 {
		t.Fatal("create playlist", code)
	}
	if code := c.do("admin", "DELETE", "/api/v1/playlists/"+p.ID, "", nil); code != 200 {
		t.Fatal("admin delete", code)
	}
	var notes NotificationPage
	c.do("alice", "GET", "/api/v1/me/notifications", "", &notes)
	if notes.Total != 1 || notes.Items[0].Kind != "playlist_removed" || notes.Items[0].Data["name"] != "Mix" || notes.Items[0].Actor != nil {
		t.Fatalf("notice %+v", notes)
	}
}

// Administrators may upload without the permission bit; other users need it.
func TestUploadPermission(t *testing.T) {
	w := interactionFixture(t)
	for name, want := range map[string]bool{"owner": true, "admin": true, "alice": false} {
		var me struct{ User User }
		if code := w.c.do(name, "GET", "/api/v1/me", "", &me); code != 200 || me.User.CanUpload != want {
			t.Fatalf("%s canUpload = %t (%d), want %t", name, me.User.CanUpload, code, want)
		}
		if code := w.c.do(name, "POST", "/api/v1/charts", "", nil); (code == 403) == want {
			t.Fatalf("%s upload status %d", name, code)
		}
	}
}

// Playlists have 32-hex IDs (R052) through creation, items, order, favorites and detail.
func TestPlaylistIDs(t *testing.T) {
	w := interactionFixture(t)
	c := w.c
	var p Playlist
	if code := c.do("owner", "POST", "/api/v1/playlists", `{"name":"P","public":true}`, &p); code != 201 || !validID(p.ID) {
		t.Fatal("create", code, p.ID)
	}
	path := "/api/v1/playlists/" + p.ID
	for _, chart := range []string{w.song, w.other} {
		if code := c.do("owner", "PUT", path+"/items/"+chart, "", nil); code != 201 {
			t.Fatal("add", chart, code)
		}
	}
	if code := c.do("owner", "PUT", path+"/order", `{"chartIds":["`+w.other+`","`+w.song+`"]}`, nil); code != 200 {
		t.Fatal("order", code)
	}
	var detail Playlist
	if code := c.do("", "GET", path, "", &detail); code != 200 || len(detail.Charts) != 2 || detail.Charts[0].ID != w.other {
		t.Fatal("detail", code, detail.Charts)
	}
	if code := c.do("alice", "PUT", path+"/favorite", "", nil); code != 200 {
		t.Fatal("favorite", code)
	}
	var mine MyPlaylists
	if code := c.do("alice", "GET", "/api/v1/me/playlists", "", &mine); code != 200 || len(mine.Favorites) != 1 || mine.Favorites[0].ID != p.ID {
		t.Fatal("favorites", code, mine.Favorites)
	}
	if code := c.do("owner", "PATCH", path, `{"name":"Q"}`, &detail); code != 200 || detail.ID != p.ID || detail.Name != "Q" {
		t.Fatal("edit", code, detail.ID)
	}
	for _, other := range []string{"1", "0123456789ABCDEF0123456789ABCDEF", ID()} {
		if code := c.do("", "GET", "/api/v1/playlists/"+other, "", nil); code != 404 {
			t.Fatal("missing playlist", other, code)
		}
	}
	if code := c.do("owner", "DELETE", path+"/items/"+w.song, "", nil); code != 200 {
		t.Fatal("remove", code)
	}
	if code := c.do("owner", "DELETE", path, "", nil); code != 200 {
		t.Fatal("delete", code)
	}
}
