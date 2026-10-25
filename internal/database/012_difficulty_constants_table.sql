-- Migration 012: the difficulty constants move to their own tables (as Fanmade 048;
-- internal/constants/store.go). A block's constants follow from what it plays, so
-- difficulty_constants is keyed by the block's content hash and route (unbranched, or normal,
-- expert and master): blocks that play the same share a row, and a replaced file whose block
-- plays the same keeps it. It holds the raw values (the first stage, constants_version) and the
-- eight published values (the second stage, rating.py) under a calibration.
-- constants_calibrations keeps every calibration: the 13 global minima and maxima, computed
-- from the live charts at start-up when due and weekly, and recorded only when they change. The
-- columns 011 put on chart_difficulties go, and with them 011's exception to R010: the constants
-- no longer live in chart_difficulties, so its touch trigger is the original one again.
CREATE TABLE public.constants_calibrations (
  version int4 PRIMARY KEY CHECK (version > 0),
  params jsonb NOT NULL,
  rating_version int2 NOT NULL CHECK (rating_version > 0),
  constants_version int2 NOT NULL CHECK (constants_version > 0),
  route_count int4 NOT NULL CHECK (route_count > 0),
  computed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.difficulty_constants (
  content_hash text NOT NULL CHECK (content_hash ~ '^v[0-9]+:[0-9a-f]{64}$'),
  route text NOT NULL CHECK (route IN ('unbranched', 'normal', 'expert', 'master')),
  constants_version int2 NOT NULL CHECK (constants_version > 0),
  note_count int4 NOT NULL CHECK (note_count >= 0),
  raw_stamina float8 NOT NULL,
  raw_handspeed float8 NOT NULL,
  raw_burst float8 NOT NULL,
  raw_complex float8 NOT NULL,
  raw_complex_ratio float8 NOT NULL,
  raw_rhythm float8 NOT NULL,
  raw_rhythm_ratio float8 NOT NULL,
  calibration_version int4 REFERENCES public.constants_calibrations(version),
  sub_constant_1 float8,
  main_constant float8,
  sub_constant_2 float8,
  stamina float8,
  handspeed float8,
  burst float8,
  complex float8,
  rhythm float8,
  PRIMARY KEY (content_hash, route),
  CONSTRAINT difficulty_constants_rating_complete CHECK (num_nulls(calibration_version, sub_constant_1, main_constant, sub_constant_2,
    stamina, handspeed, burst, complex, rhythm) IN (0, 9))
);
CREATE INDEX difficulty_constants_calibration ON public.difficulty_constants (calibration_version);
CREATE INDEX chart_difficulties_content_hash ON public.chart_difficulties (content_hash);

DROP TRIGGER chart_difficulties_touch_chart_update ON public.chart_difficulties;
DROP TRIGGER chart_difficulties_touch_chart ON public.chart_difficulties;
ALTER TABLE public.chart_difficulties
  DROP CONSTRAINT chart_difficulties_constants_complete,
  DROP COLUMN constants_version,
  DROP COLUMN raw_stamina,
  DROP COLUMN raw_handspeed,
  DROP COLUMN raw_burst,
  DROP COLUMN raw_complex,
  DROP COLUMN raw_complex_ratio,
  DROP COLUMN raw_rhythm,
  DROP COLUMN raw_rhythm_ratio,
  DROP COLUMN note_count;
CREATE TRIGGER chart_difficulties_touch_chart AFTER INSERT OR UPDATE OR DELETE ON public.chart_difficulties
  FOR EACH ROW EXECUTE FUNCTION public.touch_related_chart();

INSERT INTO internal.schema_migrations(version) VALUES (12);
