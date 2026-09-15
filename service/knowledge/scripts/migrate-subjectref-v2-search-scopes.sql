-- Candidate expand DDL. Apply only after the existing local Stage3 marker,
-- SubjectRef sidecar preflight and both v1 operation tables exist. Old rows
-- remain v1; a new operation pins one version in its original reservation.
BEGIN;
CREATE TEMP TABLE knowledge_scope_version_ddl_reference (
 scope_version text NOT NULL DEFAULT 'v1',
 CONSTRAINT knowledge_scope_version_ddl_reference_ck CHECK(scope_version IN ('v1','v2'))
) ON COMMIT DROP;
-- Reentrant ADD COLUMN / IF NOT EXISTS must never bless a preexisting weak
-- named CHECK, wrong type or v2 default. Check before the first durable DDL.
DO $$
DECLARE present integer; exact integer;
BEGIN
 SELECT count(*) INTO present FROM (VALUES
  (to_regclass('knowledge_product_search_operations')),
  (to_regclass('knowledge_tool_parents'))) r(relation_id)
 JOIN pg_attribute a ON a.attrelid=r.relation_id AND a.attname='scope_version'
 WHERE a.attnum>0 AND NOT a.attisdropped;
 IF present=0 THEN RETURN; END IF;
 IF present<>2 THEN RAISE EXCEPTION 'partly expanded search scope version columns'; END IF;
 WITH expected(relation_id,constraint_name) AS (VALUES
  (to_regclass('knowledge_product_search_operations'), 'knowledge_product_search_scope_version_ck'),
  (to_regclass('knowledge_tool_parents'), 'knowledge_tool_parent_scope_version_ck')),
 reference AS (
  SELECT a.atttypid column_type, pg_get_expr(d.adbin,d.adrelid) default_expr,
   pg_get_constraintdef(c.oid) check_expr
  FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  JOIN pg_constraint c ON c.conrelid=a.attrelid AND c.conname='knowledge_scope_version_ddl_reference_ck'
  WHERE a.attrelid='knowledge_scope_version_ddl_reference'::regclass AND a.attname='scope_version')
 SELECT count(*) INTO exact FROM expected x
 JOIN pg_class r ON r.oid=x.relation_id AND r.relkind='r'
 JOIN pg_attribute a ON a.attrelid=r.oid AND a.attname='scope_version'
  AND a.attnum>0 AND NOT a.attisdropped AND a.attnotnull
  AND a.attgenerated='' AND a.attidentity=''
 JOIN pg_attrdef d ON d.adrelid=r.oid AND d.adnum=a.attnum
 JOIN pg_constraint c ON c.conrelid=r.oid AND c.conname=x.constraint_name
  AND c.contype='c' AND c.convalidated AND NOT c.condeferrable
 CROSS JOIN reference ref
 WHERE a.atttypid=ref.column_type AND pg_get_expr(d.adbin,d.adrelid)=ref.default_expr
  AND pg_get_constraintdef(c.oid)=ref.check_expr
  AND c.conkey=ARRAY[a.attnum]::smallint[];
 IF exact<>2 THEN RAISE EXCEPTION 'preexisting search scope version catalog differs from owner DDL'; END IF;
END $$;
ALTER TABLE knowledge_product_search_operations ADD COLUMN IF NOT EXISTS scope_version text NOT NULL DEFAULT 'v1';
ALTER TABLE knowledge_tool_parents ADD COLUMN IF NOT EXISTS scope_version text NOT NULL DEFAULT 'v1';
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_product_search_operations'::regclass
  AND conname='knowledge_product_search_scope_version_ck') THEN
  ALTER TABLE knowledge_product_search_operations ADD CONSTRAINT knowledge_product_search_scope_version_ck
   CHECK(scope_version IN ('v1','v2'));
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_tool_parents'::regclass
  AND conname='knowledge_tool_parent_scope_version_ck') THEN
  ALTER TABLE knowledge_tool_parents ADD CONSTRAINT knowledge_tool_parent_scope_version_ck
   CHECK(scope_version IN ('v1','v2'));
 END IF;
END $$;
COMMIT;
