-- Migration 022: every block is under hash algorithm v2, and 020's exception ends (as Fanmade
-- 054). The triggers are 010's and 012's again: a block's scores stop being the latest, and
-- its 段位 songs valid, whenever its hash changes (the next algorithm included, unless a
-- migration says otherwise as 020 did), and any update of a block is a change of the chart (R010).
DROP TRIGGER chart_difficulties_retire_scores ON public.chart_difficulties;
CREATE TRIGGER chart_difficulties_retire_scores AFTER UPDATE OF content_hash ON public.chart_difficulties
  FOR EACH ROW WHEN (OLD.content_hash IS NOT NULL AND OLD.content_hash IS DISTINCT FROM NEW.content_hash)
  EXECUTE FUNCTION public.chart_difficulties_retire_scores();

DROP TRIGGER chart_difficulties_touch_chart_update ON public.chart_difficulties;
DROP TRIGGER chart_difficulties_touch_chart ON public.chart_difficulties;
CREATE TRIGGER chart_difficulties_touch_chart AFTER INSERT OR UPDATE OR DELETE ON public.chart_difficulties
  FOR EACH ROW EXECUTE FUNCTION public.touch_related_chart();

INSERT INTO internal.schema_migrations(version) VALUES (22);
