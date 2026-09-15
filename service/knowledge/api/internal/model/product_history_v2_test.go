package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/api/internal/logic/product_v2"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/rpc/pb"

	"google.golang.org/grpc"
)

type v2HistoryTestUserReader struct{}

func (v2HistoryTestUserReader) GetUser(_ context.Context, req *pb.GetUserReq,
	_ ...grpc.CallOption) (*pb.GetUserResp, error) {
	active := int64(0)
	return &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: req.Uid, Status: &active}}, nil
}

func canonicalHistoryRequest(t *testing.T, receipt types.SearchCitationReceipt,
	citation types.AcceptSearchCitationsReq, answerID, tenant, uid string) types.CommitAcceptedAnswerReq {
	t.Helper()
	req := acceptedAnswerRequest(t, citation, receipt, answerID)
	oldSubject, err := json.Marshal(req.Subject)
	if err != nil {
		t.Fatal(err)
	}
	req.Subject = types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: tenant, SubjectId: uid}
	newSubject, err := json.Marshal(req.Subject)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(req.TurnJson, string(oldSubject), string(newSubject), 1)
	if changed == req.TurnJson {
		t.Fatal("frozen old Subject wire absent from accepted turn")
	}
	req.TurnJson = changed
	return req
}

func TestProductHistoryV2OldSlotCollisionAndCanonicalUID(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "v2-history-search")
	receipt, err := store.AcceptSearchCitations(context.Background(), citation)
	if err != nil {
		t.Fatal(err)
	}
	const highUID = "9223372036854775807"
	old := canonicalHistoryRequest(t, receipt, citation, "v2-history-owner", "platform", highUID)
	committed, err := store.CommitAcceptedAnswer(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
	if err != nil || read != committed || object.Hash([]byte(read.TurnJson)) != object.Hash([]byte(old.TurnJson)) {
		t.Fatalf("v2 read changed high-UID legacy turn: %+v %v", read, err)
	}
	page, err := store.ListVerifiedProductAcceptedAnswers(context.Background(), types.ListAcceptedAnswersReq{
		AuthorityId: old.Subject.AuthorityId, TenantId: old.Subject.TenantId,
		SubjectId: old.Subject.SubjectId, SessionId: old.SessionId, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0] != committed {
		t.Fatalf("v2 list differs from old immutable row: %+v %v", page, err)
	}
	otherUID := old.Subject
	otherUID.SubjectId = strconv.FormatInt(9223372036854775806, 10)
	if _, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), otherUID, old.SessionId, old.AnswerId); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("other canonical UID read same AnswerID: %v", err)
	}
	for _, bad := range []string{"0", "01", "-1", "9223372036854775808", "unknown"} {
		subject := old.Subject
		subject.SubjectId = bad
		if _, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), subject, old.SessionId, old.AnswerId); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("noncanonical v2 UID %q accepted: %v", bad, err)
		}
	}
	// A second frozen v1 slot has its own ordinal 1. Dropping the slot from
	// the v2 wire would collapse these distinct old keys, even on a paged read.
	oldSlot := canonicalHistoryRequest(t, receipt, citation, "v2-history-old-slot", "archive", highUID)
	if _, err := store.CommitAcceptedAnswer(context.Background(), oldSlot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("v2 detail hid target ordinal collision: %v", err)
	}
	if _, err := store.ListVerifiedProductAcceptedAnswers(context.Background(), types.ListAcceptedAnswersReq{
		AuthorityId: old.Subject.AuthorityId, TenantId: old.Subject.TenantId,
		SubjectId: old.Subject.SubjectId, SessionId: old.SessionId, AfterOrdinal: 1, Limit: 1}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("empty later page hid earlier v2 projection collision: %v", err)
	}
}

func TestProductHistoryV2RejectsTwoNonPlatformSlotsOnEmptyCanonicalPage(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "v2-only-old-slots-search")
	receipt, err := store.AcceptSearchCitations(context.Background(), citation)
	if err != nil {
		t.Fatal(err)
	}
	archive := canonicalHistoryRequest(t, receipt, citation, "v2-only-archive", "archive", "17")
	legacy := canonicalHistoryRequest(t, receipt, citation, "v2-only-legacy", "legacy", "17")
	for _, req := range []types.CommitAcceptedAnswerReq{archive, legacy} {
		answer, err := store.CommitAcceptedAnswer(context.Background(), req)
		if err != nil || answer.AcceptedOrdinal != 1 {
			t.Fatalf("old slot did not own its valid ordinal 1: %+v %v", answer, err)
		}
	}
	canonical := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "17"}
	query := types.ListAcceptedAnswersReq{AuthorityId: canonical.AuthorityId, TenantId: canonical.TenantId,
		SubjectId: canonical.SubjectId, SessionId: archive.SessionId, Limit: 1}
	oldPage, err := store.ListAcceptedAnswers(context.Background(), query)
	if err != nil || len(oldPage.Items) != 0 {
		t.Fatalf("v1 canonical empty page changed: %+v %v", oldPage, err)
	}
	if _, err := store.GetAcceptedAnswer(context.Background(), canonical, archive.SessionId, archive.AnswerId); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("v1 canonical detail did not remain 404: %v", err)
	}
	if _, err := store.ListVerifiedProductAcceptedAnswers(context.Background(), query); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("v2 empty canonical page hid two other-slot ordinal collisions: %v", err)
	}
	if _, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), canonical, archive.SessionId, archive.AnswerId); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("v2 detail hid two other-slot ordinal collisions: %v", err)
	}
}

func TestProductHistoryV2RejectsTamperedOldRow(t *testing.T) {
	for _, fault := range []string{"turn_hash", "duplicate_subject_key", "duplicate_subject_alias", "extra_subject_realm", "quote_hash", "citation_search_id"} {
		t.Run(fault, func(t *testing.T) {
			store := testenv.Store(t)
			fixture := makeCitationFixture(t, store)
			citation := makeCitationRequest(t, fixture, "v2-tamper-search")
			receipt, err := store.AcceptSearchCitations(context.Background(), citation)
			if err != nil {
				t.Fatal(err)
			}
			old := canonicalHistoryRequest(t, receipt, citation, "v2-tamper-answer", "platform", "17")
			if _, err := store.CommitAcceptedAnswer(context.Background(), old); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "turn_hash":
				_, err = store.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers DISABLE TRIGGER knowledge_accepted_answer_immutable`)
				if err == nil {
					_, err = store.DB.Exec(context.Background(), `UPDATE knowledge_accepted_answers SET turn_hash=$2 WHERE answer_id=$1`, old.AnswerId, strings.Repeat("0", 64))
				}
			case "duplicate_subject_key", "duplicate_subject_alias", "extra_subject_realm":
				bad := strings.Replace(old.TurnJson, `"tenant_id":"platform"`, `"tenant_id":"platform","tenant_id":"platform"`, 1)
				if fault == "duplicate_subject_alias" {
					var root map[string]json.RawMessage
					var request map[string]json.RawMessage
					if json.Unmarshal([]byte(old.TurnJson), &root) != nil || json.Unmarshal(root["Request"], &request) != nil {
						t.Fatal("old turn Request fixture missing")
					}
					bad = strings.Replace(old.TurnJson, `"Subject":`, `"subject":`+string(request["Subject"])+`,"Subject":`, 1)
				} else if fault == "extra_subject_realm" {
					bad = strings.Replace(old.TurnJson, `"tenant_id":"platform"`, `"tenant_id":"platform","realm":"platform"`, 1)
				}
				if bad == old.TurnJson {
					t.Fatal("old Subject JSON fixture did not have frozen v1 key")
				}
				_, err = store.DB.Exec(context.Background(), `ALTER TABLE knowledge_accepted_answers DISABLE TRIGGER knowledge_accepted_answer_immutable`)
				if err == nil {
					_, err = store.DB.Exec(context.Background(), `UPDATE knowledge_accepted_answers SET turn_json=$2,turn_hash=$3 WHERE answer_id=$1`,
						old.AnswerId, bad, object.Hash([]byte(bad)))
				}
			case "quote_hash":
				_, err = store.DB.Exec(context.Background(), `ALTER TABLE knowledge_answer_citations DISABLE TRIGGER knowledge_answer_citation_immutable`)
				if err == nil {
					_, err = store.DB.Exec(context.Background(), `UPDATE knowledge_answer_citations SET quote_hash=$2 WHERE answer_id=$1`, old.AnswerId, strings.Repeat("0", 64))
				}
			case "citation_search_id":
				// The second search is accepted by the real citation authority,
				// so this is a valid FK, not a manufactured missing reference.
				other := makeCitationRequest(t, fixture, "v2-tamper-other-search")
				_, err = store.AcceptSearchCitations(context.Background(), other)
				if err == nil {
					_, err = store.DB.Exec(context.Background(), `ALTER TABLE knowledge_answer_citations DISABLE TRIGGER knowledge_answer_citation_immutable`)
				}
				if err == nil {
					_, err = store.DB.Exec(context.Background(), `UPDATE knowledge_answer_citations SET search_id=$2 WHERE answer_id=$1`, old.AnswerId, other.SearchId)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if fault == "citation_search_id" {
				var storedTurn, storedHash string
				if err := store.DB.QueryRow(context.Background(), `SELECT turn_json,turn_hash FROM knowledge_accepted_answers WHERE answer_id=$1`,
					old.AnswerId).Scan(&storedTurn, &storedHash); err != nil || storedTurn != old.TurnJson || storedHash != object.Hash([]byte(old.TurnJson)) {
					t.Fatalf("citation FK fixture changed old answer/turn/hash: %v", err)
				}
				v1, err := store.GetAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
				if err != nil || v1.TurnJson != old.TurnJson {
					t.Fatalf("v1 historical detail changed under citation FK fixture: %+v %v", v1, err)
				}
			}
			if _, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId); !errors.Is(err, model.ErrArtifactUnavailable) {
				t.Fatalf("v2 detail accepted tampered %s: %v", fault, err)
			}
			if _, err := store.ListVerifiedProductAcceptedAnswers(context.Background(), types.ListAcceptedAnswersReq{
				AuthorityId: old.Subject.AuthorityId, TenantId: old.Subject.TenantId,
				SubjectId: old.Subject.SubjectId, SessionId: old.SessionId, Limit: 1}); !errors.Is(err, model.ErrArtifactUnavailable) {
				t.Fatalf("v2 page accepted tampered %s: %v", fault, err)
			}
			if fault == "citation_search_id" {
				claim := context.WithValue(context.Background(), "userId", json.Number("17"))
				logic := product_v2.NewGetProductAnswerCitationStatesV2Logic(claim,
					&svc.ServiceContext{Store: store, UserRpc: v2HistoryTestUserReader{}})
				if _, err := logic.GetProductAnswerCitationStatesV2(&types.ProductAcceptedAnswerReq{
					SessionId: old.SessionId, AnswerId: old.AnswerId}); !errors.Is(err, model.ErrArtifactUnavailable) {
					t.Fatalf("v2 citations accepted tampered persisted SearchID: %v", err)
				}
			}
		})
	}
}
