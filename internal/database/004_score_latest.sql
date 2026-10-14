-- Requirements R022: scores survive a chart update. is_latest tells whether a
-- score was set on the chart's current files; which older version it belongs
-- to is not recorded.
ALTER TABLE public.scores ADD COLUMN is_latest bool NOT NULL DEFAULT true;

-- Replacing a chart's TJA or audio makes every score on its blocks an old
-- one. The files are content-addressed, so a changed reference is a changed
-- file; storing identical files again changes nothing.
CREATE FUNCTION public.charts_retire_scores() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.scores SET is_latest = false
   WHERE is_latest AND difficulty_id IN (SELECT id FROM public.chart_difficulties WHERE chart_id = NEW.id);
  RETURN NULL;
END;
$$;
CREATE TRIGGER charts_retire_scores AFTER UPDATE OF tja_file, audio_file ON public.charts
  FOR EACH ROW WHEN (OLD.tja_file IS DISTINCT FROM NEW.tja_file OR OLD.audio_file IS DISTINCT FROM NEW.audio_file)
  EXECUTE FUNCTION public.charts_retire_scores();

INSERT INTO internal.schema_migrations(version) VALUES (4);
