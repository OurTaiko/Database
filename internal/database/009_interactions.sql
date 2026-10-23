-- Migration 009: votes, comments and notifications come into use, as on Fanmade
-- (034 for songs, 044 for 段位 courses). 001 created the tables; this adds the
-- triggers that keep the denormalized counters exact (also when a chart's
-- deletion cascades through its comments and votes), the comment rankings,
-- the course tallies, and the indexes the endpoints read by.
CREATE INDEX chart_stats_score ON chart_stats((upvotes-downvotes) DESC);
CREATE INDEX chart_votes_user ON chart_votes(user_id);
CREATE INDEX comments_root ON comments(root_id);
CREATE INDEX comment_votes_user ON comment_votes(user_id);
CREATE INDEX notifications_unread ON notifications(recipient_id) WHERE read_at IS NULL;
CREATE INDEX notifications_comment ON notifications(comment_id) WHERE comment_id IS NOT NULL;
CREATE INDEX notifications_chart ON notifications(chart_id) WHERE chart_id IS NOT NULL;

CREATE FUNCTION count_chart_vote() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE song bigint; up integer:=0; down integer:=0;
BEGIN
 IF TG_OP IN ('UPDATE','DELETE') THEN
  song:=OLD.chart_id;
  IF OLD.value=1 THEN up:=up-1; ELSE down:=down-1; END IF;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  song:=NEW.chart_id;
  IF NEW.value=1 THEN up:=up+1; ELSE down:=down+1; END IF;
 END IF;
 -- A cascading chart deletion has already removed the chart row.
 IF EXISTS(SELECT 1 FROM charts WHERE id=song) THEN
  -- CHECK applies to the proposed row before ON CONFLICT, so never propose a
  -- negative count; a decrement always finds the existing row.
  INSERT INTO chart_stats(chart_id,upvotes,downvotes) VALUES(song,greatest(up,0),greatest(down,0))
  ON CONFLICT(chart_id) DO UPDATE SET upvotes=chart_stats.upvotes+up,downvotes=chart_stats.downvotes+down;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER chart_votes_count AFTER INSERT OR UPDATE OF value OR DELETE ON chart_votes FOR EACH ROW EXECUTE FUNCTION count_chart_vote();

CREATE FUNCTION count_comment_vote() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE target text; up integer:=0; down integer:=0;
BEGIN
 IF TG_OP IN ('UPDATE','DELETE') THEN
  target:=OLD.comment_id;
  IF OLD.value=1 THEN up:=up-1; ELSE down:=down-1; END IF;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  target:=NEW.comment_id;
  IF NEW.value=1 THEN up:=up+1; ELSE down:=down+1; END IF;
 END IF;
 UPDATE comments SET upvotes=upvotes+up,downvotes=downvotes+down WHERE id=target;
 RETURN NULL;
END;
$$;
CREATE TRIGGER comment_votes_count AFTER INSERT OR UPDATE OF value OR DELETE ON comment_votes FOR EACH ROW EXECUTE FUNCTION count_comment_vote();

-- Comment ranking as on Reddit. "best" is the lower bound of the Wilson score
-- interval at 80% confidence, so a few early votes cannot outrank many.
CREATE FUNCTION comment_confidence(up integer,down integer) RETURNS double precision
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN up+down=0 THEN 0 ELSE
  (up::float8/(up+down) + 1.642374415149/(2*(up+down))
   - 1.281551565545*sqrt((up::float8/(up+down)*(1-up::float8/(up+down)) + 1.642374415149/(4*(up+down)))/(up+down)))
  / (1 + 1.642374415149/(up+down)) END
$$;
-- "controversial": many votes, split close to evenly.
CREATE FUNCTION comment_controversy(up integer,down integer) RETURNS double precision
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN up<=0 OR down<=0 THEN 0
  ELSE power((up+down)::float8, CASE WHEN up>down THEN down::float8/up ELSE up::float8/down END) END
$$;

-- 段位 courses take votes and comments as charts do and keep their own tallies.
ALTER TABLE dan_courses
 ADD COLUMN upvotes integer NOT NULL DEFAULT 0 CHECK(upvotes>=0),
 ADD COLUMN downvotes integer NOT NULL DEFAULT 0 CHECK(downvotes>=0),
 ADD COLUMN comment_count integer NOT NULL DEFAULT 0 CHECK(comment_count>=0);
CREATE INDEX dan_courses_score ON dan_courses((upvotes-downvotes) DESC) WHERE status='published';

CREATE TABLE dan_votes (
 dan_id bigint NOT NULL REFERENCES dan_courses(id) ON DELETE CASCADE,
 user_id text NOT NULL REFERENCES users(id),
 value smallint NOT NULL CHECK(value IN (-1,1)),
 voted_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(dan_id,user_id)
);
CREATE INDEX dan_votes_user ON dan_votes(user_id);

CREATE FUNCTION count_dan_vote() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE target bigint; up integer:=0; down integer:=0;
BEGIN
 IF TG_OP IN ('UPDATE','DELETE') THEN
  target:=OLD.dan_id;
  IF OLD.value=1 THEN up:=up-1; ELSE down:=down-1; END IF;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  target:=NEW.dan_id;
  IF NEW.value=1 THEN up:=up+1; ELSE down:=down+1; END IF;
 END IF;
 UPDATE dan_courses SET upvotes=upvotes+up,downvotes=downvotes+down WHERE id=target;
 RETURN NULL;
END;
$$;
CREATE TRIGGER dan_votes_count AFTER INSERT OR UPDATE OF value OR DELETE ON dan_votes FOR EACH ROW EXECUTE FUNCTION count_dan_vote();

-- A comment hangs on either a chart or a course.
ALTER TABLE comments
 ALTER COLUMN chart_id DROP NOT NULL,
 ADD COLUMN dan_id bigint REFERENCES dan_courses(id) ON DELETE CASCADE,
 ADD CHECK((chart_id IS NULL)<>(dan_id IS NULL));
CREATE INDEX comments_dan_roots ON comments(dan_id,created_at DESC) WHERE parent_id IS NULL AND dan_id IS NOT NULL;

CREATE FUNCTION count_comment() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE delta integer; rec comments;
BEGIN
 IF TG_OP='INSERT' THEN delta:=1; rec:=NEW; ELSE delta:=-1; rec:=OLD; END IF;
 IF rec.parent_id IS NOT NULL THEN
  UPDATE comments SET reply_count=reply_count+delta WHERE id=rec.parent_id;
 END IF;
 IF rec.dan_id IS NOT NULL THEN
  UPDATE dan_courses SET comment_count=comment_count+delta WHERE id=rec.dan_id;
 ELSIF EXISTS(SELECT 1 FROM charts WHERE id=rec.chart_id) THEN
  INSERT INTO chart_stats(chart_id,comment_count) VALUES(rec.chart_id,greatest(delta,0))
  ON CONFLICT(chart_id) DO UPDATE SET comment_count=chart_stats.comment_count+delta;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER comments_count AFTER INSERT OR DELETE ON comments FOR EACH ROW EXECUTE FUNCTION count_comment();

ALTER TABLE notifications ADD COLUMN dan_id bigint REFERENCES dan_courses(id) ON DELETE CASCADE;

INSERT INTO internal.schema_migrations(version) VALUES (9);
