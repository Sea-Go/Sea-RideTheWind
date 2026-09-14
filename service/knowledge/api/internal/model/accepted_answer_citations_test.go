package model_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestProductAnswerCitationsTrackWithdrawalWithoutRepublishingQuote(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	request := makeCitationRequest(t, fixture, "search-product-citation-state")
	receipt, err := store.AcceptSearchCitations(ctx, request)
	receipt = must(t, receipt, err)
	answer := acceptedAnswerRequest(t, request, receipt, "answer-product-citation-state")
	_, err = store.CommitAcceptedAnswer(ctx, answer)
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.GetProductAnswerCitationStates(ctx, answer.Subject, answer.SessionId, answer.AnswerId)
	if err != nil || states.AnswerId != answer.AnswerId || states.SearchId != answer.SearchId ||
		states.ModuleId != fixture.module.Id || states.ReleaseId != fixture.release.ReleaseId ||
		states.Status != "succeeded" || len(states.Citations) != 1 ||
		states.Citations[0].State != "available" || states.Citations[0].EvidenceId == "" {
		t.Fatalf("current cited source not available: %+v %v", states, err)
	}
	other := answer.Subject
	other.SubjectId = "another-user"
	if _, err := store.GetProductAnswerCitationStates(ctx, other, answer.SessionId, answer.AnswerId); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("cross-subject citation state leaked: %v", err)
	}
	empty := insufficientAnswerRequest(t, answer, "answer-product-empty", "search-product-empty")
	_, err = store.CommitAcceptedAnswer(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	emptyStates, err := store.GetProductAnswerCitationStates(ctx, empty.Subject, empty.SessionId, empty.AnswerId)
	if err != nil || emptyStates.Status != "insufficient" || len(emptyStates.Citations) != 0 {
		t.Fatalf("empty product turn fabricated citation state: %+v %v", emptyStates, err)
	}
	_, err = store.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: fixture.module.Id,
		TargetKind: "revision", TargetId: fixture.source.RevisionId, Reason: "withdraw cited source",
		IdempotencyKey: "product-citation-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	states, err = store.GetProductAnswerCitationStates(ctx, answer.Subject, answer.SessionId, answer.AnswerId)
	if err != nil || len(states.Citations) != 1 || states.Citations[0].State != "unavailable" {
		t.Fatalf("withdrawn cited source still available: %+v %v", states, err)
	}
	wire, err := json.Marshal(states)
	if err != nil || strings.Contains(string(wire), `"quote":`) ||
		strings.Contains(string(wire), fixture.chunk.Text) {
		t.Fatalf("product citation state republished historical quote: %s %v", wire, err)
	}
}
