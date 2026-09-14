package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func acceptedAnswerRequest(t *testing.T, citation types.AcceptSearchCitationsReq, receipt types.SearchCitationReceipt, answerID string) types.CommitAcceptedAnswerReq {
	t.Helper()
	var pack map[string]any
	if err := json.Unmarshal([]byte(citation.PackJson), &pack); err != nil {
		t.Fatal(err)
	}
	evidence := pack["evidence"].([]any)[0].(map[string]any)
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw", TenantId: "single", SubjectId: "uid-17"}
	turn := map[string]any{
		"Request": map[string]any{"SearchID": citation.SearchId, "AnswerID": answerID,
			"Subject": subject, "SessionID": "learning-session-1",
			"Search": map[string]any{"Query": "What is the first paragraph?", "Depth": "fast",
				"Intelligence": "low", "Snapshot": pack["snapshot"]}},
		"result": map[string]any{"search": map[string]any{"evidence_pack": json.RawMessage(citation.PackJson),
			"citation_receipt": receipt}, "answer_id": answerID, "answer": "The first paragraph is cited.",
			"citations": []string{evidence["evidence_id"].(string)}, "summary_status": "succeeded"},
	}
	raw, err := json.Marshal(turn)
	if err != nil {
		t.Fatal(err)
	}
	return types.CommitAcceptedAnswerReq{AnswerId: answerID, SearchId: citation.SearchId,
		Subject: subject, SessionId: "learning-session-1", TurnJson: string(raw)}
}

func insufficientAnswerRequest(t *testing.T, from types.CommitAcceptedAnswerReq, answerID, searchID string) types.CommitAcceptedAnswerReq {
	t.Helper()
	var turn map[string]any
	if err := json.Unmarshal([]byte(from.TurnJson), &turn); err != nil {
		t.Fatal(err)
	}
	turn["Request"].(map[string]any)["SearchID"] = searchID
	turn["Request"].(map[string]any)["AnswerID"] = answerID
	result := turn["result"].(map[string]any)
	result["answer_id"] = answerID
	result["answer"] = ""
	result["citations"] = []string{}
	result["summary_status"] = "insufficient"
	search := result["search"].(map[string]any)
	pack := search["evidence_pack"].(map[string]any)
	pack["search_id"] = searchID
	pack["status"] = "empty"
	pack["stop_reason"] = "no_evidence"
	pack["evidence"] = []any{}
	search["citation_receipt"] = map[string]any{"search_id": "", "pack_hash": "", "durable_ref": ""}
	raw, err := json.Marshal(turn)
	if err != nil {
		t.Fatal(err)
	}
	from.AnswerId, from.SearchId, from.TurnJson = answerID, searchID, string(raw)
	return from
}

func TestAcceptedAnswerDurableHistoryAndCitationGate(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "search-answer-history")
	missing := acceptedAnswerRequest(t, citation, types.SearchCitationReceipt{SearchId: citation.SearchId,
		PackHash: citation.PackHash, DurableRef: "search-citations/uncommitted"}, "answer-missing")
	if _, err := store.CommitAcceptedAnswer(ctx, missing); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("uncommitted citation accepted: %v", err)
	}
	receipt, err := store.AcceptSearchCitations(ctx, citation)
	receipt = must(t, receipt, err)
	first := acceptedAnswerRequest(t, citation, receipt, "answer-first")
	committed, err := store.CommitAcceptedAnswer(ctx, first)
	committed = must(t, committed, err)
	if committed.AnswerId != first.AnswerId || committed.Status != "succeeded" || committed.AcceptedOrdinal != 1 || committed.TurnJson != first.TurnJson {
		t.Fatalf("accepted answer differs: %+v", committed)
	}
	replayed, err := store.CommitAcceptedAnswer(ctx, first)
	replayed = must(t, replayed, err)
	if replayed != committed {
		t.Fatalf("retry changed accepted turn: %+v %+v", committed, replayed)
	}
	var cited int
	if err := store.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_answer_citations WHERE answer_id=$1 AND search_id=$2`,
		first.AnswerId, first.SearchId).Scan(&cited); err != nil || cited != 1 {
		t.Fatalf("answer citation was not atomically stored: %d %v", cited, err)
	}
	conflicting := first
	conflicting.SessionId = "other-session"
	if _, err := store.CommitAcceptedAnswer(ctx, conflicting); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("mismatched envelope not rejected: %v", err)
	}
	conflicting = first
	conflicting.TurnJson = strings.Replace(first.TurnJson, "first paragraph is cited", "first paragraph is changed", 1)
	if _, err := store.CommitAcceptedAnswer(ctx, conflicting); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("different answer under same ID not rejected: %v", err)
	}
	wrongReceipt := acceptedAnswerRequest(t, citation, receipt, "answer-forged-receipt")
	wrongReceipt.TurnJson = strings.Replace(wrongReceipt.TurnJson, receipt.DurableRef, "search-citations/forged", 1)
	if _, err := store.CommitAcceptedAnswer(ctx, wrongReceipt); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("forged durable reference accepted: %v", err)
	}
	wrongPack := acceptedAnswerRequest(t, citation, receipt, "answer-forged-pack")
	var changed map[string]any
	if err := json.Unmarshal([]byte(wrongPack.TurnJson), &changed); err != nil {
		t.Fatal(err)
	}
	search := changed["result"].(map[string]any)["search"].(map[string]any)
	pack := search["evidence_pack"].(map[string]any)
	pack["coverage_status"] = "partial"
	changedPack, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	search["citation_receipt"].(map[string]any)["pack_hash"] = object.Hash(changedPack)
	changedRaw, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	wrongPack.TurnJson = string(changedRaw)
	if _, err := store.CommitAcceptedAnswer(ctx, wrongPack); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("forged pack hash with matching forged receipt accepted: %v", err)
	}
	insufficient := insufficientAnswerRequest(t, first, "answer-empty", "search-nohit")
	empty, err := store.CommitAcceptedAnswer(ctx, insufficient)
	empty = must(t, empty, err)
	if empty.Status != "insufficient" || empty.AcceptedOrdinal != 2 {
		t.Fatalf("empty turn was not accepted in order: %+v", empty)
	}
	if _, err = store.CommitAcceptedAnswer(ctx, insufficient); err != nil {
		t.Fatalf("empty turn replay failed: %v", err)
	}
	var emptyCitations int
	if err = store.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_answer_citations WHERE answer_id=$1`, empty.AnswerId).Scan(&emptyCitations); err != nil || emptyCitations != 0 {
		t.Fatalf("insufficient answer fabricated citations: %d %v", emptyCitations, err)
	}
	forgedEmpty := insufficient
	forgedEmpty.AnswerId = "answer-empty-forged"
	forgedEmpty.TurnJson = strings.Replace(insufficient.TurnJson, "answer-empty", forgedEmpty.AnswerId, -1)
	forgedEmpty.TurnJson = strings.Replace(forgedEmpty.TurnJson, `"answer":""`, `"answer":"invented"`, 1)
	if _, err = store.CommitAcceptedAnswer(ctx, forgedEmpty); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("fabricated empty answer accepted: %v", err)
	}
	page, err := store.ListAcceptedAnswers(ctx, types.ListAcceptedAnswersReq{AuthorityId: first.Subject.AuthorityId,
		TenantId: first.Subject.TenantId, SubjectId: first.Subject.SubjectId, SessionId: first.SessionId, Limit: 1})
	page = must(t, page, err)
	if len(page.Items) != 1 || page.Items[0].AnswerId != first.AnswerId || page.NextOrdinal != 1 {
		t.Fatalf("first history page wrong: %+v", page)
	}
	next, err := store.ListAcceptedAnswers(ctx, types.ListAcceptedAnswersReq{AuthorityId: first.Subject.AuthorityId,
		TenantId: first.Subject.TenantId, SubjectId: first.Subject.SubjectId, SessionId: first.SessionId,
		AfterOrdinal: page.NextOrdinal, Limit: 1})
	next = must(t, next, err)
	if len(next.Items) != 1 || next.Items[0].AnswerId != empty.AnswerId || next.NextOrdinal != 0 {
		t.Fatalf("second history page wrong: %+v", next)
	}
	wrongSubject := first.Subject
	wrongSubject.SubjectId = "someone-else"
	if _, err := store.GetAcceptedAnswer(ctx, wrongSubject, first.SessionId, first.AnswerId); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("wrong subject read answer: %v", err)
	}
	if _, err := store.ListAcceptedAnswers(ctx, types.ListAcceptedAnswersReq{AuthorityId: first.Subject.AuthorityId,
		TenantId: first.Subject.TenantId, SubjectId: first.Subject.SubjectId, SessionId: first.SessionId, Limit: 101}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("unbounded history read accepted: %v", err)
	}
}

func TestAcceptedAnswerConcurrentReplayOneOrdinal(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "search-concurrent-answer")
	receipt, err := store.AcceptSearchCitations(ctx, citation)
	receipt = must(t, receipt, err)
	req := acceptedAnswerRequest(t, citation, receipt, "answer-concurrent")
	var wg sync.WaitGroup
	var results [4]types.AcceptedAnswer
	var errs [4]error
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = store.CommitAcceptedAnswer(context.Background(), req)
		}(i)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || results[i] != results[0] {
			t.Fatalf("concurrent replay[%d] differs: %+v %v", i, results[i], errs[i])
		}
	}
	var count, ordinal int
	if err := store.DB.QueryRow(ctx, `SELECT count(*),max(accepted_ordinal) FROM knowledge_accepted_answers WHERE answer_id=$1`, req.AnswerId).
		Scan(&count, &ordinal); err != nil || count != 1 || ordinal != 1 {
		t.Fatalf("concurrent acceptance duplicated: %d %d %v", count, ordinal, err)
	}
}

func TestAcceptedAnswerConcurrentOrderAndCrossScopeCollision(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "search-order-base")
	base := acceptedAnswerRequest(t, citation, types.SearchCitationReceipt{SearchId: citation.SearchId,
		PackHash: citation.PackHash, DurableRef: "uncommitted-fixture"}, "answer-order-base")
	var requests [4]types.CommitAcceptedAnswerReq
	for i := range requests {
		requests[i] = insufficientAnswerRequest(t, base,
			"answer-order-"+string(rune('a'+i)), "search-order-"+string(rune('a'+i)))
	}
	var wg sync.WaitGroup
	var results [4]types.AcceptedAnswer
	var errs [4]error
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = store.CommitAcceptedAnswer(context.Background(), requests[i])
		}(i)
	}
	wg.Wait()
	orders := map[int64]bool{}
	for i := range results {
		if errs[i] != nil || results[i].Status != "insufficient" || orders[results[i].AcceptedOrdinal] {
			t.Fatalf("concurrent distinct turns lost acceptance order[%d]: %+v %v", i, results[i], errs[i])
		}
		orders[results[i].AcceptedOrdinal] = true
	}
	if len(orders) != 4 || !orders[1] || !orders[2] || !orders[3] || !orders[4] {
		t.Fatalf("accepted session order has gaps: %v", orders)
	}
	first := insufficientAnswerRequest(t, base, "answer-global-collision", "search-global-collision")
	second := first
	second.Subject.SubjectId = "other-uid"
	second.TurnJson = strings.Replace(first.TurnJson, first.Subject.SubjectId, second.Subject.SubjectId, 1)
	var collisionErrs [2]error
	wg.Add(2)
	go func() { defer wg.Done(); _, collisionErrs[0] = store.CommitAcceptedAnswer(context.Background(), first) }()
	go func() {
		defer wg.Done()
		_, collisionErrs[1] = store.CommitAcceptedAnswer(context.Background(), second)
	}()
	wg.Wait()
	accepted, conflicts := 0, 0
	for _, err := range collisionErrs {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, model.ErrConflict):
			conflicts++
		default:
			t.Fatalf("cross-scope collision returned unclassified error: %v", err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("global AnswerID collision accepted twice: %v", collisionErrs)
	}
}
