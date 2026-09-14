package model_test

import (
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

func TestProductSearchOperationFixedSnapshotLeaseAndReplay(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "9123"}
	input := model.ProductSearchInput{ModuleID: f.module.Id, Query: "What is the evidence?", Depth: "fast", Intelligence: "low"}
	const session, key = "product-operation-session", "product-operation-key"
	first, err := s.ReserveProductSearch(ctx, subject, session, key, input)
	first = must(t, first, err)
	if first.Status != "pending" || first.SearchID == "" || first.AnswerID == "" ||
		first.Snapshot.ReleaseId != f.release.ReleaseId || first.Snapshot.PublicationRevision != "1" {
		t.Fatalf("new search did not bind current manual publication: %+v", first)
	}
	var parallel [4]model.ProductSearchOperation
	var errs [4]error
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			parallel[i], errs[i] = s.ReserveProductSearch(ctx, subject, session, key, input)
		}(i)
	}
	wg.Wait()
	for i := range parallel {
		if errs[i] != nil || parallel[i].SearchID != first.SearchID || parallel[i].AnswerID != first.AnswerID ||
			!reflect.DeepEqual(parallel[i].Snapshot, first.Snapshot) {
			t.Fatalf("concurrent key did not converge: %+v %v", parallel[i], errs[i])
		}
	}
	changed := input
	changed.Query = "A different question"
	_, err = s.ReserveProductSearch(ctx, subject, session, key, changed)
	expectError(t, err, model.ErrConflict)
	claimed, yes, err := s.ClaimProductSearch(ctx, first)
	claimed = must(t, claimed, err)
	if !yes || claimed.Status != "running" || claimed.LeaseToken == "" || claimed.Attempt != 1 {
		t.Fatalf("first dispatch did not claim lease: %+v claimed=%v", claimed, yes)
	}
	busy, yes, err := s.ClaimProductSearch(ctx, first)
	busy = must(t, busy, err)
	if yes || busy.LeaseToken != claimed.LeaseToken || busy.Attempt != 1 {
		t.Fatalf("second dispatch bypassed active lease: %+v claimed=%v", busy, yes)
	}
	if err = s.CompleteProductSearch(ctx, claimed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("unaccepted answer marked committed: %v", err)
	}
	if err = s.FailProductSearch(ctx, claimed, "BTW_HTTP_502"); err != nil {
		t.Fatal(err)
	}
	reclaimed, yes, err := s.ClaimProductSearch(ctx, first)
	reclaimed = must(t, reclaimed, err)
	if !yes || reclaimed.Attempt != 2 || reclaimed.SearchID != first.SearchID || reclaimed.AnswerID != first.AnswerID ||
		reclaimed.LeaseToken == claimed.LeaseToken {
		t.Fatalf("failed operation did not retry with stable identity: %+v claimed=%v", reclaimed, yes)
	}
	other := subject
	other.SubjectId = "9124"
	_, err = s.GetProductSearchByID(ctx, other, session, first.SearchID)
	expectError(t, err, model.ErrNotFound)
	r2 := release(t, s, f.module, []string{f.source.RevisionId}, nil, "later-release")
	b2 := testenv.Ready(t, s, build(t, s, r2, "later-build"), r2)
	activate(t, s, f.module, r2, b2, 1)
	replayed, err := s.ReserveProductSearch(ctx, subject, session, key, input)
	replayed = must(t, replayed, err)
	if replayed.SearchID != first.SearchID || !reflect.DeepEqual(replayed.Snapshot, first.Snapshot) {
		t.Fatal("same key reselected current publication after pointer moved")
	}
	second, err := s.ReserveProductSearch(ctx, subject, session, "product-operation-key-2", input)
	second = must(t, second, err)
	if second.Snapshot.ReleaseId != r2.ReleaseId || second.Snapshot.PublicationRevision != "2" {
		t.Fatalf("new key did not select newer publication: %+v", second.Snapshot)
	}
}

func TestProductSearchMigrationProbeAndBadKeys(t *testing.T) {
	s := testenv.Store(t)
	if err := s.CheckProductSearchSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, "DROP TABLE knowledge_product_search_operations"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckProductSearchSchema(ctx); err == nil {
		t.Fatal("old production schema passed product search probe")
	}
	migration, err := os.ReadFile("../../../scripts/migrate-product-search.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, string(migration), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("operator migration failed on existing schema: %v", err)
	}
	if err = s.CheckProductSearchSchema(ctx); err != nil {
		t.Fatal(err)
	}
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "9123"}
	for _, key := range []string{"short", "with\ncontrol", "unicode-用户", "with space"} {
		_, err := s.GetProductSearchByKey(ctx, subject, "product-session", key)
		expectError(t, err, model.ErrInvalid)
	}
}
