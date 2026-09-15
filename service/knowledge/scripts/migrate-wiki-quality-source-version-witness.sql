-- Reentrant expand for the read-only Wiki source-version witness. Apply only
-- after reviewing existing knowledge_outbox rows and producer delivery state.
-- This does not backfill original raw Event bytes or declare a DC cutoff.
BEGIN;
CREATE OR REPLACE FUNCTION reject_knowledge_outbox_business_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'knowledge Outbox Event cannot be deleted';
 END IF;
 IF NEW.event_id IS DISTINCT FROM OLD.event_id
    OR NEW.event_type IS DISTINCT FROM OLD.event_type
    OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
    OR NEW.payload IS DISTINCT FROM OLD.payload
    OR NEW.correlation IS DISTINCT FROM OLD.correlation
    OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at) THEN
  RAISE EXCEPTION 'knowledge Outbox business Event cannot change';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS knowledge_outbox_business_immutable ON knowledge_outbox;
CREATE TRIGGER knowledge_outbox_business_immutable BEFORE UPDATE OR DELETE ON knowledge_outbox
FOR EACH ROW EXECUTE FUNCTION reject_knowledge_outbox_business_change();
COMMIT;
