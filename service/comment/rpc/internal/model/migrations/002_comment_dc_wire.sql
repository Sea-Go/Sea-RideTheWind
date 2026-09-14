-- Apply after 001 and before enabling the new comment consumer. Existing
-- business payloads are intentionally not assigned guessed DC versions.
ALTER TABLE comment_domain_fact_outbox
    ADD COLUMN IF NOT EXISTS aggregate_id varchar(128),
    ADD COLUMN IF NOT EXISTS fact_version bigint,
    ADD COLUMN IF NOT EXISTS delivery_envelope jsonb;

CREATE TABLE IF NOT EXISTS comment_fact_stream (
    aggregate_id varchar(128) PRIMARY KEY,
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_comment_fact_stream
    ON comment_domain_fact_outbox (aggregate_id, fact_version);
CREATE INDEX IF NOT EXISTS idx_comment_legacy_fact_aggregate
    ON comment_domain_fact_outbox ((payload->>'aggregate_id'))
    WHERE delivery_envelope IS NULL;

-- NOT VALID preserves pre-cutover rows for explicit reconciliation, while
-- blocking any old consumer that tries to append another unversioned row.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'comment_new_fact_has_dc_wire') THEN
        ALTER TABLE comment_domain_fact_outbox
            ADD CONSTRAINT comment_new_fact_has_dc_wire
            CHECK (aggregate_id IS NOT NULL AND fact_version IS NOT NULL AND delivery_envelope IS NOT NULL) NOT VALID;
    END IF;
END $$;
