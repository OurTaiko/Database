-- Migration 014: background movies and uploads processed after the request, as Fanmade's 039
-- and 040. A chart may reference a background movie (BGMOVIE) stored like its other files,
-- with MOVIEOFFSET in seconds. An upload is accepted before its files are decoded and stored:
-- its receipt exists from acceptance, and title lets a failure be reported after a restart.
ALTER TABLE public.charts
  ADD COLUMN movie_file int8 REFERENCES public.chart_resources(id) ON DELETE RESTRICT,
  ADD COLUMN movie_offset float8 NOT NULL DEFAULT 0 CHECK (movie_offset BETWEEN -1200 AND 1200);
CREATE INDEX charts_movie_file ON public.charts (movie_file);

ALTER TABLE internal.upload_requests
  ADD COLUMN status text NOT NULL DEFAULT 'done' CHECK (status IN ('processing', 'done')),
  ADD COLUMN title text NOT NULL DEFAULT '';

-- A changed movie file record refreshes the charts that use it, as the other files do.
CREATE OR REPLACE FUNCTION public.chart_resources_touch_charts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW IS DISTINCT FROM OLD THEN
    UPDATE public.charts SET update_at = greatest(update_at, clock_timestamp())
     WHERE NEW.id IN (tja_file, audio_file, cover_file, movie_file);
  END IF;
  RETURN NULL;
END;
$$;

INSERT INTO internal.schema_migrations(version) VALUES (14);
