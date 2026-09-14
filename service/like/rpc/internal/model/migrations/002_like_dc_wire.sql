-- Apply after 001 and before enabling the new like consumer. Existing facts
-- remain unversioned until a separate, evidence-backed reconciliation.
ALTER TABLE like_domain_fact_outbox
    ADD COLUMN IF NOT EXISTS aggregate_id varchar(128),
    ADD COLUMN IF NOT EXISTS fact_version bigint,
    ADD COLUMN IF NOT EXISTS delivery_envelope jsonb;

CREATE TABLE IF NOT EXISTS like_fact_stream (
    aggregate_id varchar(128) PRIMARY KEY,
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_like_fact_stream
    ON like_domain_fact_outbox (aggregate_id, fact_version);
CREATE INDEX IF NOT EXISTS idx_like_legacy_fact_subject_target
    ON like_domain_fact_outbox ((payload->>'subject_ref'), (payload->>'target_type'), (payload->>'target_id'))
    WHERE delivery_envelope IS NULL;

-- Old rows are retained for evidence-backed reconciliation. After migration,
-- an old consumer cannot silently create a new unversioned fact.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'like_new_fact_has_dc_wire') THEN
        ALTER TABLE like_domain_fact_outbox
            ADD CONSTRAINT like_new_fact_has_dc_wire
            CHECK (aggregate_id IS NOT NULL AND fact_version IS NOT NULL AND delivery_envelope IS NOT NULL) NOT VALID;
    END IF;
END $$;
