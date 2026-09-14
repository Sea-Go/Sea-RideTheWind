-- WS02-B: apply to the existing Article PostgreSQL database after article
-- and article_sync_outbox exist. This is the community publication authority,
-- not a second knowledge-module publication table.
CREATE TABLE IF NOT EXISTS article_revision (
    article_id varchar(32) NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    revision_id varchar(96) NOT NULL UNIQUE,
    author_id varchar(32) NOT NULL,
    source_object text NOT NULL,
    content_sha256 char(64) NOT NULL,
    title varchar(255) NOT NULL,
    brief varchar(512),
    cover_image_url varchar(255),
    manual_type_tag varchar(64),
    secondary_tags jsonb,
    markdown text NOT NULL,
    published_at timestamptz NOT NULL,
    sync_event_id varchar(64) NOT NULL UNIQUE,
    PRIMARY KEY (article_id,revision)
);
CREATE TABLE IF NOT EXISTS article_publication (
    article_id varchar(32) PRIMARY KEY,
    current_revision varchar(96),
    pointer_version bigint NOT NULL CHECK (pointer_version > 0),
    state varchar(16) NOT NULL CHECK (state IN ('published','retracted')),
    last_event_id varchar(128) NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS article_domain_outbox (
    event_id varchar(128) PRIMARY KEY,
    aggregate_id varchar(32) NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    event_type varchar(64) NOT NULL,
    payload jsonb NOT NULL,
    status smallint NOT NULL DEFAULT 0,
    retry_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (aggregate_id,aggregate_version)
);
CREATE INDEX IF NOT EXISTS article_domain_outbox_pending
    ON article_domain_outbox(status,created_at) WHERE status IN (0,2);

CREATE OR REPLACE FUNCTION reject_article_revision_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'article revision is immutable';
END;
$$;
DROP TRIGGER IF EXISTS article_revision_immutable ON article_revision;
CREATE TRIGGER article_revision_immutable
BEFORE UPDATE OR DELETE ON article_revision
FOR EACH ROW EXECUTE FUNCTION reject_article_revision_mutation();
