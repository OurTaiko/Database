package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Up and down votes on charts, 段位 courses and comments, as on Fanmade.
type voteRequest struct {
	Value *int `json:"value"`
}

// VoteResult is the target's tally after the viewer's vote was applied.
type VoteResult struct {
	Score int `json:"score"`
	// Only charts and courses publish their separate counts; comments show the score alone.
	Upvotes   *int `json:"upvotes,omitempty"`
	Downvotes *int `json:"downvotes,omitempty"`
	MyVote    int  `json:"myVote"`
}

func readVote(w http.ResponseWriter, r *http.Request) (int, bool) {
	var body voteRequest
	if !decode(w, r, &body) {
		return 0, false
	}
	if body.Value == nil || *body.Value < -1 || *body.Value > 1 {
		problem(w, 400, "VOTE_INVALID", "投票值必须为 1、0 或 -1")
		return 0, false
	}
	return *body.Value, true
}

// applyVote sets, changes or withdraws (value 0) one user's vote on a row.
func applyVote(ctx context.Context, tx pgx.Tx, table, column string, target any, user string, value int) error {
	var e error
	if value == 0 {
		_, e = tx.Exec(ctx, `DELETE FROM `+table+` WHERE `+column+`=$1 AND user_id=$2`, target, user)
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO `+table+`(`+column+`,user_id,value) VALUES($1,$2,$3)
		 ON CONFLICT(`+column+`,user_id) DO UPDATE SET value=EXCLUDED.value,voted_at=now() WHERE `+table+`.value<>EXCLUDED.value`, target, user, value)
	}
	return e
}

func (s *Server) voteChart(w http.ResponseWriter, r *http.Request) {
	s.voteSubject(w, r, chartSubject)
}

// voteDan is a chart vote on a 段位 course; the course keeps its own tallies.
func (s *Server) voteDan(w http.ResponseWriter, r *http.Request) {
	s.voteSubject(w, r, danSubject)
}

func (s *Server) voteSubject(w http.ResponseWriter, r *http.Request, load subjectLoader) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	value, ok := readVote(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// The lock also serializes the vote with a deletion.
	subject, ok := load(w, r, tx, subjectWriteLock)
	if !ok {
		return
	}
	table, tally, kind := "chart_votes", `SELECT COALESCE((SELECT upvotes FROM chart_stats WHERE chart_id=$1),0),COALESCE((SELECT downvotes FROM chart_stats WHERE chart_id=$1),0)`, "chart_upvotes"
	if subject.danID != 0 {
		table, tally, kind = "dan_votes", `SELECT upvotes,downvotes FROM dan_courses WHERE id=$1`, "dan_upvotes"
	}
	if e = applyVote(ctx, tx, table, subject.column(), subject.id(), u.User.ID, value); e != nil {
		internal(w, e)
		return
	}
	result := VoteResult{MyVote: value, Upvotes: new(int), Downvotes: new(int)}
	e = tx.QueryRow(ctx, tally, subject.id()).Scan(result.Upvotes, result.Downvotes)
	// A milestone names no voter; the owner's own vote never notifies them.
	if e == nil && value == 1 && subject.owner != u.User.ID {
		e = notifyUpvoteMilestone(ctx, tx, kind, subject.owner, Notice{ChartID: subject.chartID, DanID: subject.danID}, *result.Upvotes)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	result.Score = *result.Upvotes - *result.Downvotes
	respond(w, 200, result)
}

func (s *Server) voteComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.required(w, r, true)
	if !ok {
		return
	}
	value, ok := readVote(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback(ctx)
	c, ok := lockComment(w, r, tx, "FOR UPDATE OF m")
	if !ok {
		return
	}
	// Withdrawing a vote stays possible after a comment was deleted.
	if value != 0 && c.gone {
		problem(w, 409, "COMMENT_DELETED", "评论已删除")
		return
	}
	if e = applyVote(ctx, tx, "comment_votes", "comment_id", c.id, u.User.ID, value); e != nil {
		internal(w, e)
		return
	}
	result := VoteResult{MyVote: value}
	var upvotes int
	e = tx.QueryRow(ctx, `SELECT upvotes-downvotes,upvotes FROM comments WHERE id=$1`, c.id).Scan(&result.Score, &upvotes)
	if e == nil && value == 1 && c.authorID != u.User.ID {
		e = notifyUpvoteMilestone(ctx, tx, "comment_upvotes", c.authorID, Notice{ChartID: c.chartID, DanID: c.danID, CommentID: c.id}, upvotes)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, result)
}
