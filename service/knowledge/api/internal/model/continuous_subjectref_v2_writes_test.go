package model_test

import (
	"context"
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

func applyContinuousV2Candidate(t *testing.T, store *model.Store) {
	t.Helper()
	raw, err := os.ReadFile("../../../scripts/migrate-subjectref-v2-storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(context.Background(), string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("DB-owner candidate migration: %v", err)
	}
}

func continuousV2Store(store *model.Store) *model.Store {
	return model.New(store.DB, store.Objects, model.WithContinuousSubjectRefV2Writes())
}

func markContinuousV2Ready(t *testing.T, store *model.Store) {
	t.Helper()
	nonce := os.Getenv("KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE")
	if nonce == "" {
		t.Skip("continuous writes require the isolated owner-provisioned PG marker")
	}
	if err := store.CheckContinuousSubjectRefV2Writes(context.Background(), nonce); err != nil {
		t.Fatalf("marked local DB-owner write gate: %v", err)
	}
}

func countV2Rows(t *testing.T, store *model.Store, table string, condition string, args ...any) int {
	t.Helper()
	var n int
	if err := store.DB.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+condition, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func outboxFingerprint(t *testing.T, store *model.Store) string {
	t.Helper()
	var fingerprint string
	if err := store.DB.QueryRow(context.Background(), `SELECT coalesce(md5(string_agg(
	 event_id||':'||payload::text,',' ORDER BY event_id)),'') FROM knowledge_outbox`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestContinuousSubjectRefV2AnswerReplayAndRollback(t *testing.T) {
	oldStore := testenv.Store(t)
	fixture := makeCitationFixture(t, oldStore)
	citation := makeCitationRequest(t, fixture, "continuous-history-search")
	receipt, err := oldStore.AcceptSearchCitations(context.Background(), citation)
	if err != nil {
		t.Fatal(err)
	}
	owner := canonicalHistoryRequest(t, receipt, citation, "continuous-owner-answer", "platform", "42")
	first, err := oldStore.CommitAcceptedAnswer(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	var originalTurn, originalHash string
	if err = oldStore.DB.QueryRow(context.Background(), `SELECT turn_json,turn_hash FROM knowledge_accepted_answers
	 WHERE answer_id=$1`, owner.AnswerId).Scan(&originalTurn, &originalHash); err != nil {
		t.Fatal(err)
	}
	oldOutbox := outboxFingerprint(t, oldStore)
	applyContinuousV2Candidate(t, oldStore)
	// The default Store still writes only v1. Removing a snapshot sidecar
	// simulates a lost projection before the owner begins continuous writes.
	if _, err = oldStore.DB.Exec(context.Background(), `DELETE FROM knowledge_accepted_answers_subject_v2
	 WHERE answer_id=$1`, owner.AnswerId); err != nil {
		t.Fatal(err)
	}
	if _, err = oldStore.DB.Exec(context.Background(), `DELETE FROM knowledge_answer_sessions_subject_v2
	 WHERE issuer=$1 AND subject_id=$2 AND session_id=$3`, owner.Subject.AuthorityId,
		owner.Subject.SubjectId, owner.SessionId); err != nil {
		t.Fatal(err)
	}
	continuous := continuousV2Store(oldStore)
	other := canonicalHistoryRequest(t, receipt, citation, "continuous-other-answer", "platform", "43")
	if _, err = continuous.CommitAcceptedAnswer(context.Background(), other); !errors.Is(err, model.ErrUnavailable) ||
		countV2Rows(t, continuous, "knowledge_accepted_answers", "answer_id=$1", other.AnswerId) != 0 {
		t.Fatalf("Option alone bypassed DB-owner gate: %v", err)
	}
	markContinuousV2Ready(t, continuous)
	replayed, err := continuous.CommitAcceptedAnswer(context.Background(), owner)
	if err != nil || replayed != first {
		t.Fatalf("missing-sidecar replay changed accepted receipt: %+v %v", replayed, err)
	}
	if countV2Rows(t, continuous, "knowledge_accepted_answers_subject_v2", "answer_id=$1", owner.AnswerId) != 1 ||
		countV2Rows(t, continuous, "knowledge_answer_sessions_subject_v2", "issuer=$1 AND subject_id=$2 AND session_id=$3",
			owner.Subject.AuthorityId, owner.Subject.SubjectId, owner.SessionId) != 1 {
		t.Fatal("verified replay did not repair exactly one answer and session projection")
	}
	second, err := continuous.CommitAcceptedAnswer(context.Background(), other)
	if err != nil || second.AcceptedOrdinal != 1 || first.AcceptedOrdinal != 1 {
		t.Fatalf("two real positive UIDs sharing session did not stay distinct: %+v %+v %v", first, second, err)
	}
	var got [4]types.AcceptedAnswer
	var errs [4]error
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = continuous.CommitAcceptedAnswer(context.Background(), other)
		}(i)
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil || got[i] != second {
			t.Fatalf("lost receipt retry %d: %+v %v", i, got[i], errs[i])
		}
	}
	if countV2Rows(t, continuous, "knowledge_accepted_answers_subject_v2", "answer_id=$1", other.AnswerId) != 1 {
		t.Fatal("parallel retries duplicated sidecar")
	}
	collision := canonicalHistoryRequest(t, receipt, citation, owner.AnswerId, "platform", "43")
	if _, err = continuous.CommitAcceptedAnswer(context.Background(), collision); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("another UID reused frozen AnswerID: %v", err)
	}
	blocked := canonicalHistoryRequest(t, receipt, citation, "blocked-answer", "platform", "44")
	if _, err = continuous.DB.Exec(context.Background(), `CREATE TABLE injected_answer_owner(answer_id text PRIMARY KEY);
	 ALTER TABLE knowledge_accepted_answers_subject_v2
	 ADD CONSTRAINT injected_answer_denial_fk FOREIGN KEY(answer_id)
	 REFERENCES injected_answer_owner(answer_id) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err = continuous.CommitAcceptedAnswer(context.Background(), blocked); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("sidecar FK failed to roll back original accepted turn: %v", err)
	}
	if countV2Rows(t, continuous, "knowledge_accepted_answers", "answer_id=$1", blocked.AnswerId) != 0 ||
		countV2Rows(t, continuous, "knowledge_answer_sessions", "subject_id=$1 AND session_id=$2", "44", blocked.SessionId) != 0 {
		t.Fatal("sidecar rejection committed the original answer or session")
	}
	if _, err = continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers_subject_v2
	 DROP CONSTRAINT injected_answer_denial_fk`); err != nil {
		t.Fatal(err)
	}
	if _, err = continuous.CommitAcceptedAnswer(context.Background(), blocked); err != nil {
		t.Fatalf("same operation retry after rollback: %v", err)
	}
	var afterTurn, afterHash string
	if err = continuous.DB.QueryRow(context.Background(), `SELECT turn_json,turn_hash FROM knowledge_accepted_answers
	 WHERE answer_id=$1`, owner.AnswerId).Scan(&afterTurn, &afterHash); err != nil {
		t.Fatal(err)
	}
	if afterTurn != originalTurn || afterHash != originalHash || outboxFingerprint(t, continuous) != oldOutbox {
		t.Fatal("frozen old turn/hash or outbox changed while projecting v2")
	}
	archive := canonicalHistoryRequest(t, receipt, citation, "archive-rejected-answer", "archive", "45")
	if _, err = continuous.CommitAcceptedAnswer(context.Background(), archive); !errors.Is(err, model.ErrInvalid) ||
		countV2Rows(t, continuous, "knowledge_accepted_answers", "answer_id=$1", archive.AnswerId) != 0 {
		t.Fatalf("old compatibility slot became a tenant namespace: %v", err)
	}
}

func TestContinuousSubjectRefV2ProductAndToolReservations(t *testing.T) {
	oldStore := testenv.Store(t)
	fixture := makeCitationFixture(t, oldStore)
	applyContinuousV2Candidate(t, oldStore)
	continuous := continuousV2Store(oldStore)
	input := model.ProductSearchInput{ModuleID: fixture.module.Id, Query: "Evidence?", Depth: "fast", Intelligence: "low"}
	const session, productKey, toolKey = "shared-session", "continuous-product-key", "continuous-tool-key"
	owner := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "42"}
	other := owner
	other.SubjectId = "43"
	if _, err := continuous.ReserveProductSearch(context.Background(), owner, session, productKey, input); !errors.Is(err, model.ErrUnavailable) ||
		countV2Rows(t, continuous, "knowledge_product_search_operations", "operation_key=$1", productKey) != 0 {
		t.Fatalf("product Option alone bypassed DB-owner gate: %v", err)
	}
	markContinuousV2Ready(t, continuous)
	ownerProduct, err := continuous.ReserveProductSearch(context.Background(), owner, session, productKey, input)
	if err != nil {
		t.Fatal(err)
	}
	otherProduct, err := continuous.ReserveProductSearch(context.Background(), other, session, productKey, input)
	if err != nil || otherProduct.SearchID == ownerProduct.SearchID {
		t.Fatalf("two UIDs shared a product operation: %+v %v", otherProduct, err)
	}
	ownerTool, err := continuous.ReserveToolParent(context.Background(), owner, session, toolKey, fixture.module.Id)
	if err != nil {
		t.Fatal(err)
	}
	otherTool, err := continuous.ReserveToolParent(context.Background(), other, session, toolKey, fixture.module.Id)
	if err != nil || otherTool.OperationID == ownerTool.OperationID {
		t.Fatalf("two UIDs shared a Tool parent: %+v %v", otherTool, err)
	}
	if countV2Rows(t, continuous, "knowledge_product_search_operations_subject_v2", "session_id=$1", session) != 2 ||
		countV2Rows(t, continuous, "knowledge_tool_parents_subject_v2", "session_id=$1", session) != 2 {
		t.Fatal("product/Tool operations were not projected once per real UID")
	}
	if _, err = continuous.DB.Exec(context.Background(), `DELETE FROM knowledge_product_search_operations_subject_v2
	 WHERE issuer=$1 AND subject_id=$2 AND session_id=$3 AND operation_key=$4`, owner.AuthorityId, owner.SubjectId, session, productKey); err != nil {
		t.Fatal(err)
	}
	if _, err = continuous.DB.Exec(context.Background(), `DELETE FROM knowledge_tool_parents_subject_v2
	 WHERE issuer=$1 AND subject_id=$2 AND session_id=$3 AND operation_key=$4`, owner.AuthorityId, owner.SubjectId, session, toolKey); err != nil {
		t.Fatal(err)
	}
	productReplay, err := continuous.ReserveProductSearch(context.Background(), owner, session, productKey, input)
	if err != nil || !reflect.DeepEqual(productReplay, ownerProduct) {
		t.Fatalf("lost product sidecar replay: %+v %v", productReplay, err)
	}
	toolReplay, err := continuous.ReserveToolParent(context.Background(), owner, session, toolKey, fixture.module.Id)
	if err != nil || !reflect.DeepEqual(toolReplay, ownerTool) {
		t.Fatalf("lost Tool sidecar replay: %+v %v", toolReplay, err)
	}
	changed := input
	changed.Query = "Different evidence"
	if _, err = continuous.ReserveProductSearch(context.Background(), owner, session, productKey, changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("different product request reused original key: %v", err)
	}
	if _, err = continuous.ReserveToolParent(context.Background(), owner, session, toolKey, "different-module"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("different Tool module reused original key: %v", err)
	}
	// A sidecar failure must roll back the one old operation row too.
	if _, err = continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_product_search_operations_subject_v2
	 ADD CONSTRAINT injected_product_denial_ck CHECK(operation_key <> 'blocked-product-key')`); err != nil {
		t.Fatal(err)
	}
	if _, err = continuous.ReserveProductSearch(context.Background(), owner, session, "blocked-product-key", input); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("product sidecar rejection: %v", err)
	}
	if countV2Rows(t, continuous, "knowledge_product_search_operations", "operation_key='blocked-product-key'") != 0 {
		t.Fatal("rejected product sidecar left an old business row")
	}
	if _, err = continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_product_search_operations_subject_v2
	 DROP CONSTRAINT injected_product_denial_ck`); err != nil {
		t.Fatal(err)
	}
	if _, err = continuous.ReserveProductSearch(context.Background(), owner, session, "blocked-product-key", input); err != nil {
		t.Fatalf("retry after rolled-back product operation: %v", err)
	}
	// Option-off fallback remains an old-only write after explicit DDL.
	late := owner
	late.SubjectId = "44"
	oldProduct, err := oldStore.ReserveProductSearch(context.Background(), late, session, productKey, input)
	if err != nil {
		t.Fatal(err)
	}
	if countV2Rows(t, continuous, "knowledge_product_search_operations_subject_v2", "subject_id='44'") != 0 {
		t.Fatal("default-off writer unexpectedly projected product operation")
	}
	lateReplay, err := continuous.ReserveProductSearch(context.Background(), late, session, productKey, input)
	if err != nil || lateReplay.SearchID != oldProduct.SearchID ||
		countV2Rows(t, continuous, "knowledge_product_search_operations_subject_v2", "subject_id='44'") != 1 {
		t.Fatalf("verified late product replay did not repair original: %+v %v", lateReplay, err)
	}
}

func TestContinuousSubjectRefV2WriteGateRejectsSpoofedFK(t *testing.T) {
	nonce := os.Getenv("KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE")
	if nonce == "" {
		t.Skip("test-only PG marker nonce is supplied by the isolated runner")
	}
	store := testenv.Store(t)
	applyContinuousV2Candidate(t, store)
	continuous := continuousV2Store(store)
	if err := continuous.CheckContinuousSubjectRefV2Writes(context.Background(), nonce); err != nil {
		t.Fatalf("DB-owner marked exact candidate rejected: %v", err)
	}
	if _, err := continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers_subject_v2
	 DROP CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk`); err != nil {
		t.Fatal(err)
	}
	if _, err := continuous.DB.Exec(context.Background(), `CREATE TABLE knowledge_v2_weak_parent (
	 issuer text NOT NULL, subject_id text NOT NULL, session_id text NOT NULL,
	 UNIQUE(issuer,subject_id,session_id));
	 ALTER TABLE knowledge_accepted_answers_subject_v2
	 ADD CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk
	 FOREIGN KEY(issuer,subject_id,session_id)
	 REFERENCES knowledge_v2_weak_parent(issuer,subject_id,session_id) NOT VALID;
	 ALTER TABLE knowledge_accepted_answers_subject_v2
	 VALIDATE CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk`); err != nil {
		t.Fatal(err)
	}
	if err := continuous.CheckContinuousSubjectRefV2Writes(context.Background(), nonce); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("same-name validated FK against unrelated parent passed startup gate: %v", err)
	}
}

func TestContinuousSubjectRefV2WriteGateRejectsWeakColumns(t *testing.T) {
	nonce := os.Getenv("KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE")
	if nonce == "" {
		t.Skip("test-only PG marker nonce is supplied by the isolated runner")
	}
	store := testenv.Store(t)
	applyContinuousV2Candidate(t, store)
	continuous := continuousV2Store(store)
	if _, err := continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers_subject_v2
	 ALTER COLUMN subject_id SET DEFAULT '42'`); err != nil {
		t.Fatal(err)
	}
	if err := continuous.CheckContinuousSubjectRefV2Writes(context.Background(), nonce); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("weakened sidecar column shape passed startup gate: %v", err)
	}
	if _, err := continuous.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers_subject_v2
	 ALTER COLUMN subject_id DROP DEFAULT`); err != nil {
		t.Fatal(err)
	}
	if err := continuous.CheckContinuousSubjectRefV2Writes(context.Background(), nonce); err != nil {
		t.Fatalf("restored exact candidate columns rejected: %v", err)
	}
}
