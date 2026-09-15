package model_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
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

type reviewCaseEvidence struct {
	ID          string `json:"evidence_id"`
	Quote       string `json:"quote"`
	QuoteSHA256 string `json:"quote_sha256"`
}
type reviewCase struct {
	SchemaVersion           string               `json:"schema_version"`
	CaseID                  string               `json:"case_id"`
	PolicyRevision          string               `json:"policy_revision"`
	ReviewStatus            string               `json:"review_status"`
	Activation              string               `json:"activation"`
	SearchID                string               `json:"search_id"`
	AnswerID                string               `json:"answer_id"`
	ModelInteractionID      string               `json:"model_interaction_id"`
	ModelResponseSHA256     string               `json:"model_response_sha256"`
	RTWAnswerReportSHA256   string               `json:"rtw_answer_report_sha256"`
	DCUsageReportSHA256     string               `json:"dc_usage_report_sha256"`
	RTWTraceLogSHA256       string               `json:"rtw_trace_log_sha256"`
	RTWTraceID              string               `json:"rtw_trace_id"`
	TraceScope              string               `json:"trace_scope"`
	CitationPackRef         string               `json:"citation_pack_ref"`
	Evidence                []reviewCaseEvidence `json:"evidence"`
	AcceptedAnswer          string               `json:"accepted_answer"`
	AcceptedAnswerSHA256    string               `json:"accepted_answer_sha256"`
	PriorQualityObservation string               `json:"prior_quality_observation"`
}
type reviewClaim struct {
	StartByte   int      `json:"start_byte"`
	EndByte     int      `json:"end_byte"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
}
type reviewPayload struct {
	SchemaVersion     string        `json:"schema_version"`
	CaseSHA256        string        `json:"case_sha256"`
	PolicyRevision    string        `json:"policy_revision"`
	DataKind          string        `json:"data_kind"`
	ReviewerAuthority string        `json:"reviewer_authority"`
	ReviewerID        string        `json:"reviewer_id"`
	ReviewedAt        string        `json:"reviewed_at"`
	CoverageComplete  bool          `json:"coverage_complete"`
	Claims            []reviewClaim `json:"claims"`
}
type signedReview struct {
	Payload   reviewPayload `json:"payload"`
	Signature string        `json:"signature"`
}

func caseBytes(t *testing.T, c reviewCase) string {
	t.Helper()
	c.CaseID = ""
	identity, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	c.CaseID = "grounding-" + object.Hash(identity)
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(append(raw, '\n'))
}

func reviewBytes(t *testing.T, caseRaw string, key types.ReviewerKeyRecord, private ed25519.PrivateKey) string {
	t.Helper()
	var c reviewCase
	if err := json.Unmarshal([]byte(caseRaw), &c); err != nil {
		t.Fatal(err)
	}
	p := reviewPayload{SchemaVersion: "sea.search.answer-grounding-review.v1",
		CaseSHA256: object.Hash([]byte(caseRaw)), PolicyRevision: "sea.search.answer-grounding-human-review.v1",
		DataKind: key.DataKind, ReviewerAuthority: key.ReviewerAuthority, ReviewerID: key.ReviewerId,
		ReviewedAt: time.Now().UTC().Format(time.RFC3339Nano), CoverageComplete: true,
		Claims: []reviewClaim{{StartByte: 0, EndByte: len(c.AcceptedAnswer), Text: c.AcceptedAnswer,
			EvidenceIDs: []string{c.Evidence[0].ID}, Label: "unsupported", Reason: "test-generated reviewer judgment"}}}
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(signedReview{Payload: p, Signature: hex.EncodeToString(ed25519.Sign(private, payload))})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func acceptedGroundingCase(t *testing.T, s *model.Store) reviewCase {
	t.Helper()
	f := makeCitationFixture(t, s)
	const searchID = "search-grounding-case"
	citation := makeCitationRequest(t, f, searchID)
	receipt, err := s.AcceptSearchCitations(ctx, citation)
	receipt = must(t, receipt, err)
	answerReq := acceptedAnswerRequest(t, citation, receipt, "answer-grounding-case")
	_, err = s.CommitAcceptedAnswer(ctx, answerReq)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := s.GetSearchCitations(ctx, searchID)
	seen = must(t, seen, err)
	return reviewCase{SchemaVersion: "sea.search.answer-grounding-case.v1",
		PolicyRevision: "sea.search.answer-grounding-human-review.v1",
		ReviewStatus:   "pending_human_review", Activation: "none", SearchID: searchID,
		AnswerID: answerReq.AnswerId, ModelInteractionID: "fixture-interaction",
		ModelResponseSHA256:   object.Hash([]byte("synthetic-model")),
		RTWAnswerReportSHA256: object.Hash([]byte("synthetic-rtw-report")),
		DCUsageReportSHA256:   object.Hash([]byte("synthetic-dc-report")),
		RTWTraceLogSHA256:     object.Hash([]byte("synthetic-rtw-trace")),
		RTWTraceID:            strings.Repeat("a", 32), TraceScope: "rtw_structured_log_not_otlp_collector",
		CitationPackRef: receipt.DurableRef, Evidence: []reviewCaseEvidence{{ID: seen.Evidence[0].EvidenceId,
			Quote: f.chunk.Text, QuoteSHA256: f.chunk.TextHash}},
		AcceptedAnswer:          "The first paragraph is cited.",
		AcceptedAnswerSHA256:    object.Hash([]byte("The first paragraph is cited.")),
		PriorQualityObservation: "fixture-only"}
}

func TestReviewerRegistrySignedGroundingAndRevocation(t *testing.T) {
	s := testenv.Store(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	register := types.RegisterReviewerKeyReq{KeyId: "synthetic-reviewer-key", DataKind: "synthetic_fixture",
		PublicKeyEd25519Hex: hex.EncodeToString(public), IdempotencyKey: "reviewer-register-key"}
	key, err := s.RegisterReviewerKey(ctx, "admin-1", register)
	key = must(t, key, err)
	if key.Status != "active" || key.RegistryRevision != 1 || key.ReviewerId != "admin-1" || key.RegistrationEventId == "" {
		t.Fatalf("key authority missing: %+v", key)
	}
	readKey, err := s.GetReviewerKey(ctx, key.KeyId)
	readKey = must(t, readKey, err)
	if readKey != key {
		t.Fatalf("key read differs: %+v %+v", key, readKey)
	}
	if replay, err := s.RegisterReviewerKey(ctx, "admin-1", register); err != nil || replay != key {
		t.Fatalf("key replay drift: %+v %v", replay, err)
	}
	c := acceptedGroundingCase(t, s)
	caseRaw := caseBytes(t, c)
	reviewRaw := reviewBytes(t, caseRaw, key, private)
	req := types.SubmitGroundingReviewReq{KeyId: key.KeyId, CaseJson: caseRaw, ReviewJson: reviewRaw,
		IdempotencyKey: "grounding-review-submit-key"}
	_, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_review_event() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.event_type='knowledge.answer.grounding.review.accepted.v1' THEN RAISE EXCEPTION 'injected review outbox failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_review_event BEFORE INSERT ON knowledge_outbox FOR EACH ROW EXECUTE FUNCTION reject_review_event()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitGroundingReview(ctx, "admin-1", req); err == nil {
		t.Fatal("review outbox failure acknowledged")
	}
	var uncommitted int
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_answer_grounding_reviews").Scan(&uncommitted); err != nil || uncommitted != 0 {
		t.Fatalf("review committed without outbox: count=%d err=%v", uncommitted, err)
	}
	if _, err = s.DB.Exec(ctx, "DROP TRIGGER reject_review_event ON knowledge_outbox"); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.SubmitGroundingReview(ctx, "admin-1", req)
	receipt = must(t, receipt, err)
	if receipt.CaseSha256 != object.Hash([]byte(caseRaw)) || receipt.ReviewSha256 != object.Hash([]byte(reviewRaw)) ||
		receipt.TraceAuthorityStatus != "external_case_unverified" || receipt.RegistryRevisionAtCommit != 1 ||
		receipt.CitationPackRef != c.CitationPackRef || receipt.AnswerId != c.AnswerID || receipt.EventSha256 == "" {
		t.Fatalf("immutable receipt lost source binding: %+v", receipt)
	}
	got, err := s.GetGroundingReview(ctx, receipt.CaseSha256)
	got = must(t, got, err)
	if got != receipt {
		t.Fatalf("review read differs: %+v %+v", got, receipt)
	}
	if replay, err := s.SubmitGroundingReview(ctx, "admin-1", req); err != nil || replay != receipt {
		t.Fatalf("review replay drift: %+v %v", replay, err)
	}
	changed := req
	changed.IdempotencyKey = "different-review-command-key"
	if _, err := s.SubmitGroundingReview(ctx, "admin-1", changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("duplicate case accepted: %v", err)
	}
	revoke := types.RevokeReviewerKeyReq{KeyId: key.KeyId, Reason: "synthetic reviewer retired", IdempotencyKey: "reviewer-revoke-key"}
	revoked, err := s.RevokeReviewerKey(ctx, "admin-1", revoke)
	revoked = must(t, revoked, err)
	if revoked.Status != "revoked" || revoked.RegistryRevision != 2 || revoked.RevokedAt == "" || revoked.RevocationEventId == "" {
		t.Fatalf("revocation missing: %+v", revoked)
	}
	if current, err := s.GetReviewerKey(ctx, key.KeyId); err != nil || current != revoked {
		t.Fatalf("current key not revoked: %+v %v", current, err)
	}
	if replay, err := s.RevokeReviewerKey(ctx, "admin-1", revoke); err != nil || replay != revoked {
		t.Fatalf("revocation replay drift: %+v %v", replay, err)
	}
	if old, err := s.GetGroundingReview(ctx, receipt.CaseSha256); err != nil || old != receipt {
		t.Fatalf("revocation erased history: %+v %v", old, err)
	}
	if replay, err := s.SubmitGroundingReview(ctx, "admin-1", req); err != nil || replay != receipt {
		t.Fatalf("revocation changed committed review replay: %+v %v", replay, err)
	}
	if _, err := s.SubmitGroundingReview(ctx, "admin-1", types.SubmitGroundingReviewReq{KeyId: key.KeyId,
		CaseJson: caseRaw, ReviewJson: reviewRaw, IdempotencyKey: "review-after-revocation"}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("revoked key signed a new review: %v", err)
	}
	if _, err := s.DB.Exec(ctx, "UPDATE knowledge_reviewer_keys SET reviewer_id='evil' WHERE key_id=$1", key.KeyId); err == nil {
		t.Fatal("immutable key updated")
	}
	if _, err := s.DB.Exec(ctx, "DELETE FROM knowledge_answer_grounding_reviews WHERE case_sha256=$1", receipt.CaseSha256); err == nil {
		t.Fatal("immutable review deleted")
	}
	if _, err := s.DB.Exec(ctx, "UPDATE knowledge_reviewer_key_revocations SET reason='changed' WHERE key_id=$1", key.KeyId); err == nil {
		t.Fatal("immutable revocation updated")
	}
}

func TestReviewerRegistrationConcurrentIdentityCollision(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		sameKeyID  bool
		samePublic bool
	}{
		{name: "key id", sameKeyID: true},
		{name: "public key", samePublic: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := testenv.Store(t)
			publicA, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			publicB, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.samePublic {
				publicB = publicA
			}
			keyA, keyB := "concurrent-reviewer-a", "concurrent-reviewer-b"
			if scenario.sameKeyID {
				keyB = keyA
			}
			requests := [2]types.RegisterReviewerKeyReq{
				{KeyId: keyA, DataKind: "synthetic_fixture", PublicKeyEd25519Hex: hex.EncodeToString(publicA), IdempotencyKey: "concurrent-register-a"},
				{KeyId: keyB, DataKind: "synthetic_fixture", PublicKeyEd25519Hex: hex.EncodeToString(publicB), IdempotencyKey: "concurrent-register-b"},
			}
			start := make(chan struct{})
			var results [2]types.ReviewerKeyRecord
			var errs [2]error
			var wg sync.WaitGroup
			for i := range requests {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					results[i], errs[i] = s.RegisterReviewerKey(ctx, "admin-"+string(rune('1'+i)), requests[i])
				}(i)
			}
			close(start)
			wg.Wait()
			succeeded, conflicted := 0, 0
			for i := range errs {
				switch {
				case errs[i] == nil:
					succeeded++
					if results[i].RegistrationEventId == "" {
						t.Fatal("winning registration has no event")
					}
				case errors.Is(errs[i], model.ErrConflict):
					conflicted++
				default:
					t.Fatalf("collision leaked storage error: %v", errs[i])
				}
			}
			var keys, events int
			if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_reviewer_keys").Scan(&keys); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE event_type='knowledge.reviewer.key.registered.v1'").Scan(&events); err != nil {
				t.Fatal(err)
			}
			if succeeded != 1 || conflicted != 1 || keys != 1 || events != 1 {
				t.Fatalf("registry collision did not have one immutable winner: success=%d conflict=%d keys=%d events=%d", succeeded, conflicted, keys, events)
			}
		})
	}
}

func TestGroundingReviewConcurrentKeysHaveOneCaseWinner(t *testing.T) {
	s := testenv.Store(t)
	var keys [2]types.ReviewerKeyRecord
	var privateKeys [2]ed25519.PrivateKey
	for i := range keys {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		privateKeys[i] = private
		keys[i], err = s.RegisterReviewerKey(ctx, "admin-1", types.RegisterReviewerKeyReq{
			KeyId: "parallel-review-key-" + string(rune('a'+i)), DataKind: "synthetic_fixture",
			PublicKeyEd25519Hex: hex.EncodeToString(public), IdempotencyKey: "parallel-key-register-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	c := acceptedGroundingCase(t, s)
	caseRaw := caseBytes(t, c)
	requests := [2]types.SubmitGroundingReviewReq{}
	for i := range requests {
		requests[i] = types.SubmitGroundingReviewReq{KeyId: keys[i].KeyId, CaseJson: caseRaw,
			ReviewJson:     reviewBytes(t, caseRaw, keys[i], privateKeys[i]),
			IdempotencyKey: "parallel-case-review-" + string(rune('a'+i))}
	}
	start := make(chan struct{})
	var results [2]types.GroundingReviewReceipt
	var errs [2]error
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = s.SubmitGroundingReview(ctx, "admin-1", requests[i])
		}(i)
	}
	close(start)
	wg.Wait()
	succeeded, conflicted := 0, 0
	var winner types.GroundingReviewReceipt
	for i := range errs {
		switch {
		case errs[i] == nil:
			succeeded++
			winner = results[i]
		case errors.Is(errs[i], model.ErrConflict):
			conflicted++
		default:
			t.Fatalf("case collision leaked storage error: %v", errs[i])
		}
	}
	var reviews, events int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_answer_grounding_reviews").Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE event_type='knowledge.answer.grounding.review.accepted.v1'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	read, err := s.GetGroundingReview(ctx, object.Hash([]byte(caseRaw)))
	if err != nil || read != winner || succeeded != 1 || conflicted != 1 || reviews != 1 || events != 1 {
		t.Fatalf("case collision did not converge: read=%+v winner=%+v success=%d conflict=%d reviews=%d events=%d err=%v",
			read, winner, succeeded, conflicted, reviews, events, err)
	}
}

func TestGroundingReviewRejectsForgedSignatureAndSource(t *testing.T) {
	s := testenv.Store(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.RegisterReviewerKey(ctx, "admin-1", types.RegisterReviewerKeyReq{KeyId: "source-mismatch-key",
		DataKind: "synthetic_fixture", PublicKeyEd25519Hex: hex.EncodeToString(public), IdempotencyKey: "source-mismatch-register"})
	key = must(t, key, err)
	c := acceptedGroundingCase(t, s)
	for name, mutate := range map[string]func(*reviewCase){
		"wrong answer": func(c *reviewCase) {
			c.AcceptedAnswer = "another answer"
			c.AcceptedAnswerSHA256 = object.Hash([]byte(c.AcceptedAnswer))
		},
		"wrong quote": func(c *reviewCase) {
			c.Evidence[0].Quote = "forged quote"
			c.Evidence[0].QuoteSHA256 = object.Hash([]byte(c.Evidence[0].Quote))
		},
		"wrong pack":   func(c *reviewCase) { c.CitationPackRef = "search-citations/sha256/" + strings.Repeat("a", 64) },
		"wrong search": func(c *reviewCase) { c.SearchID = "search_other" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			bad.Evidence = append([]reviewCaseEvidence(nil), c.Evidence...)
			mutate(&bad)
			caseRaw := caseBytes(t, bad)
			req := types.SubmitGroundingReviewReq{KeyId: key.KeyId, CaseJson: caseRaw,
				ReviewJson: reviewBytes(t, caseRaw, key, private), IdempotencyKey: "bad-" + strings.ReplaceAll(name, " ", "-") + "-key"}
			if _, err := s.SubmitGroundingReview(ctx, "admin-1", req); err == nil {
				t.Fatal("forged source accepted")
			}
		})
	}
	caseRaw := caseBytes(t, c)
	reviewRaw := reviewBytes(t, caseRaw, key, private)
	var signed signedReview
	if err := json.Unmarshal([]byte(reviewRaw), &signed); err != nil {
		t.Fatal(err)
	}
	signed.Payload.Claims[0].Label = "supported"
	forged, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitGroundingReview(ctx, "admin-1", types.SubmitGroundingReviewReq{KeyId: key.KeyId,
		CaseJson: caseRaw, ReviewJson: string(forged), IdempotencyKey: "bad-signature-key"}); err == nil {
		t.Fatal("forged review payload accepted")
	}
	if _, err := s.SubmitGroundingReview(ctx, "admin-2", types.SubmitGroundingReviewReq{KeyId: key.KeyId,
		CaseJson: caseRaw, ReviewJson: reviewRaw, IdempotencyKey: "wrong-actor-key"}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("another admin used reviewer key: %v", err)
	}
	var count int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_answer_grounding_reviews").Scan(&count); err != nil || count != 0 {
		t.Fatalf("forged review entered PG: count=%d err=%v", count, err)
	}
}

func TestGroundingReviewMigrationAndOutboxRollback(t *testing.T) {
	s := testenv.Store(t)
	if err := s.CheckGroundingReviewSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `DROP TABLE knowledge_answer_grounding_reviews,
 knowledge_reviewer_key_revocations,knowledge_reviewer_keys`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckGroundingReviewSchema(ctx); err == nil {
		t.Fatal("old schema passed enabled review probe")
	}
	migration, err := os.ReadFile("../../../scripts/migrate-grounding-reviews.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.DB.Exec(ctx, string(migration), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
		if err = s.CheckGroundingReviewSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.DB.Exec(ctx, `CREATE FUNCTION reject_reviewer_event() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.event_type='knowledge.reviewer.key.registered.v1' THEN RAISE EXCEPTION 'injected reviewer outbox failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_reviewer_event BEFORE INSERT ON knowledge_outbox FOR EACH ROW EXECUTE FUNCTION reject_reviewer_event()`)
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	req := types.RegisterReviewerKeyReq{KeyId: "rollback-reviewer-key", DataKind: "synthetic_fixture",
		PublicKeyEd25519Hex: hex.EncodeToString(public), IdempotencyKey: "rollback-registration"}
	if _, err := s.RegisterReviewerKey(context.Background(), "admin-1", req); err == nil {
		t.Fatal("outbox failure acknowledged")
	}
	var count int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_reviewer_keys").Scan(&count); err != nil || count != 0 {
		t.Fatalf("key committed without outbox: %d %v", count, err)
	}
	if _, err := s.DB.Exec(ctx, "DROP TRIGGER reject_reviewer_event ON knowledge_outbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterReviewerKey(ctx, "admin-1", req); err != nil {
		t.Fatalf("same key could not recover: %v", err)
	}
}
