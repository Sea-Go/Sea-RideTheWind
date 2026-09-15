-- Apply only after reviewing the existing Knowledge database. Reentrant;
-- no old Wiki/Source revisions, labels, EventSpec or publication are backfilled.
BEGIN;
CREATE TABLE IF NOT EXISTS knowledge_wiki_quality_revisions (
 revision_id text PRIMARY KEY, judgment_id text NOT NULL,
 module_id text NOT NULL REFERENCES knowledge_modules(id), page_id text NOT NULL,
 wiki_revision_id text NOT NULL REFERENCES knowledge_revisions(id),
 fact_id text NOT NULL CHECK(fact_id ~ '^fact_[a-f0-9]{64}$'),
 base_judge_revision_id text NOT NULL DEFAULT '',
 judge_revision bigint NOT NULL CHECK(judge_revision > 0),
 assessment text NOT NULL CHECK(assessment IN ('covered','missing','conflict','undetermined')),
 grade smallint, data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(wiki_revision_id,fact_id,judge_revision),
 CHECK((assessment='undetermined' AND grade IS NULL)
    OR (assessment IN ('missing','conflict') AND grade=0)
    OR (assessment='covered' AND grade BETWEEN 1 AND 3))
);
CREATE INDEX IF NOT EXISTS knowledge_wiki_quality_revision_pair
 ON knowledge_wiki_quality_revisions(wiki_revision_id,fact_id,judge_revision DESC);
CREATE TABLE IF NOT EXISTS knowledge_wiki_quality_heads (
 wiki_revision_id text NOT NULL REFERENCES knowledge_revisions(id),
 fact_id text NOT NULL, judgment_id text NOT NULL UNIQUE,
 revision_id text NOT NULL REFERENCES knowledge_wiki_quality_revisions(revision_id),
 PRIMARY KEY(wiki_revision_id,fact_id)
);
CREATE TABLE IF NOT EXISTS knowledge_wiki_quality_events (
 event_id text PRIMARY KEY REFERENCES knowledge_outbox(event_id),
 revision_id text NOT NULL UNIQUE REFERENCES knowledge_wiki_quality_revisions(revision_id),
 event_json text NOT NULL,
 event_raw_sha256 char(64) NOT NULL CHECK(event_raw_sha256 ~ '^[a-f0-9]{64}$'),
 event_jcs_sha256 char(64) NOT NULL CHECK(event_jcs_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION knowledge_immutable_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'immutable knowledge record';
END $$;
DROP TRIGGER IF EXISTS knowledge_wiki_quality_revision_immutable ON knowledge_wiki_quality_revisions;
CREATE TRIGGER knowledge_wiki_quality_revision_immutable BEFORE UPDATE OR DELETE ON knowledge_wiki_quality_revisions
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_wiki_quality_event_immutable ON knowledge_wiki_quality_events;
CREATE TRIGGER knowledge_wiki_quality_event_immutable BEFORE UPDATE OR DELETE ON knowledge_wiki_quality_events
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
CREATE OR REPLACE FUNCTION reject_wiki_quality_outbox_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.event_type='knowledge.wiki.quality.judged.v1'
    AND (NEW.event_id IS DISTINCT FROM OLD.event_id
      OR NEW.event_type IS DISTINCT FROM OLD.event_type
      OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
      OR NEW.payload IS DISTINCT FROM OLD.payload
      OR NEW.correlation IS DISTINCT FROM OLD.correlation
      OR NEW.created_at IS DISTINCT FROM OLD.created_at
      OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at)) THEN
   RAISE EXCEPTION 'frozen Wiki quality Outbox envelope cannot change';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS wiki_quality_outbox_identity_immutable ON knowledge_outbox;
CREATE TRIGGER wiki_quality_outbox_identity_immutable BEFORE UPDATE ON knowledge_outbox
FOR EACH ROW EXECUTE FUNCTION reject_wiki_quality_outbox_identity_change();
COMMIT;
