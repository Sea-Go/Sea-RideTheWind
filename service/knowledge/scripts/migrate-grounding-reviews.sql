-- Apply before enabling GroundingReviews. No reviewer key or decision is backfilled.
BEGIN;
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
CREATE OR REPLACE FUNCTION knowledge_immutable_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'immutable knowledge record';
END $$;
DROP TRIGGER IF EXISTS knowledge_reviewer_key_immutable ON knowledge_reviewer_keys;
CREATE TRIGGER knowledge_reviewer_key_immutable BEFORE UPDATE OR DELETE ON knowledge_reviewer_keys
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_reviewer_key_revocation_immutable ON knowledge_reviewer_key_revocations;
CREATE TRIGGER knowledge_reviewer_key_revocation_immutable BEFORE UPDATE OR DELETE ON knowledge_reviewer_key_revocations
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
DROP TRIGGER IF EXISTS knowledge_answer_grounding_review_immutable ON knowledge_answer_grounding_reviews;
CREATE TRIGGER knowledge_answer_grounding_review_immutable BEFORE UPDATE OR DELETE ON knowledge_answer_grounding_reviews
 FOR EACH ROW EXECUTE FUNCTION knowledge_immutable_row();
COMMIT;
