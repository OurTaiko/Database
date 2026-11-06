-- Migration 020: for the move to hash algorithm v2 only, a new algorithm is not a new chart
-- (as Fanmade 053). TJAParser v0.13.0 hashes by algorithm v2 (times to a thousandth of a
-- millisecond instead of cut to a whole one, under a millisecond apart): the startup backfill
-- puts every block under "v2:", and 010's trigger would take each for a block that plays
-- differently, retiring every score and 段位 song. While this stands they go only when the
-- hash changes within one version, or goes. Once every block is under "v2:" the next
-- migration puts 010's trigger back: this is an exception for this change, not the rule for
-- the next algorithm.
DROP TRIGGER chart_difficulties_retire_scores ON public.chart_difficulties;
CREATE TRIGGER chart_difficulties_retire_scores AFTER UPDATE OF content_hash ON public.chart_difficulties
  FOR EACH ROW WHEN (OLD.content_hash IS NOT NULL AND OLD.content_hash IS DISTINCT FROM NEW.content_hash
    AND (NEW.content_hash IS NULL OR split_part(OLD.content_hash, ':', 1) = split_part(NEW.content_hash, ':', 1)))
  EXECUTE FUNCTION public.chart_difficulties_retire_scores();

-- Nor is it a change of the chart (R010): while this stands, an update that changes nothing
-- but the hash leaves update_at alone (012's trigger touched the chart for any update of a block).
DROP TRIGGER chart_difficulties_touch_chart ON public.chart_difficulties;
CREATE TRIGGER chart_difficulties_touch_chart AFTER INSERT OR DELETE ON public.chart_difficulties
  FOR EACH ROW EXECUTE FUNCTION public.touch_related_chart();
CREATE TRIGGER chart_difficulties_touch_chart_update AFTER UPDATE ON public.chart_difficulties
  FOR EACH ROW WHEN ((to_jsonb(OLD) - 'content_hash') IS DISTINCT FROM (to_jsonb(NEW) - 'content_hash'))
  EXECUTE FUNCTION public.touch_related_chart();

INSERT INTO internal.schema_migrations(version) VALUES (20);
