-- Apply after 001_favorite_fact_outbox.sql. The RTW row records only DC's
-- technical receipt; downstream user-fact/domain acceptance is independent.
ALTER TABLE favorite_fact_outbox
    ADD COLUMN IF NOT EXISTS technical_receipt_id varchar(128),
    ADD COLUMN IF NOT EXISTS technical_input_hash char(64),
    ADD COLUMN IF NOT EXISTS technical_offset bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS technical_received_at timestamptz,
    ADD COLUMN IF NOT EXISTS delivered_at timestamptz;

CREATE OR REPLACE FUNCTION reject_favorite_fact_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.event_id IS DISTINCT FROM OLD.event_id
       OR NEW.favorite_id IS DISTINCT FROM OLD.favorite_id
       OR NEW.aggregate_version IS DISTINCT FROM OLD.aggregate_version
       OR NEW.payload IS DISTINCT FROM OLD.payload THEN
        RAISE EXCEPTION 'favorite fact envelope is immutable';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS favorite_fact_identity_immutable ON favorite_fact_outbox;
CREATE TRIGGER favorite_fact_identity_immutable
BEFORE UPDATE ON favorite_fact_outbox
FOR EACH ROW EXECUTE FUNCTION reject_favorite_fact_identity_change();
