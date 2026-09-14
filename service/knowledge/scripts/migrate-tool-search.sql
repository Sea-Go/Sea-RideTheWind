-- Apply before enabling SearchTools. Reentrant on the Knowledge database.
BEGIN;
CREATE TABLE IF NOT EXISTS knowledge_tool_parents (
 authority_id text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL, operation_id text NOT NULL UNIQUE,
 module_id text NOT NULL, snapshot jsonb NOT NULL, snapshot_ref text NOT NULL,
 scope_ref text NOT NULL, budget_ref text NOT NULL UNIQUE, expires_at timestamptz NOT NULL,
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
COMMIT;
