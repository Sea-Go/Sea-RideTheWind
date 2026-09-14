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
 payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), delivered_at timestamptz
);
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
