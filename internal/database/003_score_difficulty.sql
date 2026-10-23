-- Requirements R020: a score records the chart block that was really played,
-- by its chart_difficulties row. The chart, the base course and the side all
-- follow from that row, so the separate columns go.
--
-- A two-player 2P score on a course that only has a single chart points at
-- the single row: what was played, not where the player stood.
DROP INDEX public.scores_leaderboard_current;
ALTER TABLE public.scores DROP COLUMN song_id, DROP COLUMN difficulty, DROP COLUMN side;
-- No default: every score names its block. The table is empty at this point.
ALTER TABLE public.scores ADD COLUMN difficulty_id int8 NOT NULL REFERENCES public.chart_difficulties(id) ON DELETE NO ACTION;
CREATE INDEX scores_leaderboard_current ON public.scores (difficulty_id, score DESC, submitted_at);
CREATE INDEX scores_user ON public.scores (user_id, submitted_at DESC);

INSERT INTO internal.schema_migrations(version) VALUES (3);
