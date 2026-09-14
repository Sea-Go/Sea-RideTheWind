-- Apply to the existing Knowledge PostgreSQL database before enabling
-- SearchJudgments. Reentrant; no synthetic or historical labels are backfilled.
BEGIN;
CREATE TABLE IF NOT EXISTS knowledge_search_judgment_revisions (
 revision_id text PRIMARY KEY, judgment_id text NOT NULL,
 module_id text NOT NULL REFERENCES knowledge_modules(id), search_id text NOT NULL,
 chunk_id text NOT NULL, base_revision_id text NOT NULL DEFAULT '',
 state text NOT NULL CHECK(state IN ('judged','withdrawn')),
 grade smallint CHECK(grade BETWEEN 0 AND 3),
 data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((state='judged' AND grade IS NOT NULL) OR (state='withdrawn' AND grade IS NULL))
);
CREATE INDEX IF NOT EXISTS knowledge_search_judgment_revisions_pair
 ON knowledge_search_judgment_revisions(search_id,chunk_id,created_at,revision_id);
CREATE TABLE IF NOT EXISTS knowledge_search_judgment_heads (
 search_id text NOT NULL, chunk_id text NOT NULL, judgment_id text NOT NULL UNIQUE,
 revision_id text NOT NULL REFERENCES knowledge_search_judgment_revisions(revision_id),
 PRIMARY KEY(search_id,chunk_id)
);
CREATE TABLE IF NOT EXISTS knowledge_search_judgment_events (
 event_id text PRIMARY KEY, revision_id text NOT NULL UNIQUE REFERENCES knowledge_search_judgment_revisions(revision_id),
 event_json text NOT NULL, event_sha256 text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION knowledge_immutable_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'immutable knowledge record';
END $$;
DROP TRIGGER IF EXISTS knowledge_search_judgment_revision_immutable ON knowledge_search_judgment_revisions;
CREATE TRIGGER knowledge_search_judgment_revision_immutable BEFORE UPDATE OR DELETE ON knowledge_search_judgment_revisions
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_search_judgment_event_immutable ON knowledge_search_judgment_events;
CREATE TRIGGER knowledge_search_judgment_event_immutable BEFORE UPDATE OR DELETE ON knowledge_search_judgment_events
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
COMMIT;
