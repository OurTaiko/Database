-- Migration 013: the game's song-select preview clip (as Fanmade's preview_file): a 15 second
-- Ogg cut from the audio at the chart's preview range, made on upload, replacement and range
-- edits and backfilled for older charts. Like the difficulty constants it is derived from what
-- the chart already says, so writing it is not a change of the chart (R010): update_at ignores
-- preview_file.
CREATE OR REPLACE FUNCTION public.charts_touch_update_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF to_jsonb(NEW) - 'update_at' - 'preview_file' IS DISTINCT FROM to_jsonb(OLD) - 'update_at' - 'preview_file' THEN
    NEW.update_at := greatest(OLD.update_at, clock_timestamp());
  ELSE
    -- Only update_at or the preview clip was written: a related row touching the chart.
    NEW.update_at := greatest(OLD.update_at, NEW.update_at);
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.chart_resources_touch_charts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW IS DISTINCT FROM OLD THEN
    UPDATE public.charts SET update_at = greatest(update_at, clock_timestamp())
     WHERE NEW.id IN (tja_file, audio_file, cover_file);
  END IF;
  RETURN NULL;
END;
$$;

INSERT INTO internal.schema_migrations(version) VALUES (13);
