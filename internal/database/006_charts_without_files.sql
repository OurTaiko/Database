-- R038: a song may be registered before its files exist, for a chart that is
-- known (its title, Nijiiro ID or official ID) but has no TJA or audio yet.
-- Such a chart has neither file, no blocks, duration 0, and a BPM only when
-- the uploader gave one; PUT /charts/{id}/files later fills it from a real
-- TJA and audio. The game catalog and the system playlists leave it out.
DO $$
DECLARE name text;
BEGIN
  FOR name IN
    SELECT conname FROM pg_constraint
     WHERE conrelid = 'public.charts'::regclass AND contype = 'c'
       AND (pg_get_constraintdef(oid) LIKE '%tja_file%' OR pg_get_constraintdef(oid) LIKE '%bpm%' OR pg_get_constraintdef(oid) LIKE '%duration%')
  LOOP
    EXECUTE format('ALTER TABLE public.charts DROP CONSTRAINT %I', name);
  END LOOP;
END;
$$;

ALTER TABLE public.charts
  ADD CONSTRAINT charts_files_check CHECK (status = 'deleted' OR (tja_file IS NULL) = (audio_file IS NULL)),
  ADD CONSTRAINT charts_bpm_check CHECK (bpm >= 0 AND bpm < 'Infinity'::float8 AND (tja_file IS NULL OR bpm > 0)),
  ADD CONSTRAINT charts_duration_check CHECK (duration >= 0 AND duration <= 1200 AND (tja_file IS NULL OR duration > 0));

INSERT INTO internal.schema_migrations(version) VALUES (6);
