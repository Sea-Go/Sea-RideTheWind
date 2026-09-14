-- WS02-B: apply to the existing Favorite PostgreSQL database before RPC
-- rollout. It never creates a second favorite authority.
CREATE TABLE IF NOT EXISTS favorite_fact_outbox (
    event_id varchar(128) PRIMARY KEY,
    favorite_id bigint NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version IN (1,2)),
    payload jsonb NOT NULL,
    status smallint NOT NULL DEFAULT 0,
    retry_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (favorite_id,aggregate_version)
);
CREATE INDEX IF NOT EXISTS favorite_fact_outbox_pending
    ON favorite_fact_outbox(status,created_at) WHERE status IN (0,2);
