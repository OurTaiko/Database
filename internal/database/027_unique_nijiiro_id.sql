-- Migration 027: a Nijiiro ID names one chart (R062), so /charts/{n}?by=nijiiro has one answer.
-- Deleting a chart deletes its row, so every row counts.
DROP INDEX public.charts_nijiiro_id;
CREATE UNIQUE INDEX charts_nijiiro_id ON public.charts (nijiiro_id) WHERE nijiiro_id IS NOT NULL;

INSERT INTO internal.schema_migrations(version) VALUES (27);
