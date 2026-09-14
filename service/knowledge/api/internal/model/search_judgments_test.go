package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func committedJudgmentSearch(t *testing.T, s *model.Store, f citationFixture) model.ProductSearchOperation {
	t.Helper()
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw", TenantId: "single", SubjectId: "uid-17"}
	op, err := s.ReserveProductSearch(ctx, subject, "learning-session-1", "human-qrel-search-key",
		model.ProductSearchInput{ModuleID: f.module.Id, Query: "What is the first paragraph?",
			Depth: "fast", Intelligence: "low"})
	op = must(t, op, err)
	citation := makeCitationRequest(t, f, op.SearchID)
	receipt, err := s.AcceptSearchCitations(ctx, citation)
	receipt = must(t, receipt, err)
	answer := acceptedAnswerRequest(t, citation, receipt, op.AnswerID)
	_, err = s.CommitAcceptedAnswer(ctx, answer)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteProductSearch(ctx, op); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestHumanSearchJudgmentRevisionWithdrawalAndEventReceipt(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	op := committedJudgmentSearch(t, s, f)
	request := types.RecordSearchJudgmentReq{ModuleId: f.module.Id, SearchId: op.SearchID,
		ContentRevisionId: f.chunk.RevisionId, ChunkId: f.chunk.ChunkId, Grade: "3",
		RubricVersion: "sea.search.relevance.v1", Reason: "direct answer in this immutable chunk",
		IdempotencyKey: "human-judgment-one"}
	first, err := s.RecordSearchJudgment(ctx, "admin-1", request)
	first = must(t, first, err)
	if first.State != "judged" || first.EventId == "" || first.EventSha256 == "" {
		t.Fatalf("missing first receipt: %+v", first)
	}
	wire, err := s.GetSearchJudgmentEvent(ctx, first.EventId)
	wire = must(t, wire, err)
	if wire.EventSha256 != first.EventSha256 || object.Hash([]byte(wire.EventJson)) != wire.EventSha256 {
		t.Fatalf("source event receipt differs: %+v", wire)
	}
	var event model.Event
	if err = json.Unmarshal([]byte(wire.EventJson), &event); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if event.Producer != "ridethewind.knowledge" || event.EventType != "knowledge.search.judgment.revised.v1" ||
		payload["judgment_source"] != "human_judgment" || payload["grade"] != float64(3) ||
		payload["query_text_sha256"] != object.Hash([]byte(op.Search.Query)) ||
		payload["judgment_revision"] != float64(1) || payload["chunk_text"] != f.chunk.Text ||
		payload["chunk_text_sha256"] != f.chunk.TextHash || payload["content_revision_id"] != f.chunk.RevisionId ||
		payload["actor_id"] != "admin-1" || payload["query_time"] == "" ||
		payload["content_available_at"] == "" || payload["judged_at"] == "" {
		t.Fatalf("event lost frozen qrel authority: %+v", payload)
	}
	contentAt, err := time.Parse(time.RFC3339Nano, payload["content_available_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	queryAt, err := time.Parse(time.RFC3339Nano, payload["query_time"].(string))
	if err != nil {
		t.Fatal(err)
	}
	judgedAt, err := time.Parse(time.RFC3339Nano, payload["judged_at"].(string))
	if err != nil || contentAt.After(queryAt) || queryAt.After(judgedAt) {
		t.Fatalf("qrel availability order invalid: content=%v query=%v judged=%v err=%v", contentAt, queryAt, judgedAt, err)
	}
	var parallel [4]types.SearchJudgmentReceipt
	var errs [4]error
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func(i int) { defer wg.Done(); parallel[i], errs[i] = s.RecordSearchJudgment(ctx, "admin-1", request) }(i)
	}
	wg.Wait()
	for i := range parallel {
		if errs[i] != nil || parallel[i] != first {
			t.Fatalf("idempotent retry changed revision: %+v %v", parallel[i], errs[i])
		}
	}
	changed := request
	changed.Grade = "2"
	if _, err := s.RecordSearchJudgment(ctx, "admin-1", changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key different grade accepted: %v", err)
	}
	changed.IdempotencyKey = "human-judgment-two"
	if _, err := s.RecordSearchJudgment(ctx, "admin-2", changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale base accepted: %v", err)
	}
	changed.BaseRevisionId = first.RevisionId
	changed.Grade = "0"
	changed.Reason = "explicitly judged unrelated, not an unobserved click"
	second, err := s.RecordSearchJudgment(ctx, "admin-2", changed)
	second = must(t, second, err)
	if second.JudgmentId != first.JudgmentId || second.RevisionId == first.RevisionId {
		t.Fatalf("revision history drift: %+v %+v", first, second)
	}
	secondWire, err := s.GetSearchJudgmentEvent(ctx, second.EventId)
	secondWire = must(t, secondWire, err)
	if err = json.Unmarshal([]byte(secondWire.EventJson), &event); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(event.Payload, &payload); err != nil || payload["judgment_revision"] != float64(2) || payload["grade"] != float64(0) {
		t.Fatalf("second judgment revision missing: %+v %v", payload, err)
	}
	withdraw := types.WithdrawSearchJudgmentReq{ModuleId: f.module.Id, SearchId: op.SearchID,
		ChunkId: f.chunk.ChunkId, BaseRevisionId: second.RevisionId,
		Reason: "judge retracted this label", IdempotencyKey: "human-judgment-withdraw"}
	third, err := s.WithdrawSearchJudgment(ctx, "admin-2", withdraw)
	third = must(t, third, err)
	wire, err = s.GetSearchJudgmentEvent(ctx, third.EventId)
	wire = must(t, wire, err)
	if err = json.Unmarshal([]byte(wire.EventJson), &event); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if event.EventType != "knowledge.search.judgment.withdrawn.v1" || payload["grade"] != nil ||
		payload["state"] != "withdrawn" || payload["judgment_revision"] != float64(3) ||
		payload["base_revision_id"] != second.RevisionId {
		t.Fatalf("withdrawal did not append tombstone: %+v", payload)
	}
	if _, err := s.WithdrawSearchJudgment(ctx, "admin-2", withdraw); err != nil {
		t.Fatalf("withdraw replay failed: %v", err)
	}
	var revisions, events int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_search_judgment_revisions WHERE judgment_id=$1", first.JudgmentId).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_search_judgment_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if revisions != 3 || events != 3 {
		t.Fatalf("replay duplicated durable history: revisions=%d events=%d", revisions, events)
	}
	if _, err := s.DB.Exec(ctx, "UPDATE knowledge_search_judgment_revisions SET data='{}'::jsonb WHERE revision_id=$1", first.RevisionId); err == nil {
		t.Fatal("immutable revision updated")
	}
	if _, err := s.DB.Exec(ctx, "DELETE FROM knowledge_search_judgment_events WHERE event_id=$1", first.EventId); err == nil {
		t.Fatal("immutable event deleted")
	}
	// Historical source bytes and an idempotent response remain available even
	// after the human label is withdrawn.
	if old, err := s.GetSearchJudgmentEvent(ctx, first.EventId); err != nil || old.EventSha256 != first.EventSha256 {
		t.Fatalf("historical event missing after withdrawal: %+v %v", old, err)
	}
	if _, err := s.Withdraw(ctx, "admin-1", types.WithdrawReq{ModuleId: f.module.Id,
		TargetKind: "revision", TargetId: f.source.RevisionId, Reason: "retire source after judgment",
		IdempotencyKey: "retire-source-after-qrel"}); err != nil {
		t.Fatal(err)
	}
	if replay, err := s.RecordSearchJudgment(ctx, "admin-1", request); err != nil || replay != first {
		t.Fatalf("source retirement changed idempotent historical judgment: %+v %v", replay, err)
	}
	if _, err := s.GetSearchJudgmentEvent(ctx, first.EventId); err != nil {
		t.Fatalf("source retirement erased historical event: %v", err)
	}
}

func TestHumanSearchJudgmentRejectsUnacceptedAndUnpublishedPairs(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw", TenantId: "single", SubjectId: "uid-17"}
	op, err := s.ReserveProductSearch(ctx, subject, "learning-session-1", "unaccepted-search-key",
		model.ProductSearchInput{ModuleID: f.module.Id, Query: "What?", Depth: "fast", Intelligence: "low"})
	op = must(t, op, err)
	request := types.RecordSearchJudgmentReq{ModuleId: f.module.Id, SearchId: op.SearchID,
		ContentRevisionId: f.chunk.RevisionId, ChunkId: f.chunk.ChunkId, Grade: "0",
		RubricVersion: "sea.search.relevance.v1", Reason: "not related", IdempotencyKey: "invalid-source-key"}
	if _, err = s.RecordSearchJudgment(ctx, "admin-1", request); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("unaccepted search became human qrel: %v", err)
	}
	op = committedJudgmentSearch(t, s, f)
	request.SearchId = op.SearchID
	for name, mutate := range map[string]func(*types.RecordSearchJudgmentReq){
		"missing grade":  func(r *types.RecordSearchJudgmentReq) { r.Grade = "" },
		"bad grade":      func(r *types.RecordSearchJudgmentReq) { r.Grade = "4" },
		"unknown rubric": func(r *types.RecordSearchJudgmentReq) { r.RubricVersion = "unknown" },
		"other revision": func(r *types.RecordSearchJudgmentReq) { r.ContentRevisionId = "revision_other" },
		"other chunk":    func(r *types.RecordSearchJudgmentReq) { r.ChunkId = strings.Repeat("a", 64) },
		"missing actor":  func(r *types.RecordSearchJudgmentReq) {},
	} {
		t.Run(name, func(t *testing.T) {
			bad := request
			mutate(&bad)
			actor := "admin-1"
			if name == "missing actor" {
				actor = ""
			}
			bad.IdempotencyKey = "bad-" + strings.ReplaceAll(name, " ", "-") + "-key"
			if _, err := s.RecordSearchJudgment(ctx, actor, bad); err == nil {
				t.Fatal("invalid judgment accepted")
			}
		})
	}
	var count int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_search_judgment_events").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid input emitted event: %d %v", count, err)
	}
}

func TestHumanSearchJudgmentOutboxFailureRollsBackAll(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	op := committedJudgmentSearch(t, s, f)
	_, err := s.DB.Exec(ctx, `CREATE FUNCTION reject_qrel_event() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.event_type='knowledge.search.judgment.revised.v1' THEN RAISE EXCEPTION 'injected qrel outbox failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_qrel_event BEFORE INSERT ON knowledge_outbox FOR EACH ROW EXECUTE FUNCTION reject_qrel_event()`)
	if err != nil {
		t.Fatal(err)
	}
	request := types.RecordSearchJudgmentReq{ModuleId: f.module.Id, SearchId: op.SearchID,
		ContentRevisionId: f.chunk.RevisionId, ChunkId: f.chunk.ChunkId, Grade: "1",
		RubricVersion: "sea.search.relevance.v1", Reason: "partially related", IdempotencyKey: "qrel-outbox-failure"}
	if _, err = s.RecordSearchJudgment(ctx, "admin-1", request); err == nil {
		t.Fatal("outbox failure acknowledged as judgment")
	}
	var rows int
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_search_judgment_revisions").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("revision committed without outbox: %d %v", rows, err)
	}
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_search_judgment_heads").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("head committed without outbox: %d %v", rows, err)
	}
	if _, err = s.DB.Exec(ctx, "DROP TRIGGER reject_qrel_event ON knowledge_outbox"); err != nil {
		t.Fatal(err)
	}
	first, err := s.RecordSearchJudgment(context.Background(), "admin-1", request)
	if err != nil || first.EventId == "" {
		t.Fatalf("same key could not recover: %+v %v", first, err)
	}
}

func TestSearchJudgmentMigrationProbeAndReentry(t *testing.T) {
	s := testenv.Store(t)
	if err := s.CheckSearchJudgmentSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `DROP TABLE knowledge_search_judgment_events,
 knowledge_search_judgment_heads,knowledge_search_judgment_revisions`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSearchJudgmentSchema(ctx); err == nil {
		t.Fatal("old database passed enabled judgment probe")
	}
	migration, err := os.ReadFile("../../../scripts/migrate-search-judgments.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.DB.Exec(ctx, string(migration), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("operator migration run %d failed: %v", i+1, err)
		}
		if err = s.CheckSearchJudgmentSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
