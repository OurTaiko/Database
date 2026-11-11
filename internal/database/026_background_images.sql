-- Migration 026: a chart may reference a background image (BGIMAGE), stored as WebP like its
-- cover, as Fanmade's 058. Replaced images are released by the code (releaseFiles), as the
-- other files are; a changed file record refreshes the charts that use it.
ALTER TABLE public.charts
  ADD COLUMN bgimage_file int8 REFERENCES public.chart_resources(id) ON DELETE RESTRICT;
CREATE INDEX charts_bgimage_file ON public.charts (bgimage_file);

CREATE OR REPLACE FUNCTION public.chart_resources_touch_charts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW IS DISTINCT FROM OLD THEN
    UPDATE public.charts SET update_at = greatest(update_at, clock_timestamp())
     WHERE NEW.id IN (tja_file, audio_file, cover_file, movie_file, bgimage_file);
  END IF;
  RETURN NULL;
END;
$$;

INSERT INTO internal.schema_migrations(version) VALUES (26);
