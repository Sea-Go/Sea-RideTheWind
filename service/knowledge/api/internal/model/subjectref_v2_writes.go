package model

import (
	"context"
	"errors"
	"regexp"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var localSubjectRefV2Nonce = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CheckContinuousSubjectRefV2Writes keeps this candidate confined to a fresh,
// DB-owner-marked disposable PostgreSQL instance. The marker is provisioned by
// the migration test owner, never by the Knowledge service. A production online
// expand/watermark contract must replace this local gate before any release.
func (s *Store) CheckContinuousSubjectRefV2Writes(ctx context.Context, nonce string) error {
	s.continuousSubjectRefV2Ready = false
	if !s.continuousSubjectRefV2Writes || !localSubjectRefV2Nonce.MatchString(nonce) {
		return invalid("explicit local SubjectRef v2 write option and nonce required")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// A temporary canonical CHECK uses the same expression as the guarded
	// offline migration. pg_get_constraintdef then compares all four real
	// constraints without depending on their different column numbers.
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE knowledge_subject_v2_runtime_reference (
	 issuer text, tenant_id text, subject_id text,
	 CONSTRAINT knowledge_subject_v2_runtime_reference_identity_ck CHECK (
	  issuer='rtw.identity' AND tenant_id='platform' AND
	  CASE WHEN subject_id ~ '^[1-9][0-9]*$' AND length(subject_id)<=19
	   THEN subject_id::numeric<=9223372036854775807 ELSE false END)
	 ) ON COMMIT DROP`)
	if err != nil {
		return err
	}
	var ready bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.knowledge_subjectref_v2_local_test_gate
	 WHERE nonce=$1 AND created_at BETWEEN clock_timestamp()-interval '1 hour' AND clock_timestamp())
	 AND current_database()='postgres' AND current_user='sea_knowledge_test'
	 AND inet_server_addr()='127.0.0.1'::inet AND inet_client_addr()='127.0.0.1'::inet
	 AND to_regclass('knowledge_answer_sessions_subject_v2') IS NOT NULL
	 AND to_regclass('knowledge_accepted_answers_subject_v2') IS NOT NULL
	 AND to_regclass('knowledge_product_search_operations_subject_v2') IS NOT NULL
	 AND to_regclass('knowledge_tool_parents_subject_v2') IS NOT NULL`, nonce).Scan(&ready)
	if err != nil || !ready {
		return v2StorageUnavailable("marked local DB and candidate DDL required")
	}
	// Match each constraint to the *active* relation and exact keys/parent.
	// Counting names globally allows an unrelated schema to spoof readiness.
	err = tx.QueryRow(ctx, `WITH specs(relation_name,constraint_name,constraint_type,
	 key_columns,parent_name,parent_columns) AS (VALUES
	 ('knowledge_answer_sessions_subject_v2','knowledge_answer_sessions_subject_v2_identity_ck','c',ARRAY[]::text[],'',ARRAY[]::text[]),
	 ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_identity_ck','c',ARRAY[]::text[],'',ARRAY[]::text[]),
	 ('knowledge_product_search_operations_subject_v2','knowledge_product_search_operations_subject_v2_identity_ck','c',ARRAY[]::text[],'',ARRAY[]::text[]),
	 ('knowledge_tool_parents_subject_v2','knowledge_tool_parents_subject_v2_identity_ck','c',ARRAY[]::text[],'',ARRAY[]::text[]),
	 ('knowledge_answer_sessions_subject_v2','knowledge_answer_sessions_subject_v2_pkey','p',ARRAY['issuer','subject_id','session_id']::text[],'',ARRAY[]::text[]),
	 ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_pkey','p',ARRAY['answer_id']::text[],'',ARRAY[]::text[]),
	 ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_v2_uq','u',ARRAY['issuer','subject_id','session_id','accepted_ordinal']::text[],'',ARRAY[]::text[]),
	 ('knowledge_product_search_operations_subject_v2','knowledge_product_search_operations_subject_v2_pkey','p',ARRAY['issuer','subject_id','session_id','operation_key']::text[],'',ARRAY[]::text[]),
	 ('knowledge_tool_parents_subject_v2','knowledge_tool_parents_subject_v2_pkey','p',ARRAY['issuer','subject_id','session_id','operation_key']::text[],'',ARRAY[]::text[]),
	 ('knowledge_answer_sessions_subject_v2','knowledge_answer_sessions_subject_v2_legacy_fk','f',ARRAY['issuer','tenant_id','subject_id','session_id']::text[],'knowledge_answer_sessions',ARRAY['authority_id','tenant_id','subject_id','session_id']::text[]),
	 ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_legacy_fk','f',ARRAY['answer_id','issuer','tenant_id','subject_id','session_id','accepted_ordinal']::text[],'knowledge_accepted_answers',ARRAY['answer_id','authority_id','tenant_id','subject_id','session_id','accepted_ordinal']::text[]),
	 ('knowledge_accepted_answers_subject_v2','knowledge_accepted_answers_subject_v2_session_fk','f',ARRAY['issuer','subject_id','session_id']::text[],'knowledge_answer_sessions_subject_v2',ARRAY['issuer','subject_id','session_id']::text[]),
	 ('knowledge_product_search_operations_subject_v2','knowledge_product_search_operations_subject_v2_legacy_fk','f',ARRAY['issuer','tenant_id','subject_id','session_id','operation_key']::text[],'knowledge_product_search_operations',ARRAY['authority_id','tenant_id','subject_id','session_id','operation_key']::text[]),
	 ('knowledge_tool_parents_subject_v2','knowledge_tool_parents_subject_v2_legacy_fk','f',ARRAY['issuer','tenant_id','subject_id','session_id','operation_key']::text[],'knowledge_tool_parents',ARRAY['authority_id','tenant_id','subject_id','session_id','operation_key']::text[])
	), canonical AS (SELECT pg_get_constraintdef(c.oid) definition FROM pg_constraint c
	 WHERE c.conrelid='knowledge_subject_v2_runtime_reference'::regclass
	 AND c.conname='knowledge_subject_v2_runtime_reference_identity_ck')
	SELECT count(*)=14 FROM specs x JOIN pg_constraint c
	 ON c.conrelid=to_regclass(x.relation_name) AND c.conname=x.constraint_name
	 AND c.contype=x.constraint_type::char AND c.convalidated AND NOT c.condeferrable
	WHERE (c.contype<>'c' OR pg_get_constraintdef(c.oid)=(SELECT definition FROM canonical))
	 AND (c.contype='c' OR (
	  (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
	   FROM unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality)
	   JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)=x.key_columns
	  AND (c.contype='f' OR EXISTS (SELECT 1 FROM pg_index i WHERE i.indexrelid=c.conindid
	   AND i.indisvalid AND i.indisready AND i.indimmediate))
	 ))
	 AND (c.contype<>'f' OR (
	  c.confrelid=to_regclass(x.parent_name) AND c.confupdtype='a' AND c.confdeltype='a'
	  AND c.confmatchtype='s' AND
	  (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
	   FROM unnest(c.confkey) WITH ORDINALITY k(attnum,ordinality)
	   JOIN pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum)=x.parent_columns
	 ))`).Scan(&ready)
	if err != nil || !ready {
		return v2StorageUnavailable("sidecar catalog differs from DB-owner candidate")
	}
	err = tx.QueryRow(ctx, `WITH specs(relation_name,column_shape) AS (VALUES
	 ('knowledge_answer_sessions_subject_v2','issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0'),
	 ('knowledge_accepted_answers_subject_v2','answer_id:text:1:0:0:0,issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,accepted_ordinal:bigint:1:0:0:0'),
	 ('knowledge_product_search_operations_subject_v2','issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,operation_key:text:1:0:0:0'),
	 ('knowledge_tool_parents_subject_v2','issuer:text:1:0:0:0,tenant_id:text:1:0:0:0,subject_id:text:1:0:0:0,session_id:text:1:0:0:0,operation_key:text:1:0:0:0')
	) SELECT count(*)=4 FROM specs x JOIN pg_class r ON r.oid=to_regclass(x.relation_name)
	 AND r.relkind='r' JOIN LATERAL (
	 SELECT string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||
	  CASE WHEN a.attnotnull THEN '1' ELSE '0' END||':'||
	  CASE WHEN EXISTS (SELECT 1 FROM pg_attrdef d WHERE d.adrelid=a.attrelid AND d.adnum=a.attnum)
	   THEN '1' ELSE '0' END||':'||
	  CASE WHEN a.attgenerated='' THEN '0' ELSE '1' END||':'||
	  CASE WHEN a.attidentity='' THEN '0' ELSE '1' END,',' ORDER BY a.attnum) shape
	 FROM pg_attribute a WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped
	) columns ON columns.shape=x.column_shape`).Scan(&ready)
	if err != nil || !ready {
		return v2StorageUnavailable("sidecar columns differ from DB-owner candidate")
	}
	s.continuousSubjectRefV2Ready = true
	return nil
}

func (s *Store) requireContinuousSubjectRefV2Ready() error {
	if !s.continuousSubjectRefV2Ready {
		return v2StorageUnavailable("successful local DB-owner startup gate required")
	}
	return nil
}

func v2StorageUnavailable(why string) error {
	return &reasonError{Cause: ErrUnavailable, Code: "SUBJECTREF_V2_STORAGE_UNAVAILABLE", Message: why}
}

func v2WriteSubject(subject types.AcceptedSubjectRef) error {
	if !validProductHistorySubject(subject) {
		return invalid("canonical RTW identity and positive UID required for v2 storage")
	}
	return nil
}

func v2ProjectionError(err error) error {
	if err == nil {
		return nil
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		switch pgerr.Code {
		case "23503", "23505", "23514":
			return conflictCode("SUBJECTREF_V2_PROJECTION_CONFLICT", "v2 sidecar conflicts with the original owner or key")
		case "42P01", "42703":
			return v2StorageUnavailable("candidate DDL missing")
		}
	}
	return err
}

// Every projector executes after its original INSERT and before that same
// transaction commits. ON CONFLICT is followed by an exact row check; neither
// a lost reply nor a preexisting wrong projection may hide a conflict.
func projectV2Session(ctx context.Context, tx pgx.Tx, subject types.AcceptedSubjectRef, session string) error {
	if err := v2WriteSubject(subject); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO knowledge_answer_sessions_subject_v2
	 (issuer,tenant_id,subject_id,session_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		subject.AuthorityId, subject.TenantId, subject.SubjectId, session)
	if err != nil {
		return v2ProjectionError(err)
	}
	var issuer, tenant, uid, storedSession string
	err = tx.QueryRow(ctx, `SELECT issuer,tenant_id,subject_id,session_id
	 FROM knowledge_answer_sessions_subject_v2
	 WHERE issuer=$1 AND subject_id=$2 AND session_id=$3 FOR SHARE`,
		subject.AuthorityId, subject.SubjectId, session).Scan(&issuer, &tenant, &uid, &storedSession)
	if err != nil {
		return v2ProjectionReadError(err)
	}
	if issuer != subject.AuthorityId || tenant != subject.TenantId || uid != subject.SubjectId || storedSession != session {
		return conflictCode("SUBJECTREF_V2_PROJECTION_CONFLICT", "session sidecar differs from original owner")
	}
	return nil
}

func projectV2Answer(ctx context.Context, tx pgx.Tx, answerID string, subject types.AcceptedSubjectRef,
	session string, ordinal int64) error {
	if err := v2WriteSubject(subject); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO knowledge_accepted_answers_subject_v2
	 (answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal)
	 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
		answerID, subject.AuthorityId, subject.TenantId, subject.SubjectId, session, ordinal)
	if err != nil {
		return v2ProjectionError(err)
	}
	var storedID, issuer, tenant, uid, storedSession string
	var storedOrdinal int64
	err = tx.QueryRow(ctx, `SELECT answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal
	 FROM knowledge_accepted_answers_subject_v2 WHERE answer_id=$1 FOR SHARE`, answerID).
		Scan(&storedID, &issuer, &tenant, &uid, &storedSession, &storedOrdinal)
	if err != nil {
		return v2ProjectionReadError(err)
	}
	if storedID != answerID || issuer != subject.AuthorityId || tenant != subject.TenantId ||
		uid != subject.SubjectId || storedSession != session || storedOrdinal != ordinal {
		return conflictCode("SUBJECTREF_V2_PROJECTION_CONFLICT", "answer sidecar differs from frozen owner and ordinal")
	}
	return nil
}

func projectV2Operation(ctx context.Context, tx pgx.Tx, table string,
	subject types.AcceptedSubjectRef, session, key string) error {
	if err := v2WriteSubject(subject); err != nil {
		return err
	}
	if table != "knowledge_product_search_operations_subject_v2" && table != "knowledge_tool_parents_subject_v2" {
		return invalid("unknown v2 operation projection")
	}
	_, err := tx.Exec(ctx, `INSERT INTO `+table+`
	 (issuer,tenant_id,subject_id,session_id,operation_key) VALUES($1,$2,$3,$4,$5)
	 ON CONFLICT DO NOTHING`, subject.AuthorityId, subject.TenantId, subject.SubjectId, session, key)
	if err != nil {
		return v2ProjectionError(err)
	}
	var issuer, tenant, uid, storedSession, storedKey string
	err = tx.QueryRow(ctx, `SELECT issuer,tenant_id,subject_id,session_id,operation_key FROM `+table+`
	 WHERE issuer=$1 AND subject_id=$2 AND session_id=$3 AND operation_key=$4 FOR SHARE`,
		subject.AuthorityId, subject.SubjectId, session, key).
		Scan(&issuer, &tenant, &uid, &storedSession, &storedKey)
	if err != nil {
		return v2ProjectionReadError(err)
	}
	if issuer != subject.AuthorityId || tenant != subject.TenantId || uid != subject.SubjectId ||
		storedSession != session || storedKey != key {
		return conflictCode("SUBJECTREF_V2_PROJECTION_CONFLICT", "operation sidecar differs from original owner")
	}
	return nil
}

func v2ProjectionReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return conflictCode("SUBJECTREF_V2_PROJECTION_CONFLICT", "v2 sidecar key belongs to another original row")
	}
	return v2ProjectionError(err)
}

// ensureV2Operation handles both a new reservation and a verified retry. The
// original operation and its projection commit together; a mismatched replay
// or missing original row aborts without accepting a sidecar.
func (s *Store) ensureV2Operation(ctx context.Context, table string, subject types.AcceptedSubjectRef,
	session, key, originalField, expected, insertSQL string, insertArgs ...any) error {
	if err := v2WriteSubject(subject); err != nil {
		return err
	}
	if (table != "knowledge_product_search_operations_subject_v2" || originalField != "request_hash") &&
		(table != "knowledge_tool_parents_subject_v2" || originalField != "module_id") {
		return invalid("unknown v2 operation binding")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if insertSQL != "" {
		if _, err = tx.Exec(ctx, insertSQL, insertArgs...); err != nil {
			return v2ProjectionError(err)
		}
	}
	var stored string
	originalTable := table[:len(table)-len("_subject_v2")]
	err = tx.QueryRow(ctx, `SELECT `+originalField+` FROM `+originalTable+`
	 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4
	 AND operation_key=$5 FOR SHARE`, subject.AuthorityId, subject.TenantId,
		subject.SubjectId, session, key).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflictCode("IDEMPOTENCY_CONFLICT", "original operation key not present after reservation")
	}
	if err != nil {
		return err
	}
	if stored != expected {
		return conflictCode("IDEMPOTENCY_CONFLICT", "operation key reused with different original request")
	}
	if err = projectV2Operation(ctx, tx, table, subject, session, key); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
