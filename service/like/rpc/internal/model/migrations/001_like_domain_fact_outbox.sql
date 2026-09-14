-- Apply before deploying the like MQ consumer. Old last_operation_id=0 rows
-- are retained as legacy state; ambiguous historical state=2 is not backfilled
-- into new domain facts.
ALTER TABLE like_record
    ADD COLUMN IF NOT EXISTS last_operation_id bigint NOT NULL DEFAULT 0;

ALTER TABLE like_consume_inbox
    ADD COLUMN IF NOT EXISTS payload_hash varchar(64) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS like_domain_fact_outbox (
    event_id varchar(128) PRIMARY KEY,
    event_type varchar(64) NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
