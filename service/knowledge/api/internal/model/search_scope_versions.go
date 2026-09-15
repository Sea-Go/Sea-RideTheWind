package model

import (
	"context"
	"fmt"

	"sea-try-go/service/knowledge/api/internal/types"
)

// DetectSearchScopeVersions checks the active relation catalog against a
// canonical local reference in a rolled-back transaction. Both old tables
// without a version column continue to mean v1. A partly expanded or
// weakened contract fails closed before the service begins v2 traffic.
func (s *Store) DetectSearchScopeVersions(ctx context.Context) error {
	s.scopeVersionReady = false
	var columns int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM (VALUES
  (to_regclass('knowledge_product_search_operations')),
  (to_regclass('knowledge_tool_parents'))) r(relation_id)
 JOIN pg_attribute a ON a.attrelid=r.relation_id AND a.attname='scope_version'
 WHERE a.attnum>0 AND NOT a.attisdropped`).Scan(&columns)
	if err != nil {
		return fmt.Errorf("scope version catalog unavailable: %w", err)
	}
	if columns == 0 {
		return nil
	}
	if columns != 2 {
		return v2StorageUnavailable("search scope version tables are only partly expanded")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE knowledge_scope_version_runtime_reference (
  scope_version text NOT NULL DEFAULT 'v1',
  CONSTRAINT knowledge_scope_version_runtime_reference_ck CHECK(scope_version IN ('v1','v2'))
 ) ON COMMIT DROP`)
	if err != nil {
		return err
	}
	var exact bool
	err = tx.QueryRow(ctx, `WITH expected(relation_id,constraint_name) AS (VALUES
  (to_regclass('knowledge_product_search_operations'), 'knowledge_product_search_scope_version_ck'),
  (to_regclass('knowledge_tool_parents'), 'knowledge_tool_parent_scope_version_ck')),
 reference AS (
  SELECT a.atttypid column_type, pg_get_expr(d.adbin,d.adrelid) default_expr,
   pg_get_constraintdef(c.oid) check_expr
  FROM pg_attribute a
  JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  JOIN pg_constraint c ON c.conrelid=a.attrelid
   AND c.conname='knowledge_scope_version_runtime_reference_ck'
  WHERE a.attrelid='knowledge_scope_version_runtime_reference'::regclass
   AND a.attname='scope_version')
 SELECT count(*)=2 FROM expected x
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
  AND c.conkey=ARRAY[a.attnum]::smallint[]`).Scan(&exact)
	if err != nil || !exact {
		return v2StorageUnavailable("active scope version type, v1 default or exact v1/v2 CHECK differs from owner DDL")
	}
	s.scopeVersionReady = true
	return nil
}

func (s *Store) RequireSearchScopeVersions() error {
	if !s.scopeVersionReady || !s.continuousSubjectRefV2Ready {
		return v2StorageUnavailable("marked local v2 write gate and pinned scope schema required")
	}
	return nil
}

func (s *Store) ProductSearchScopeVersion(ctx context.Context, op ProductSearchOperation) (string, error) {
	if !s.scopeVersionReady {
		return "v1", nil
	}
	var version string
	err := s.DB.QueryRow(ctx, `SELECT scope_version FROM knowledge_product_search_operations
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND search_id=$5`,
		op.Subject.AuthorityId, op.Subject.TenantId, op.Subject.SubjectId, op.SessionID, op.SearchID).Scan(&version)
	if err != nil || (version != "v1" && version != "v2") {
		return "", v2StorageUnavailable("pinned product scope version unavailable")
	}
	return version, nil
}

func (s *Store) ToolParentScopeVersion(ctx context.Context, subject types.AcceptedSubjectRef, session, operationID string) (string, error) {
	if !s.scopeVersionReady {
		return "v1", nil
	}
	var version string
	err := s.DB.QueryRow(ctx, `SELECT scope_version FROM knowledge_tool_parents
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND operation_id=$5`,
		subject.AuthorityId, subject.TenantId, subject.SubjectId, session, operationID).Scan(&version)
	if err != nil || (version != "v1" && version != "v2") {
		return "", v2StorageUnavailable("pinned Tool scope version unavailable")
	}
	return version, nil
}
