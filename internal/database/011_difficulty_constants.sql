-- Migration 011: the raw values behind OurTaiko's difficulty constants (定数) for each block
-- (as Fanmade 047), computed by the server from the notes TJAParser plays (internal/constants,
-- a port of the constants repository's algorithms; the calibration and the final 0–15.5
-- constants stay there). A branching block stores its master route. raw_handspeed is the
-- repository's speed; raw_complex_ratio and raw_rhythm_ratio are what its final conversion
-- reads; note_count is totalNotes, the route's don and ka notes. constants_version is the
-- algorithm's version: the startup backfill fills NULL rows and recomputes older ones. All
-- nine are set together.
ALTER TABLE public.chart_difficulties
  ADD COLUMN constants_version int2 CHECK (constants_version > 0),
  ADD COLUMN raw_stamina float8,
  ADD COLUMN raw_handspeed float8,
  ADD COLUMN raw_burst float8,
  ADD COLUMN raw_complex float8,
  ADD COLUMN raw_complex_ratio float8,
  ADD COLUMN raw_rhythm float8,
  ADD COLUMN raw_rhythm_ratio float8,
  ADD COLUMN note_count int4 CHECK (note_count >= 0),
  ADD CONSTRAINT chart_difficulties_constants_complete CHECK (num_nulls(constants_version, raw_stamina, raw_handspeed,
    raw_burst, raw_complex, raw_complex_ratio, raw_rhythm, raw_rhythm_ratio, note_count) IN (0, 9));

-- R010 counts a block's change as a change of its chart. The constants are derived from the
-- stored file, not written by anyone, so filling or recomputing them (the backfill after this
-- migration, or a new algorithm version) leaves update_at alone.
DROP TRIGGER chart_difficulties_touch_chart ON public.chart_difficulties;
CREATE TRIGGER chart_difficulties_touch_chart AFTER INSERT OR DELETE ON public.chart_difficulties
  FOR EACH ROW EXECUTE FUNCTION public.touch_related_chart();
CREATE TRIGGER chart_difficulties_touch_chart_update AFTER UPDATE ON public.chart_difficulties
  FOR EACH ROW WHEN ((to_jsonb(OLD) - ARRAY['constants_version', 'raw_stamina', 'raw_handspeed', 'raw_burst', 'raw_complex',
    'raw_complex_ratio', 'raw_rhythm', 'raw_rhythm_ratio', 'note_count'])
    IS DISTINCT FROM (to_jsonb(NEW) - ARRAY['constants_version', 'raw_stamina', 'raw_handspeed', 'raw_burst', 'raw_complex',
    'raw_complex_ratio', 'raw_rhythm', 'raw_rhythm_ratio', 'note_count']))
  EXECUTE FUNCTION public.touch_related_chart();

INSERT INTO internal.schema_migrations(version) VALUES (11);
