-- Apply to the existing Knowledge PostgreSQL database before enabling
-- SearchSummary. Reentrant; holds only normal DDL locks. Run in a maintenance
-- window and back up the database under the deployment's normal procedure.
BEGIN;
CREATE TABLE IF NOT EXISTS knowledge_product_search_operations (
 authority_id text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL, request_hash text NOT NULL,
 request_json jsonb NOT NULL, snapshot jsonb NOT NULL,
 search_id text NOT NULL UNIQUE, answer_id text NOT NULL UNIQUE,
 status text NOT NULL CHECK(status IN ('pending','running','failed','committed')),
 attempt bigint NOT NULL DEFAULT 0, lease_token text, lease_until timestamptz,
 last_error_code text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(authority_id,tenant_id,subject_id,session_id,operation_key)
);
CREATE INDEX IF NOT EXISTS knowledge_product_search_session ON knowledge_product_search_operations
 (authority_id,tenant_id,subject_id,session_id,created_at);
COMMIT;
