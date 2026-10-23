-- Migration 010: a chart block's identity is what it plays, not the file it came in (as
-- Fanmade 046). chart_difficulties.content_hash is TJAParser's hash of the block as played
-- ("v1:" + 64 hex, the algorithm version in the prefix); the server fills it on upload,
-- replacement and, for stored charts, a startup backfill (NULL until then). Scores stop being
-- the latest when their block's hash changes (not when the TJA file does); a changed audio
-- still retires the whole chart. 段位 songs name a side and carry a valid flag the same
-- trigger clears; tja_hash (the file's hash) goes.
ALTER TABLE public.chart_difficulties ADD COLUMN content_hash text CHECK (content_hash ~ '^v[0-9]+:[0-9a-f]{64}$');

CREATE FUNCTION public.chart_difficulties_retire_scores() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.scores SET is_latest = false WHERE is_latest AND difficulty_id = NEW.id;
  UPDATE public.dan_course_songs SET valid = false WHERE chart_id = NEW.chart_id AND course = NEW.course AND side = NEW.side;
  RETURN NULL;
END;
$$;
-- OLD NULL is the backfill filling a hash in for the first time: nothing changed.
CREATE TRIGGER chart_difficulties_retire_scores AFTER UPDATE OF content_hash ON public.chart_difficulties
  FOR EACH ROW WHEN (OLD.content_hash IS NOT NULL AND OLD.content_hash IS DISTINCT FROM NEW.content_hash)
  EXECUTE FUNCTION public.chart_difficulties_retire_scores();

CREATE FUNCTION public.chart_difficulties_invalidate_dan_songs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.dan_course_songs SET valid = false WHERE chart_id = OLD.chart_id AND course = OLD.course AND side = OLD.side;
  RETURN NULL;
END;
$$;
CREATE TRIGGER chart_difficulties_invalidate_dan_songs AFTER DELETE ON public.chart_difficulties
  FOR EACH ROW EXECUTE FUNCTION public.chart_difficulties_invalidate_dan_songs();

-- 004's whole-chart retirement now only follows the audio.
DROP TRIGGER charts_retire_scores ON public.charts;
CREATE TRIGGER charts_retire_scores AFTER UPDATE OF audio_file ON public.charts
  FOR EACH ROW WHEN (OLD.audio_file IS DISTINCT FROM NEW.audio_file)
  EXECUTE FUNCTION public.charts_retire_scores();

ALTER TABLE public.dan_course_songs
  ADD COLUMN side text NOT NULL DEFAULT 'single' CHECK (side IN ('single','p1','p2')),
  ADD COLUMN valid boolean NOT NULL DEFAULT true;
ALTER TABLE public.dan_course_songs ALTER COLUMN side DROP DEFAULT;
-- Existing songs are pinned to the single block; a course without one is unplayable until its
-- maker picks a side.
UPDATE public.dan_course_songs s SET valid = false
 WHERE NOT EXISTS (SELECT 1 FROM public.chart_difficulties d WHERE d.chart_id = s.chart_id AND d.course = s.course AND d.side = 'single');
ALTER TABLE public.dan_course_songs DROP COLUMN tja_hash;

INSERT INTO internal.schema_migrations(version) VALUES (10);
