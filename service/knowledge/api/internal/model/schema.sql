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
-- Wiki Compile source envelopes are frozen once committed. Technical delivery
-- may set delivered_at, but it cannot edit the business EventID, body or
-- correlation to change a DC Job's canonical input under the same key.
CREATE OR REPLACE FUNCTION reject_wiki_compile_outbox_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.event_type IN ('knowledge.wiki.compile.requested.v1','knowledge.wiki.compile.cancelled.v1')
    AND (NEW.event_id IS DISTINCT FROM OLD.event_id
      OR NEW.event_type IS DISTINCT FROM OLD.event_type
      OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
      OR NEW.payload IS DISTINCT FROM OLD.payload
      OR NEW.correlation IS DISTINCT FROM OLD.correlation
      OR NEW.created_at IS DISTINCT FROM OLD.created_at
      OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at)) THEN
   RAISE EXCEPTION 'frozen Wiki Compile Outbox envelope cannot change';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS wiki_compile_outbox_identity_immutable ON knowledge_outbox;
CREATE TRIGGER wiki_compile_outbox_identity_immutable BEFORE UPDATE ON knowledge_outbox
FOR EACH ROW EXECUTE FUNCTION reject_wiki_compile_outbox_identity_change();
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

-- Default-off Wiki Compile -> DC Jobs technical receipts. The source Outbox
-- EventID/payload remain unchanged; this sidecar stores only the one DC job
-- accepted for a committed compile request and its optional cancellation.
CREATE TABLE IF NOT EXISTS knowledge_compile_jobs (
 compile_id text PRIMARY KEY REFERENCES knowledge_compiles(id),
 requested_event_id text NOT NULL UNIQUE REFERENCES knowledge_outbox(event_id),
 requested_operation_id text NOT NULL UNIQUE,
 source_event_jcs_sha256 char(64) NOT NULL CHECK(source_event_jcs_sha256 ~ '^[a-f0-9]{64}$'),
 submit_input_hash char(64) NOT NULL CHECK(submit_input_hash ~ '^[a-f0-9]{64}$'),
 job_id uuid NOT NULL UNIQUE,
 technical_received_at timestamptz NOT NULL,
 cancel_event_id text UNIQUE REFERENCES knowledge_outbox(event_id),
 cancel_version bigint NOT NULL DEFAULT 0 CHECK(cancel_version >= 0),
 technical_state text NOT NULL DEFAULT 'accepted' CHECK(technical_state IN ('accepted','cancel_requested','cancelled')),
 created_at timestamptz NOT NULL DEFAULT now()
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

-- RTW owns accepted product turns. A session row serializes its acceptance
-- order; framework Agent events remain in BTW's private runtime Session.
CREATE TABLE IF NOT EXISTS knowledge_answer_sessions (
 authority_id text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, last_ordinal bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(authority_id,tenant_id,subject_id,session_id)
);
CREATE TABLE IF NOT EXISTS knowledge_accepted_answers (
 answer_id text PRIMARY KEY, authority_id text NOT NULL, tenant_id text NOT NULL,
 subject_id text NOT NULL, session_id text NOT NULL, accepted_ordinal bigint NOT NULL,
 search_id text NOT NULL, status text NOT NULL CHECK(status IN ('succeeded','insufficient')),
 turn_hash text NOT NULL, turn_json text NOT NULL, accepted_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(authority_id,tenant_id,subject_id,session_id) REFERENCES knowledge_answer_sessions(authority_id,tenant_id,subject_id,session_id),
 UNIQUE(authority_id,tenant_id,subject_id,session_id,accepted_ordinal)
);
CREATE INDEX IF NOT EXISTS knowledge_accepted_answers_session_order ON knowledge_accepted_answers
 (authority_id,tenant_id,subject_id,session_id,accepted_ordinal);
-- A public product key binds one canonical request and the manually published
-- snapshot before any BTW call. A lease is only dispatch ownership; an accepted
-- answer is the authoritative completion and can recover after a lost reply.
CREATE TABLE IF NOT EXISTS knowledge_product_search_operations (
 authority_id text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL, request_hash text NOT NULL,
 request_json jsonb NOT NULL, snapshot jsonb NOT NULL,
 search_id text NOT NULL UNIQUE, answer_id text NOT NULL UNIQUE,
 status text NOT NULL CHECK(status IN ('pending','running','failed','committed')),
 scope_version text NOT NULL DEFAULT 'v1'
  CONSTRAINT knowledge_product_search_scope_version_ck CHECK(scope_version IN ('v1','v2')),
 attempt bigint NOT NULL DEFAULT 0, lease_token text, lease_until timestamptz,
 last_error_code text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(authority_id,tenant_id,subject_id,session_id,operation_key)
);
CREATE INDEX IF NOT EXISTS knowledge_product_search_session ON knowledge_product_search_operations
 (authority_id,tenant_id,subject_id,session_id,created_at);
-- A Tool parent pins one manually published snapshot and server-owned total
-- budget. Child reservations spend their upper bound before BTW is called.
CREATE TABLE IF NOT EXISTS knowledge_tool_parents (
 authority_id text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL, operation_id text NOT NULL UNIQUE,
 module_id text NOT NULL, snapshot jsonb NOT NULL, snapshot_ref text NOT NULL,
 scope_ref text NOT NULL, budget_ref text NOT NULL UNIQUE, expires_at timestamptz NOT NULL,
 scope_version text NOT NULL DEFAULT 'v1'
  CONSTRAINT knowledge_tool_parent_scope_version_ck CHECK(scope_version IN ('v1','v2')),
 search_remaining integer NOT NULL CHECK(search_remaining>=0),
 read_remaining integer NOT NULL CHECK(read_remaining>=0),
 quote_remaining integer NOT NULL CHECK(quote_remaining>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(authority_id,tenant_id,subject_id,session_id,operation_key)
);
CREATE TABLE IF NOT EXISTS knowledge_tool_searches (
 operation_id text NOT NULL REFERENCES knowledge_tool_parents(operation_id),
 operation_key text NOT NULL, request_hash text NOT NULL, request_json jsonb NOT NULL,
 search_id text NOT NULL UNIQUE, reserved_reads integer NOT NULL CHECK(reserved_reads>0),
 reserved_runes integer NOT NULL CHECK(reserved_runes>0),
 status text NOT NULL CHECK(status IN ('pending','running','failed','complete')),
 lease_token text, lease_until timestamptz, attempt bigint NOT NULL DEFAULT 0,
 result_json jsonb, last_error_code text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,operation_key)
);
CREATE INDEX IF NOT EXISTS knowledge_tool_searches_parent ON knowledge_tool_searches(operation_id,created_at);
CREATE TABLE IF NOT EXISTS knowledge_tool_reads (
 operation_id text NOT NULL REFERENCES knowledge_tool_parents(operation_id),
 operation_key text NOT NULL, search_id text NOT NULL REFERENCES knowledge_tool_searches(search_id),
 evidence_id text NOT NULL, result_json jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,operation_key)
);
CREATE TABLE IF NOT EXISTS knowledge_answer_citations (
 answer_id text NOT NULL REFERENCES knowledge_accepted_answers(answer_id),
 search_id text NOT NULL REFERENCES knowledge_search_citations(search_id),
 citation_order integer NOT NULL, evidence_id text NOT NULL,
 source_kind text NOT NULL, content_id text NOT NULL, revision_id text NOT NULL,
 chunk_id text NOT NULL, locator jsonb NOT NULL, quote_hash text NOT NULL,
 PRIMARY KEY(answer_id,evidence_id), UNIQUE(answer_id,citation_order)
);
DROP TRIGGER IF EXISTS knowledge_accepted_answer_immutable ON knowledge_accepted_answers;
CREATE TRIGGER knowledge_accepted_answer_immutable BEFORE UPDATE OR DELETE ON knowledge_accepted_answers
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_payload();
DROP TRIGGER IF EXISTS knowledge_answer_citation_immutable ON knowledge_answer_citations;
CREATE TRIGGER knowledge_answer_citation_immutable BEFORE UPDATE OR DELETE ON knowledge_answer_citations
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_payload();

-- An administrator's relevance decision is an append-only revision over one
-- real product query and one fixed, published chunk. A withdrawal appends a
-- tombstone rather than erasing the original human judgment or event.
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

-- Reviewer keys are administrator-owned, append-only registrations. Revocation
-- is a separate immutable fact; it invalidates current offline trust without
-- deleting historical signed reviews.
CREATE TABLE IF NOT EXISTS knowledge_reviewer_keys (
 key_id text PRIMARY KEY, reviewer_authority text NOT NULL, reviewer_id text NOT NULL,
 data_kind text NOT NULL CHECK(data_kind IN ('human_admin','synthetic_fixture')),
 public_key_ed25519_hex text NOT NULL UNIQUE, registered_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS knowledge_reviewer_key_revocations (
 key_id text PRIMARY KEY REFERENCES knowledge_reviewer_keys(key_id),
 revoked_at timestamptz NOT NULL, actor_id text NOT NULL, reason text NOT NULL,
 event_id text NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS knowledge_answer_grounding_reviews (
 case_sha256 text PRIMARY KEY, key_id text NOT NULL REFERENCES knowledge_reviewer_keys(key_id),
 case_json text NOT NULL, review_json text NOT NULL, review_sha256 text NOT NULL,
 reviewer_authority text NOT NULL, reviewer_id text NOT NULL, data_kind text NOT NULL,
 registry_revision_at_commit bigint NOT NULL,
 answer_id text NOT NULL REFERENCES knowledge_accepted_answers(answer_id), search_id text NOT NULL,
 accepted_answer_sha256 text NOT NULL, turn_sha256 text NOT NULL,
 citation_pack_ref text NOT NULL, citation_pack_sha256 text NOT NULL,
 trace_authority_status text NOT NULL CHECK(trace_authority_status='external_case_unverified'),
 reviewed_at timestamptz NOT NULL, event_id text NOT NULL UNIQUE,
 event_json text NOT NULL, event_sha256 text NOT NULL
);
DROP TRIGGER IF EXISTS knowledge_reviewer_key_immutable ON knowledge_reviewer_keys;
CREATE TRIGGER knowledge_reviewer_key_immutable BEFORE UPDATE OR DELETE ON knowledge_reviewer_keys
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_reviewer_key_revocation_immutable ON knowledge_reviewer_key_revocations;
CREATE TRIGGER knowledge_reviewer_key_revocation_immutable BEFORE UPDATE OR DELETE ON knowledge_reviewer_key_revocations
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_answer_grounding_review_immutable ON knowledge_answer_grounding_reviews;
CREATE TRIGGER knowledge_answer_grounding_review_immutable BEFORE UPDATE OR DELETE ON knowledge_answer_grounding_reviews
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
