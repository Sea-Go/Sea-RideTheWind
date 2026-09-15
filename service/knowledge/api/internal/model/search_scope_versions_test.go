package model_test

import (
	"errors"
	"os"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

func TestSearchScopeVersionCatalogRejectsWeakDDLBeforeSigner(t *testing.T) {
	store := testenv.Store(t)
	check := func(wantReady bool) {
		t.Helper()
		err := store.DetectSearchScopeVersions(ctx)
		if wantReady && err != nil {
			t.Fatalf("owner DDL was rejected: %v", err)
		}
		if !wantReady && !errors.Is(err, model.ErrUnavailable) {
			t.Fatalf("weakened DDL was accepted: %v", err)
		}
	}
	execDDL := func(sql string) {
		t.Helper()
		if _, err := store.DB.Exec(ctx, sql); err != nil {
			t.Fatalf("disposable PG catalog fixture %q: %v", sql, err)
		}
	}
	check(true)
	raw, err := os.ReadFile("../../../scripts/migrate-subjectref-v2-search-scopes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("clean reentrant owner DDL: %v", err)
	}
	check(true)
	execDDL(`ALTER TABLE knowledge_product_search_operations ALTER COLUMN scope_version SET DEFAULT 'v2'`)
	check(false)
	// The reentrant owner candidate cannot silently bless an old named
	// constraint or default; startup must still reject after it is reapplied.
	if _, err = store.DB.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err == nil {
		t.Fatal("candidate DDL accepted an existing v2 default")
	}
	check(false)
	execDDL(`ALTER TABLE knowledge_product_search_operations ALTER COLUMN scope_version SET DEFAULT 'v1'`)
	check(true)
	execDDL(`ALTER TABLE knowledge_product_search_operations DROP CONSTRAINT knowledge_product_search_scope_version_ck`)
	execDDL(`ALTER TABLE knowledge_product_search_operations ADD CONSTRAINT knowledge_product_search_scope_version_ck
 CHECK(scope_version IN ('v1','v2','v3'))`)
	check(false)
	if _, err = store.DB.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err == nil {
		t.Fatal("candidate DDL accepted an existing weak named CHECK")
	}
	check(false)
	execDDL(`ALTER TABLE knowledge_product_search_operations DROP CONSTRAINT knowledge_product_search_scope_version_ck`)
	execDDL(`ALTER TABLE knowledge_product_search_operations ADD CONSTRAINT knowledge_product_search_scope_version_ck
 CHECK(scope_version IN ('v1','v2'))`)
	check(true)
	execDDL(`ALTER TABLE knowledge_tool_parents ALTER COLUMN scope_version TYPE varchar USING scope_version::varchar`)
	check(false)
	execDDL(`ALTER TABLE knowledge_tool_parents ALTER COLUMN scope_version TYPE text USING scope_version::text`)
	check(true)
	execDDL(`ALTER TABLE knowledge_tool_parents ALTER COLUMN scope_version DROP NOT NULL`)
	check(false)
	execDDL(`ALTER TABLE knowledge_tool_parents ALTER COLUMN scope_version SET NOT NULL`)
	check(true)
	execDDL(`ALTER TABLE knowledge_product_search_operations DROP COLUMN scope_version`)
	check(false)
	execDDL(`ALTER TABLE knowledge_tool_parents DROP COLUMN scope_version`)
	if err := store.DetectSearchScopeVersions(ctx); err != nil {
		t.Fatalf("unexpanded v1 pair failed to retain old path: %v", err)
	}
	if err := store.RequireSearchScopeVersions(); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("old v1 pair acquired v2 signing capability: %v", err)
	}
	if _, err = store.DB.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("old v1 pair cannot apply candidate expand DDL: %v", err)
	}
	check(true)
}

func TestV2SearchScopePinsOneWirePerOperation(t *testing.T) {
	old := testenv.Store(t)
	fixture := makeCitationFixture(t, old)
	applyContinuousV2Candidate(t, old)
	store := continuousV2Store(old)
	markContinuousV2Ready(t, store)
	if err := store.DetectSearchScopeVersions(ctx); err != nil {
		t.Fatal(err)
	}
	owner := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "42"}
	other := owner
	other.SubjectId = "43"
	input := model.ProductSearchInput{ModuleID: fixture.module.Id,
		Query: "Where is Evidence?", Depth: "fast", Intelligence: "low"}
	const session = "same-logical-session"
	legacy, err := store.ReserveProductSearch(ctx, owner, session, "legacy-pinned-key", input, "v1")
	legacy = must(t, legacy, err)
	if got, err := store.ProductSearchScopeVersion(ctx, legacy); err != nil || got != "v1" {
		t.Fatalf("legacy version=%q error=%v", got, err)
	}
	legacyReplay, err := store.ReserveProductSearch(ctx, owner, session, "legacy-pinned-key", input, "v2")
	if err != nil || legacyReplay.SearchID != legacy.SearchID {
		t.Fatalf("v2 switch duplicated a legacy search: %+v %v", legacyReplay, err)
	}
	if got, err := store.ProductSearchScopeVersion(ctx, legacyReplay); err != nil || got != "v1" {
		t.Fatalf("legacy operation was reserialized as %q: %v", got, err)
	}
	v2, err := store.ReserveProductSearch(ctx, owner, session, "v2-pinned-key", input, "v2")
	v2 = must(t, v2, err)
	rolledBack, err := store.ReserveProductSearch(ctx, owner, session, "v2-pinned-key", input, "v1")
	if err != nil || rolledBack.SearchID != v2.SearchID || rolledBack.AnswerID != v2.AnswerID {
		t.Fatalf("rollback duplicated a v2 search: %+v %v", rolledBack, err)
	}
	if got, err := store.ProductSearchScopeVersion(ctx, rolledBack); err != nil || got != "v2" {
		t.Fatalf("v2 operation was reserialized as %q: %v", got, err)
	}
	otherV2, err := store.ReserveProductSearch(ctx, other, session, "v2-pinned-key", input, "v2")
	if err != nil || otherV2.SearchID == v2.SearchID {
		t.Fatalf("same session/key across two UIDs collided: %+v %v", otherV2, err)
	}
	parent, err := store.ReserveToolParent(ctx, owner, session, "tool-pinned-key", fixture.module.Id, "v2")
	parent = must(t, parent, err)
	parentReplay, err := store.ReserveToolParent(ctx, owner, session, "tool-pinned-key", fixture.module.Id, "v1")
	if err != nil || parentReplay.OperationID != parent.OperationID {
		t.Fatalf("Tool rollback duplicated parent: %+v %v", parentReplay, err)
	}
	if got, err := store.ToolParentScopeVersion(ctx, owner, session, parent.OperationID); err != nil || got != "v2" {
		t.Fatalf("Tool parent version=%q error=%v", got, err)
	}
	var operations, parents int
	if err := store.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_product_search_operations
 WHERE authority_id='rtw.identity' AND tenant_id='platform' AND subject_id='42' AND session_id=$1`, session).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_tool_parents
 WHERE authority_id='rtw.identity' AND tenant_id='platform' AND subject_id='42' AND session_id=$1`, session).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if operations != 2 || parents != 1 {
		t.Fatalf("one semantic operation produced duplicate DB effects: searches=%d parents=%d", operations, parents)
	}
	wrong := owner
	wrong.TenantId = "organization-x"
	if _, err := store.ReserveProductSearch(ctx, wrong, session, "wrong-slot-key", input, "v2"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("client-like tenant escaped before reservation: %v", err)
	}
}
