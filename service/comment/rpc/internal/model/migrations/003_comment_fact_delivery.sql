-- Apply after 002_comment_dc_wire.sql. A local accepted status means only that
-- DataCenter durably returned a matching technical receipt.
ALTER TABLE comment_domain_fact_outbox
    ADD COLUMN IF NOT EXISTS delivery_status smallint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS retry_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS technical_receipt_id varchar(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS technical_input_hash char(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS technical_offset bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS technical_received_at timestamptz,
    ADD COLUMN IF NOT EXISTS delivered_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_comment_fact_delivery
    ON comment_domain_fact_outbox (delivery_status, created_at, event_id);

CREATE OR REPLACE FUNCTION reject_comment_fact_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.event_id IS DISTINCT FROM OLD.event_id
       OR NEW.event_type IS DISTINCT FROM OLD.event_type
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
       OR NEW.fact_version IS DISTINCT FROM OLD.fact_version
       OR NEW.delivery_envelope IS DISTINCT FROM OLD.delivery_envelope THEN
        RAISE EXCEPTION 'comment fact envelope is immutable';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS comment_fact_identity_immutable ON comment_domain_fact_outbox;
CREATE TRIGGER comment_fact_identity_immutable
BEFORE UPDATE ON comment_domain_fact_outbox
FOR EACH ROW EXECUTE FUNCTION reject_comment_fact_identity_change();
