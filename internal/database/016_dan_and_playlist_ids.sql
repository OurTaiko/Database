-- 段位 courses and playlists get IDs like charts (R051), 32 random lowercase hex digits, instead
-- of counting up; Fanmade's courses do the same (050). Votes and comments are not counted again
-- and the tallies stay: only the ID columns change.
CREATE TEMP TABLE dan_id_map ON COMMIT DROP AS
SELECT id::text AS old_id, replace(gen_random_uuid()::text, '-', '') AS new_id FROM dan_courses;
CREATE UNIQUE INDEX ON dan_id_map (old_id);

ALTER TABLE dan_course_songs DROP CONSTRAINT dan_course_songs_course_id_fkey;
ALTER TABLE dan_attempts DROP CONSTRAINT dan_attempts_course_id_fkey;
ALTER TABLE dan_votes DROP CONSTRAINT dan_votes_dan_id_fkey;
ALTER TABLE comments DROP CONSTRAINT comments_dan_id_fkey;
ALTER TABLE notifications DROP CONSTRAINT notifications_dan_id_fkey;

ALTER TABLE dan_courses ALTER COLUMN id DROP IDENTITY;
ALTER TABLE dan_courses ALTER COLUMN id TYPE text USING id::text;
ALTER TABLE dan_course_songs ALTER COLUMN course_id TYPE text USING course_id::text;
ALTER TABLE dan_attempts ALTER COLUMN course_id TYPE text USING course_id::text;
ALTER TABLE dan_votes ALTER COLUMN dan_id TYPE text USING dan_id::text;
ALTER TABLE comments ALTER COLUMN dan_id TYPE text USING dan_id::text;
ALTER TABLE notifications ALTER COLUMN dan_id TYPE text USING dan_id::text;

UPDATE dan_courses t SET id = m.new_id FROM dan_id_map m WHERE m.old_id = t.id;
UPDATE dan_course_songs t SET course_id = m.new_id FROM dan_id_map m WHERE m.old_id = t.course_id;
UPDATE dan_attempts t SET course_id = m.new_id FROM dan_id_map m WHERE m.old_id = t.course_id;
UPDATE dan_votes t SET dan_id = m.new_id FROM dan_id_map m WHERE m.old_id = t.dan_id;
UPDATE comments t SET dan_id = m.new_id FROM dan_id_map m WHERE m.old_id = t.dan_id;
UPDATE notifications t SET dan_id = m.new_id FROM dan_id_map m WHERE m.old_id = t.dan_id;
-- A course's upvote milestones are kept once each by "<kind>:dan<course ID>:<count>".
UPDATE notifications t SET dedupe_key = split_part(t.dedupe_key, ':', 1) || ':dan' || m.new_id || ':' || split_part(t.dedupe_key, ':', 3)
  FROM dan_id_map m
 WHERE t.dan_id = m.new_id AND t.comment_id IS NULL AND split_part(t.dedupe_key, ':', 2) = 'dan' || m.old_id;

ALTER TABLE dan_courses ADD CONSTRAINT dan_courses_id_format CHECK (id ~ '^[0-9a-f]{32}$');
ALTER TABLE dan_course_songs ADD CONSTRAINT dan_course_songs_course_id_fkey FOREIGN KEY (course_id) REFERENCES dan_courses(id) ON DELETE CASCADE;
ALTER TABLE dan_attempts ADD CONSTRAINT dan_attempts_course_id_fkey FOREIGN KEY (course_id) REFERENCES dan_courses(id);
ALTER TABLE dan_votes ADD CONSTRAINT dan_votes_dan_id_fkey FOREIGN KEY (dan_id) REFERENCES dan_courses(id) ON DELETE CASCADE;
ALTER TABLE comments ADD CONSTRAINT comments_dan_id_fkey FOREIGN KEY (dan_id) REFERENCES dan_courses(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD CONSTRAINT notifications_dan_id_fkey FOREIGN KEY (dan_id) REFERENCES dan_courses(id) ON DELETE CASCADE;

CREATE OR REPLACE FUNCTION count_dan_vote() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE target text; up integer:=0; down integer:=0;
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

CREATE TEMP TABLE playlist_id_map ON COMMIT DROP AS
SELECT id::text AS old_id, replace(gen_random_uuid()::text, '-', '') AS new_id FROM playlists;
CREATE UNIQUE INDEX ON playlist_id_map (old_id);
ALTER TABLE playlist_items DROP CONSTRAINT playlist_items_playlist_id_fkey;
ALTER TABLE playlist_favorites DROP CONSTRAINT playlist_favorites_playlist_id_fkey;
ALTER TABLE playlists ALTER COLUMN id DROP IDENTITY;
ALTER TABLE playlists ALTER COLUMN id TYPE text USING id::text;
ALTER TABLE playlist_items ALTER COLUMN playlist_id TYPE text USING playlist_id::text;
ALTER TABLE playlist_favorites ALTER COLUMN playlist_id TYPE text USING playlist_id::text;
UPDATE playlists t SET id = m.new_id FROM playlist_id_map m WHERE m.old_id = t.id;
UPDATE playlist_items t SET playlist_id = m.new_id FROM playlist_id_map m WHERE m.old_id = t.playlist_id;
UPDATE playlist_favorites t SET playlist_id = m.new_id FROM playlist_id_map m WHERE m.old_id = t.playlist_id;
ALTER TABLE playlists ADD CONSTRAINT playlists_id_format CHECK (id ~ '^[0-9a-f]{32}$');
ALTER TABLE playlist_items ADD CONSTRAINT playlist_items_playlist_id_fkey FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE;
ALTER TABLE playlist_favorites ADD CONSTRAINT playlist_favorites_playlist_id_fkey FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE;

INSERT INTO internal.schema_migrations(version) VALUES (16);
