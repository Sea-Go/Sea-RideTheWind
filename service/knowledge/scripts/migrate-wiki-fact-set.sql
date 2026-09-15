-- Apply to an existing Knowledge database only after reviewing the old
-- Wiki/Source/quality rows. Reentrant, append-only and default-off: this
-- creates no FactSet revision/head/Event and rewrites no old EventSpec.
BEGIN;
CREATE TABLE IF NOT EXISTS knowledge_wiki_fact_set_revisions (
 revision_id text PRIMARY KEY, fact_set_id text NOT NULL,
 module_id text NOT NULL REFERENCES knowledge_modules(id), page_id text NOT NULL,
 source_scope_revision text NOT NULL CHECK(source_scope_revision ~ '^scope_[a-f0-9]{64}$'),
 wiki_revision_id text NOT NULL REFERENCES knowledge_revisions(id),
 base_fact_set_revision_id text NOT NULL DEFAULT '',
 fact_set_revision bigint NOT NULL CHECK(fact_set_revision > 0),
 fact_set_jcs_sha256 char(64) NOT NULL CHECK(fact_set_jcs_sha256 ~ '^[a-f0-9]{64}$'),
 facts_complete boolean NOT NULL CHECK(facts_complete),
 data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(module_id,page_id,source_scope_revision,fact_set_revision),
 UNIQUE(fact_set_id,fact_set_revision)
);
CREATE TABLE IF NOT EXISTS knowledge_wiki_fact_set_heads (
 module_id text NOT NULL REFERENCES knowledge_modules(id), page_id text NOT NULL,
 source_scope_revision text NOT NULL CHECK(source_scope_revision ~ '^scope_[a-f0-9]{64}$'),
 fact_set_id text NOT NULL UNIQUE,
 revision_id text NOT NULL REFERENCES knowledge_wiki_fact_set_revisions(revision_id),
 PRIMARY KEY(module_id,page_id,source_scope_revision)
);
CREATE TABLE IF NOT EXISTS knowledge_wiki_fact_set_events (
 event_id text PRIMARY KEY REFERENCES knowledge_outbox(event_id),
 revision_id text NOT NULL UNIQUE REFERENCES knowledge_wiki_fact_set_revisions(revision_id),
 event_json text NOT NULL,
 event_raw_sha256 char(64) NOT NULL CHECK(event_raw_sha256 ~ '^[a-f0-9]{64}$'),
 event_jcs_sha256 char(64) NOT NULL CHECK(event_jcs_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION knowledge_immutable_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'immutable knowledge record';
END $$;
DROP TRIGGER IF EXISTS knowledge_wiki_fact_set_revision_immutable ON knowledge_wiki_fact_set_revisions;
CREATE TRIGGER knowledge_wiki_fact_set_revision_immutable BEFORE UPDATE OR DELETE ON knowledge_wiki_fact_set_revisions
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_wiki_fact_set_event_immutable ON knowledge_wiki_fact_set_events;
CREATE TRIGGER knowledge_wiki_fact_set_event_immutable BEFORE UPDATE OR DELETE ON knowledge_wiki_fact_set_events
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
CREATE OR REPLACE FUNCTION reject_wiki_fact_set_outbox_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.event_type='knowledge.wiki.fact-set.frozen.v1'
    AND (NEW.event_id IS DISTINCT FROM OLD.event_id
      OR NEW.event_type IS DISTINCT FROM OLD.event_type
      OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
      OR NEW.payload IS DISTINCT FROM OLD.payload
      OR NEW.correlation IS DISTINCT FROM OLD.correlation
      OR NEW.created_at IS DISTINCT FROM OLD.created_at
      OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at)) THEN
   RAISE EXCEPTION 'frozen Wiki FactSet Outbox envelope cannot change';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS wiki_fact_set_outbox_identity_immutable ON knowledge_outbox;
CREATE TRIGGER wiki_fact_set_outbox_identity_immutable BEFORE UPDATE ON knowledge_outbox
FOR EACH ROW EXECUTE FUNCTION reject_wiki_fact_set_outbox_identity_change();
COMMIT;
