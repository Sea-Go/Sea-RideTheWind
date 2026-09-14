CREATE TABLE IF NOT EXISTS knowledge_modules (
 id text PRIMARY KEY, data jsonb NOT NULL, event_sequence bigint NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS knowledge_revisions (
 id text PRIMARY KEY, module_id text NOT NULL REFERENCES knowledge_modules(id),
 entity_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('source','wiki')),
 data jsonb NOT NULL, withdrawn boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS knowledge_heads (
 module_id text NOT NULL REFERENCES knowledge_modules(id), kind text NOT NULL,
 entity_id text NOT NULL, revision_id text NOT NULL REFERENCES knowledge_revisions(id),
 PRIMARY KEY(module_id,kind,entity_id)
);
CREATE TABLE IF NOT EXISTS knowledge_releases (
 id text PRIMARY KEY, module_id text NOT NULL REFERENCES knowledge_modules(id), ordinal bigint NOT NULL,
 data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(module_id,ordinal)
);
CREATE TABLE IF NOT EXISTS knowledge_builds (
 id text PRIMARY KEY, release_id text NOT NULL REFERENCES knowledge_releases(id),
 module_id text NOT NULL REFERENCES knowledge_modules(id), generation bigint NOT NULL,
 data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(release_id,generation)
);
CREATE TABLE IF NOT EXISTS knowledge_operations (
 scope text NOT NULL, operation_key text NOT NULL, input_hash text NOT NULL,
 response jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(scope,operation_key)
);
CREATE TABLE IF NOT EXISTS knowledge_outbox (
 event_id text PRIMARY KEY, event_type text NOT NULL, aggregate_id text NOT NULL,
 payload jsonb NOT NULL, correlation jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(), delivered_at timestamptz
);
ALTER TABLE knowledge_outbox ADD COLUMN IF NOT EXISTS correlation jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX IF NOT EXISTS knowledge_outbox_pending ON knowledge_outbox(created_at,event_id) WHERE delivered_at IS NULL;
CREATE TABLE IF NOT EXISTS knowledge_publications (
 module_id text NOT NULL REFERENCES knowledge_modules(id), pointer_revision bigint NOT NULL,
 release_id text NOT NULL REFERENCES knowledge_releases(id), build_id text NOT NULL REFERENCES knowledge_builds(id),
 actor text NOT NULL, reason text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(module_id,pointer_revision)
);
-- Revisions/releases are append-only. Withdrawal is a separate mutable column.
CREATE OR REPLACE FUNCTION knowledge_immutable_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable knowledge record'; END IF;
 IF (to_jsonb(NEW)-'withdrawn') IS DISTINCT FROM (to_jsonb(OLD)-'withdrawn') THEN
  RAISE EXCEPTION 'immutable knowledge payload';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS knowledge_revision_immutable ON knowledge_revisions;
CREATE TRIGGER knowledge_revision_immutable BEFORE UPDATE OR DELETE ON knowledge_revisions FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_payload();
DROP TRIGGER IF EXISTS knowledge_release_immutable ON knowledge_releases;
CREATE TRIGGER knowledge_release_immutable BEFORE UPDATE OR DELETE ON knowledge_releases FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_payload();
CREATE TABLE IF NOT EXISTS knowledge_compiles (
 id text PRIMARY KEY, module_id text NOT NULL REFERENCES knowledge_modules(id),
 page_id text NOT NULL, generation bigint NOT NULL, data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(module_id,page_id,generation)
);

-- These identities are immutable creation order, not public entity IDs. Creation
-- paths hold the module lock before INSERT, so a module's committed upper bound
-- excludes all later inserts even across same-timestamp transactions or UUIDs.
ALTER TABLE knowledge_revisions ADD COLUMN IF NOT EXISTS list_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX IF NOT EXISTS knowledge_revisions_module_page ON knowledge_revisions(module_id,list_order DESC);
ALTER TABLE knowledge_releases ADD COLUMN IF NOT EXISTS list_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX IF NOT EXISTS knowledge_releases_module_page ON knowledge_releases(module_id,list_order DESC);
ALTER TABLE knowledge_builds ADD COLUMN IF NOT EXISTS list_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX IF NOT EXISTS knowledge_builds_module_page ON knowledge_builds(module_id,list_order DESC);
ALTER TABLE knowledge_compiles ADD COLUMN IF NOT EXISTS list_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX IF NOT EXISTS knowledge_compiles_module_page ON knowledge_compiles(module_id,list_order DESC);
CREATE INDEX IF NOT EXISTS knowledge_publications_release ON knowledge_publications(module_id,release_id);
-- A search ID has one immutable accepted evidence pack. The original JSON
-- bytes are retained because BTW hashes its exact encoding before delivery.
CREATE TABLE IF NOT EXISTS knowledge_search_citations (
 search_id text PRIMARY KEY, pack_hash text NOT NULL, pack_json text NOT NULL,
 durable_ref text NOT NULL UNIQUE, module_id text NOT NULL REFERENCES knowledge_modules(id),
 release_id text NOT NULL REFERENCES knowledge_releases(id), pointer_revision bigint NOT NULL,
 generation bigint NOT NULL, accepted_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_search_citations_module ON knowledge_search_citations(module_id,accepted_at);
DROP TRIGGER IF EXISTS knowledge_search_citation_immutable ON knowledge_search_citations;
CREATE TRIGGER knowledge_search_citation_immutable BEFORE UPDATE OR DELETE ON knowledge_search_citations
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_payload();
