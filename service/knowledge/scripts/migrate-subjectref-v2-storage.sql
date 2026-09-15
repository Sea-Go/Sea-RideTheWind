-- Explicit, local-candidate expand/backfill. Never include this file in schema.sql
-- or a Store constructor. The stage-1 read-only preflight must pass first.
-- PostgreSQL aborts the whole DDL and projection if any old row is incompatible.
BEGIN;
SET LOCAL lock_timeout = '10s';
LOCK TABLE knowledge_answer_sessions, knowledge_accepted_answers,
 knowledge_product_search_operations, knowledge_tool_parents IN SHARE MODE;

-- The original answer_id is the frozen public ID. This additive index makes
-- the sidecar FK bind answer_id AND its exact v1 scope/ordinal to one old row.
CREATE UNIQUE INDEX IF NOT EXISTS knowledge_accepted_answers_v2_anchor_uq
 ON knowledge_accepted_answers(answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal);

-- tenant_id appears only as the fixed v1 compatibility slot. It is absent
-- from every v2 PK/unique key and may never be supplied as a namespace.
CREATE TABLE IF NOT EXISTS knowledge_answer_sessions_subject_v2 (
 issuer text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL,
 CONSTRAINT knowledge_answer_sessions_subject_v2_pkey PRIMARY KEY(issuer,subject_id,session_id),
 CONSTRAINT knowledge_answer_sessions_subject_v2_identity_ck CHECK (
  issuer='rtw.identity' AND tenant_id='platform' AND
  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
);
CREATE TABLE IF NOT EXISTS knowledge_accepted_answers_subject_v2 (
 answer_id text NOT NULL, issuer text NOT NULL, tenant_id text NOT NULL,
 subject_id text NOT NULL, session_id text NOT NULL, accepted_ordinal bigint NOT NULL,
 CONSTRAINT knowledge_accepted_answers_subject_v2_pkey PRIMARY KEY(answer_id),
 CONSTRAINT knowledge_accepted_answers_subject_v2_v2_uq
  UNIQUE(issuer,subject_id,session_id,accepted_ordinal),
 CONSTRAINT knowledge_accepted_answers_subject_v2_identity_ck CHECK (
  issuer='rtw.identity' AND tenant_id='platform' AND
  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
);
CREATE TABLE IF NOT EXISTS knowledge_product_search_operations_subject_v2 (
 issuer text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL,
 CONSTRAINT knowledge_product_search_operations_subject_v2_pkey
  PRIMARY KEY(issuer,subject_id,session_id,operation_key),
 CONSTRAINT knowledge_product_search_operations_subject_v2_identity_ck CHECK (
  issuer='rtw.identity' AND tenant_id='platform' AND
  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
);
CREATE TABLE IF NOT EXISTS knowledge_tool_parents_subject_v2 (
 issuer text NOT NULL, tenant_id text NOT NULL, subject_id text NOT NULL,
 session_id text NOT NULL, operation_key text NOT NULL,
 CONSTRAINT knowledge_tool_parents_subject_v2_pkey
  PRIMARY KEY(issuer,subject_id,session_id,operation_key),
 CONSTRAINT knowledge_tool_parents_subject_v2_identity_ck CHECK (
  issuer='rtw.identity' AND tenant_id='platform' AND
  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
);

-- NOT VALID followed by VALIDATE makes the verification point explicit.
-- Old PK/FK/UQ, immutable triggers, turns, operation IDs and outbox are untouched.
DO $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_answer_sessions_subject_v2'::regclass
  AND conname='knowledge_answer_sessions_subject_v2_legacy_fk') THEN
  ALTER TABLE knowledge_answer_sessions_subject_v2 ADD CONSTRAINT knowledge_answer_sessions_subject_v2_legacy_fk
   FOREIGN KEY(issuer,tenant_id,subject_id,session_id)
   REFERENCES knowledge_answer_sessions(authority_id,tenant_id,subject_id,session_id) NOT VALID;
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_accepted_answers_subject_v2'::regclass
  AND conname='knowledge_accepted_answers_subject_v2_legacy_fk') THEN
  ALTER TABLE knowledge_accepted_answers_subject_v2 ADD CONSTRAINT knowledge_accepted_answers_subject_v2_legacy_fk
   FOREIGN KEY(answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal)
   REFERENCES knowledge_accepted_answers(answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal) NOT VALID;
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_accepted_answers_subject_v2'::regclass
  AND conname='knowledge_accepted_answers_subject_v2_session_fk') THEN
  ALTER TABLE knowledge_accepted_answers_subject_v2 ADD CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk
   FOREIGN KEY(issuer,subject_id,session_id)
   REFERENCES knowledge_answer_sessions_subject_v2(issuer,subject_id,session_id) NOT VALID;
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_product_search_operations_subject_v2'::regclass
  AND conname='knowledge_product_search_operations_subject_v2_legacy_fk') THEN
  ALTER TABLE knowledge_product_search_operations_subject_v2 ADD CONSTRAINT knowledge_product_search_operations_subject_v2_legacy_fk
   FOREIGN KEY(issuer,tenant_id,subject_id,session_id,operation_key)
   REFERENCES knowledge_product_search_operations(authority_id,tenant_id,subject_id,session_id,operation_key) NOT VALID;
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='knowledge_tool_parents_subject_v2'::regclass
  AND conname='knowledge_tool_parents_subject_v2_legacy_fk') THEN
  ALTER TABLE knowledge_tool_parents_subject_v2 ADD CONSTRAINT knowledge_tool_parents_subject_v2_legacy_fk
   FOREIGN KEY(issuer,tenant_id,subject_id,session_id,operation_key)
   REFERENCES knowledge_tool_parents(authority_id,tenant_id,subject_id,session_id,operation_key) NOT VALID;
 END IF;
END $$;
ALTER TABLE knowledge_answer_sessions_subject_v2 VALIDATE CONSTRAINT knowledge_answer_sessions_subject_v2_legacy_fk;
ALTER TABLE knowledge_accepted_answers_subject_v2 VALIDATE CONSTRAINT knowledge_accepted_answers_subject_v2_legacy_fk;
ALTER TABLE knowledge_accepted_answers_subject_v2 VALIDATE CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk;
ALTER TABLE knowledge_product_search_operations_subject_v2 VALIDATE CONSTRAINT knowledge_product_search_operations_subject_v2_legacy_fk;
ALTER TABLE knowledge_tool_parents_subject_v2 VALIDATE CONSTRAINT knowledge_tool_parents_subject_v2_legacy_fk;

-- An IF NOT EXISTS replay must reject a preexisting object with the same name
-- but different shape, instead of silently accepting a broken catalog.
CREATE TEMP TABLE knowledge_subject_v2_check_reference (
 issuer text, tenant_id text, subject_id text,
 CONSTRAINT knowledge_subject_v2_check_reference_identity_ck CHECK (
  issuer='rtw.identity' AND tenant_id='platform' AND
  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
) ON COMMIT DROP;
DO $$
DECLARE spec record; actual text; reference_check text;
BEGIN
 SELECT pg_get_constraintdef(oid) INTO reference_check FROM pg_constraint
  WHERE conrelid='knowledge_subject_v2_check_reference'::regclass
   AND conname='knowledge_subject_v2_check_reference_identity_ck';
 FOR spec IN SELECT * FROM (VALUES
  ('knowledge_answer_sessions_subject_v2',
   'issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0'),
  ('knowledge_accepted_answers_subject_v2',
   'answer_id:text:1:0:0:0,issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,accepted_ordinal:bigint:1:0:0:0'),
  ('knowledge_product_search_operations_subject_v2',
   'issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,operation_key:text:1:0:0:0'),
  ('knowledge_tool_parents_subject_v2',
   'issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,operation_key:text:1:0:0:0')
 ) AS x(relation_name,column_shape) LOOP
  IF (SELECT relkind FROM pg_class WHERE oid=to_regclass(spec.relation_name)) <> 'r' THEN
   RAISE EXCEPTION 'v2 sidecar relation shape changed: %',spec.relation_name;
  END IF;
  SELECT string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||
   CASE WHEN a.attnotnull THEN '1' ELSE '0' END||':'||
   CASE WHEN EXISTS (SELECT 1 FROM pg_attrdef d WHERE d.adrelid=a.attrelid AND d.adnum=a.attnum)
    THEN '1' ELSE '0' END||':'||
   CASE WHEN a.attgenerated='' THEN '0' ELSE '1' END||':'||
   CASE WHEN a.attidentity='' THEN '0' ELSE '1' END,',' ORDER BY a.attnum)
   INTO actual FROM pg_attribute a WHERE a.attrelid=to_regclass(spec.relation_name)
   AND a.attnum>0 AND NOT a.attisdropped;
  IF actual IS DISTINCT FROM spec.column_shape THEN
   RAISE EXCEPTION 'v2 sidecar column shape changed: %',spec.relation_name;
  END IF;
  SELECT pg_get_constraintdef(oid) INTO actual FROM pg_constraint
   WHERE conrelid=to_regclass(spec.relation_name)
    AND conname=spec.relation_name||'_identity_ck' AND contype='c' AND convalidated;
  IF actual IS DISTINCT FROM reference_check THEN
   RAISE EXCEPTION 'v2 sidecar identity CHECK changed: %',spec.relation_name;
  END IF;
 END LOOP;
 -- All expected PK/UQ/FK constraints must be validated and match their exact
 -- legacy/v2 column lists, parent relation and NO ACTION semantics.
 FOR spec IN SELECT * FROM (VALUES
  ('knowledge_answer_sessions_subject_v2','knowledge_answer_sessions_subject_v2_pkey','p',
   ARRAY['issuer','subject_id','session_id']::text[],'',ARRAY[]::text[]),
  ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_pkey','p',
   ARRAY['answer_id']::text[],'',ARRAY[]::text[]),
  ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_v2_uq','u',
   ARRAY['issuer','subject_id','session_id','accepted_ordinal']::text[],'',ARRAY[]::text[]),
  ('knowledge_product_search_operations_subject_v2','knowledge_product_search_operations_subject_v2_pkey','p',
   ARRAY['issuer','subject_id','session_id','operation_key']::text[],'',ARRAY[]::text[]),
  ('knowledge_tool_parents_subject_v2','knowledge_tool_parents_subject_v2_pkey','p',
   ARRAY['issuer','subject_id','session_id','operation_key']::text[],'',ARRAY[]::text[]),
  ('knowledge_answer_sessions_subject_v2','knowledge_answer_sessions_subject_v2_legacy_fk','f',
   ARRAY['issuer','tenant_id','subject_id','session_id']::text[],'knowledge_answer_sessions',
   ARRAY['authority_id','tenant_id','subject_id','session_id']::text[]),
  ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_legacy_fk','f',
   ARRAY['answer_id','issuer','tenant_id','subject_id','session_id','accepted_ordinal']::text[],'knowledge_accepted_answers',
   ARRAY['answer_id','authority_id','tenant_id','subject_id','session_id','accepted_ordinal']::text[]),
  ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_session_fk','f',
   ARRAY['issuer','subject_id','session_id']::text[],'knowledge_answer_sessions_subject_v2',
   ARRAY['issuer','subject_id','session_id']::text[]),
  ('knowledge_product_search_operations_subject_v2','knowledge_product_search_operations_subject_v2_legacy_fk','f',
   ARRAY['issuer','tenant_id','subject_id','session_id','operation_key']::text[],'knowledge_product_search_operations',
   ARRAY['authority_id','tenant_id','subject_id','session_id','operation_key']::text[]),
  ('knowledge_tool_parents_subject_v2','knowledge_tool_parents_subject_v2_legacy_fk','f',
   ARRAY['issuer','tenant_id','subject_id','session_id','operation_key']::text[],'knowledge_tool_parents',
   ARRAY['authority_id','tenant_id','subject_id','session_id','operation_key']::text[])
 ) AS x(relation_name,constraint_name,constraint_type,key_columns,parent_name,parent_columns) LOOP
  IF NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conrelid=to_regclass(spec.relation_name)
   AND c.conname=spec.constraint_name AND c.contype=spec.constraint_type::char AND c.convalidated
   AND NOT c.condeferrable AND (spec.parent_name='' OR
    (c.confrelid=to_regclass(spec.parent_name) AND c.confupdtype='a' AND c.confdeltype='a'
     AND c.confmatchtype='s' AND
     (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
      FROM unnest(c.confkey) WITH ORDINALITY k(attnum,ordinality)
      JOIN pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum)=spec.parent_columns))
   AND (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
        FROM unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality)
        JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)=spec.key_columns
   AND (c.contype='f' OR EXISTS (SELECT 1 FROM pg_index i WHERE i.indexrelid=c.conindid
        AND i.indisvalid AND i.indisready AND i.indimmediate))) THEN
   RAISE EXCEPTION 'v2 sidecar constraint changed: %.%',spec.relation_name,spec.constraint_name;
  END IF;
 END LOOP;
 IF NOT EXISTS (SELECT 1 FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
   WHERE idx.relname='knowledge_accepted_answers_v2_anchor_uq'
   AND i.indrelid='knowledge_accepted_answers'::regclass AND i.indisunique AND i.indisvalid
   AND i.indisready AND i.indimmediate AND i.indpred IS NULL AND i.indexprs IS NULL
   AND (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
        FROM unnest(i.indkey) WITH ORDINALITY k(attnum,ordinality)
        JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum)=
        ARRAY['answer_id','authority_id','tenant_id','subject_id','session_id','accepted_ordinal']::text[]) THEN
  RAISE EXCEPTION 'v2 legacy answer anchor index changed';
 END IF;
END $$;

-- Old writers are blocked by the SHARE table lock until this one transaction
-- commits. Any invalid legacy row or projected-key collision aborts everything;
-- no historical slot is omitted or mapped by guessing.
INSERT INTO knowledge_answer_sessions_subject_v2(issuer,tenant_id,subject_id,session_id)
 SELECT authority_id,tenant_id,subject_id,session_id FROM knowledge_answer_sessions
 ON CONFLICT DO NOTHING;
INSERT INTO knowledge_accepted_answers_subject_v2
 (answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal)
 SELECT answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal
 FROM knowledge_accepted_answers ON CONFLICT DO NOTHING;
INSERT INTO knowledge_product_search_operations_subject_v2
 (issuer,tenant_id,subject_id,session_id,operation_key)
 SELECT authority_id,tenant_id,subject_id,session_id,operation_key
 FROM knowledge_product_search_operations ON CONFLICT DO NOTHING;
INSERT INTO knowledge_tool_parents_subject_v2
 (issuer,tenant_id,subject_id,session_id,operation_key)
 SELECT authority_id,tenant_id,subject_id,session_id,operation_key
 FROM knowledge_tool_parents ON CONFLICT DO NOTHING;

DO $$
BEGIN
 IF EXISTS (SELECT authority_id,tenant_id,subject_id,session_id FROM knowledge_answer_sessions
  EXCEPT SELECT issuer,tenant_id,subject_id,session_id FROM knowledge_answer_sessions_subject_v2) OR
    EXISTS (SELECT issuer,tenant_id,subject_id,session_id FROM knowledge_answer_sessions_subject_v2
  EXCEPT SELECT authority_id,tenant_id,subject_id,session_id FROM knowledge_answer_sessions) THEN
  RAISE EXCEPTION 'v2 session sidecar does not exactly mirror v1 keys';
 END IF;
 IF EXISTS (SELECT answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal
   FROM knowledge_accepted_answers
  EXCEPT SELECT answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal
   FROM knowledge_accepted_answers_subject_v2) OR
    EXISTS (SELECT answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal
   FROM knowledge_accepted_answers_subject_v2
  EXCEPT SELECT answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal
   FROM knowledge_accepted_answers) THEN
  RAISE EXCEPTION 'v2 accepted sidecar does not exactly mirror v1 keys';
 END IF;
 IF EXISTS (SELECT authority_id,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_product_search_operations
  EXCEPT SELECT issuer,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_product_search_operations_subject_v2) OR
    EXISTS (SELECT issuer,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_product_search_operations_subject_v2
  EXCEPT SELECT authority_id,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_product_search_operations) THEN
  RAISE EXCEPTION 'v2 product operation sidecar does not exactly mirror v1 keys';
 END IF;
 IF EXISTS (SELECT authority_id,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_tool_parents
  EXCEPT SELECT issuer,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_tool_parents_subject_v2) OR
    EXISTS (SELECT issuer,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_tool_parents_subject_v2
  EXCEPT SELECT authority_id,tenant_id,subject_id,session_id,operation_key
   FROM knowledge_tool_parents) THEN
  RAISE EXCEPTION 'v2 Tool parent sidecar does not exactly mirror v1 keys';
 END IF;
END $$;
COMMIT;
