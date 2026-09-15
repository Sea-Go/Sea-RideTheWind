package model

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

const groundingCaseSchema = "sea.search.answer-grounding-case.v1"
const groundingReviewSchema = "sea.search.answer-grounding-review.v1"
const groundingReviewPolicy = "sea.search.answer-grounding-human-review.v1"

// Field order matches the independent BTW case/review JSON contract. These
// DTOs do not import BTW internals or trust the caller's derived hashes.
type groundingEvidence struct {
	ID          string `json:"evidence_id"`
	Quote       string `json:"quote"`
	QuoteSHA256 string `json:"quote_sha256"`
}
type groundingCase struct {
	SchemaVersion           string              `json:"schema_version"`
	CaseID                  string              `json:"case_id"`
	PolicyRevision          string              `json:"policy_revision"`
	ReviewStatus            string              `json:"review_status"`
	Activation              string              `json:"activation"`
	SearchID                string              `json:"search_id"`
	AnswerID                string              `json:"answer_id"`
	ModelInteractionID      string              `json:"model_interaction_id"`
	ModelResponseSHA256     string              `json:"model_response_sha256"`
	RTWAnswerReportSHA256   string              `json:"rtw_answer_report_sha256"`
	DCUsageReportSHA256     string              `json:"dc_usage_report_sha256"`
	RTWTraceLogSHA256       string              `json:"rtw_trace_log_sha256"`
	RTWTraceID              string              `json:"rtw_trace_id"`
	TraceScope              string              `json:"trace_scope"`
	CitationPackRef         string              `json:"citation_pack_ref"`
	Evidence                []groundingEvidence `json:"evidence"`
	AcceptedAnswer          string              `json:"accepted_answer"`
	AcceptedAnswerSHA256    string              `json:"accepted_answer_sha256"`
	PriorQualityObservation string              `json:"prior_quality_observation"`
}
type groundingClaim struct {
	StartByte   int      `json:"start_byte"`
	EndByte     int      `json:"end_byte"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
}
type groundingReviewPayload struct {
	SchemaVersion     string           `json:"schema_version"`
	CaseSHA256        string           `json:"case_sha256"`
	PolicyRevision    string           `json:"policy_revision"`
	DataKind          string           `json:"data_kind"`
	ReviewerAuthority string           `json:"reviewer_authority"`
	ReviewerID        string           `json:"reviewer_id"`
	ReviewedAt        string           `json:"reviewed_at"`
	CoverageComplete  bool             `json:"coverage_complete"`
	Claims            []groundingClaim `json:"claims"`
}
type groundingSignedReview struct {
	Payload   groundingReviewPayload `json:"payload"`
	Signature string                 `json:"signature"`
}

func strictGroundingJSON(raw string, out any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return invalid("malformed grounding JSON")
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return invalid("trailing grounding JSON")
	}
	return nil
}

func validTraceID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func parseGroundingCase(raw string) (groundingCase, string, error) {
	var c groundingCase
	if len(raw) < 2 || len(raw) > 1<<20 {
		return c, "", invalid("bounded case JSON required")
	}
	if err := strictGroundingJSON(raw, &c); err != nil {
		return c, "", err
	}
	if c.SchemaVersion != groundingCaseSchema || c.PolicyRevision != groundingReviewPolicy ||
		c.ReviewStatus != "pending_human_review" || c.Activation != "none" ||
		!citationIdentity(c.SearchID) || !citationIdentity(c.AnswerID) || c.ModelInteractionID == "" ||
		!citationHash(c.ModelResponseSHA256) || !citationHash(c.RTWAnswerReportSHA256) ||
		!citationHash(c.DCUsageReportSHA256) || !citationHash(c.RTWTraceLogSHA256) ||
		!validTraceID(c.RTWTraceID) || c.TraceScope != "rtw_structured_log_not_otlp_collector" ||
		!strings.HasPrefix(c.CitationPackRef, "search-citations/sha256/") ||
		!citationHash(strings.TrimPrefix(c.CitationPackRef, "search-citations/sha256/")) ||
		strings.TrimSpace(c.AcceptedAnswer) == "" || object.Hash([]byte(c.AcceptedAnswer)) != c.AcceptedAnswerSHA256 ||
		len(c.Evidence) == 0 || len(c.Evidence) > 100 {
		return c, "", invalid("incomplete grounding case")
	}
	seen := map[string]bool{}
	for _, e := range c.Evidence {
		if !citationIdentity(e.ID) || seen[e.ID] || e.Quote == "" || object.Hash([]byte(e.Quote)) != e.QuoteSHA256 {
			return c, "", invalid("grounding case evidence differs from quote hash")
		}
		seen[e.ID] = true
	}
	copyCase := c
	copyCase.CaseID = ""
	identity, err := json.Marshal(copyCase)
	if err != nil || c.CaseID != "grounding-"+object.Hash(identity) {
		return c, "", invalid("grounding case ID differs")
	}
	canonical, err := json.MarshalIndent(c, "", "  ")
	if err != nil || !bytes.Equal([]byte(raw), append(canonical, '\n')) {
		return c, "", invalid("noncanonical grounding case bytes")
	}
	return c, object.Hash([]byte(raw)), nil
}

func parseGroundingReview(raw, caseSHA string, key types.ReviewerKeyRecord) (groundingSignedReview, time.Time, error) {
	var review groundingSignedReview
	if len(raw) < 2 || len(raw) > 1<<20 {
		return review, time.Time{}, invalid("bounded review JSON required")
	}
	if err := strictGroundingJSON(raw, &review); err != nil {
		return review, time.Time{}, err
	}
	p := review.Payload
	if p.SchemaVersion != groundingReviewSchema || p.CaseSHA256 != caseSHA ||
		p.PolicyRevision != groundingReviewPolicy || p.DataKind != key.DataKind ||
		p.ReviewerAuthority != key.ReviewerAuthority || p.ReviewerID != key.ReviewerId ||
		len(p.Claims) == 0 || len(p.Claims) > 100 {
		return review, time.Time{}, invalid("review scope differs from registered key or case")
	}
	when, err := time.Parse(time.RFC3339Nano, p.ReviewedAt)
	if err != nil || when.Location() == nil {
		return review, time.Time{}, invalid("review UTC time required")
	}
	_, offset := when.Zone()
	registered, err := time.Parse(time.RFC3339Nano, key.RegisteredAt)
	if err != nil || offset != 0 || when.Before(registered) || when.After(time.Now().UTC().Add(5*time.Minute)) {
		return review, time.Time{}, invalid("review outside registered key interval")
	}
	publicKey, err := hex.DecodeString(key.PublicKeyEd25519Hex)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return review, time.Time{}, ErrArtifactUnavailable
	}
	signature, err := hex.DecodeString(review.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || strings.ToLower(review.Signature) != review.Signature {
		return review, time.Time{}, invalid("Ed25519 signature required")
	}
	encoded, err := json.Marshal(p)
	if err != nil || !ed25519.Verify(publicKey, encoded, signature) {
		return review, time.Time{}, invalid("review Ed25519 signature invalid")
	}
	return review, when, nil
}

func validateGroundingClaims(answer string, evidence []groundingEvidence, p groundingReviewPayload) error {
	allowed := map[string]bool{}
	for _, e := range evidence {
		allowed[e.ID] = true
	}
	answerBytes := []byte(answer)
	covered := make([]bool, len(answerBytes))
	for _, claim := range p.Claims {
		if claim.StartByte < 0 || claim.EndByte <= claim.StartByte || claim.EndByte > len(answerBytes) ||
			!utf8.Valid(answerBytes[:claim.StartByte]) || !utf8.Valid(answerBytes[:claim.EndByte]) ||
			claim.Text != string(answerBytes[claim.StartByte:claim.EndByte]) ||
			strings.TrimSpace(claim.Reason) == "" || len(claim.Reason) > 2000 ||
			len(claim.EvidenceIDs) == 0 || len(claim.EvidenceIDs) > 100 {
			return invalid("invalid claim span or reason")
		}
		switch claim.Label {
		case "supported", "unsupported", "uncertain":
		default:
			return invalid("unknown claim label")
		}
		seen := map[string]bool{}
		for _, id := range claim.EvidenceIDs {
			if !allowed[id] || seen[id] {
				return invalid("claim evidence does not belong to accepted answer")
			}
			seen[id] = true
		}
		for i := claim.StartByte; i < claim.EndByte; i++ {
			if covered[i] {
				return invalid("overlapping claims")
			}
			covered[i] = true
		}
	}
	if p.CoverageComplete {
		for i, b := range answerBytes {
			if b != ' ' && b != '\n' && b != '\t' && !covered[i] {
				return invalid("complete review leaves answer bytes unjudged")
			}
		}
	}
	return nil
}

func verifyGroundingCaseSource(ctx context.Context, tx pgx.Tx, c groundingCase) (string, string, error) {
	var searchID, status, turnHash, turnJSON string
	err := tx.QueryRow(ctx, `SELECT search_id,status,turn_hash,turn_json FROM knowledge_accepted_answers
 WHERE answer_id=$1 FOR SHARE`, c.AnswerID).Scan(&searchID, &status, &turnHash, &turnJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if searchID != c.SearchID || status != "succeeded" || object.Hash([]byte(turnJSON)) != turnHash {
		return "", "", conflict("case differs from accepted answer")
	}
	var turn acceptedRootTurn
	if err = json.Unmarshal([]byte(turnJSON), &turn); err != nil ||
		turn.Request.SearchID != c.SearchID || turn.Request.AnswerID != c.AnswerID ||
		turn.Result.AnswerID != c.AnswerID || turn.Result.SummaryStatus != "succeeded" ||
		turn.Result.Answer != c.AcceptedAnswer {
		return "", "", conflict("case answer differs from accepted turn")
	}
	var packHash, packJSON, durableRef string
	err = tx.QueryRow(ctx, `SELECT pack_hash,pack_json,durable_ref FROM knowledge_search_citations
 WHERE search_id=$1 FOR SHARE`, c.SearchID).Scan(&packHash, &packJSON, &durableRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if object.Hash([]byte(packJSON)) != packHash || durableRef != c.CitationPackRef ||
		turn.Result.Search.Receipt.DurableRef != durableRef || turn.Result.Search.Receipt.PackHash != packHash {
		return "", "", conflict("case citation pack differs from accepted source")
	}
	var pack citationPack
	if err = json.Unmarshal([]byte(packJSON), &pack); err != nil || pack.SearchID != c.SearchID {
		return "", "", ErrArtifactUnavailable
	}
	if len(c.Evidence) != len(turn.Result.Citations) {
		return "", "", conflict("case did not include every cited reference")
	}
	byID := map[string]citationEvidence{}
	for _, e := range pack.Evidence {
		byID[e.ID] = e
	}
	rows, err := tx.Query(ctx, `SELECT evidence_id,quote_hash FROM knowledge_answer_citations
 WHERE answer_id=$1 ORDER BY citation_order`, c.AnswerID)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var id, quoteHash string
		if err = rows.Scan(&id, &quoteHash); err != nil {
			return "", "", err
		}
		if index >= len(c.Evidence) || id != c.Evidence[index].ID || id != turn.Result.Citations[index] ||
			quoteHash != c.Evidence[index].QuoteSHA256 || byID[id].Quote != c.Evidence[index].Quote ||
			byID[id].QuoteHash != quoteHash {
			return "", "", conflict("case quote differs from accepted citation")
		}
		index++
	}
	if err = rows.Err(); err != nil {
		return "", "", err
	}
	if index != len(c.Evidence) {
		return "", "", conflict("case citation count differs")
	}
	return turnHash, packHash, nil
}

func (s *Store) SubmitGroundingReview(ctx context.Context, actor string, req types.SubmitGroundingReviewReq) (types.GroundingReviewReceipt, error) {
	return observe(ctx, s, "knowledge.answer.grounding.review.submit", operationID("grounding-review/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.GroundingReviewReceipt, error) {
			if !validLogicalSessionID(actor) || !citationIdentity(req.KeyId) || len(req.KeyId) > 128 ||
				!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 200 ||
				len(req.CaseJson) > 1<<20 || len(req.ReviewJson) > 1<<20 {
				return types.GroundingReviewReceipt{}, invalid("bounded key, case, review and idempotency required")
			}
			return command(ctx, s, "grounding-review/"+actor, req.IdempotencyKey, req,
				func(tx pgx.Tx) (types.GroundingReviewReceipt, error) {
					key, err := reviewerKey(ctx, tx, req.KeyId, true)
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					if key.Status != "active" || key.ReviewerId != actor {
						return types.GroundingReviewReceipt{}, conflict("reviewer key inactive or owned by another administrator")
					}
					caseValue, caseSHA, err := parseGroundingCase(req.CaseJson)
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					// A case is one immutable review aggregate even when an
					// administrator has more than one active key. Serialize by the
					// derived case identity before the existence check so concurrent
					// submissions have one winner and a stable conflict for the rest.
					if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "grounding-review/case/"+caseSHA); err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					review, when, err := parseGroundingReview(req.ReviewJson, caseSHA, key)
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					if err = validateGroundingClaims(caseValue.AcceptedAnswer, caseValue.Evidence, review.Payload); err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					turnSHA, packSHA, err := verifyGroundingCaseSource(ctx, tx, caseValue)
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					var exists bool
					if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_answer_grounding_reviews WHERE case_sha256=$1)`, caseSHA).Scan(&exists); err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					if exists {
						return types.GroundingReviewReceipt{}, conflict("grounding case already reviewed")
					}
					out := types.GroundingReviewReceipt{SchemaVersion: "sea.rtw.answer-grounding-review-receipt.v1",
						CaseSha256: caseSHA, CaseJson: req.CaseJson, ReviewJson: req.ReviewJson,
						ReviewSha256: object.Hash([]byte(req.ReviewJson)), KeyId: req.KeyId,
						ReviewerAuthority: key.ReviewerAuthority, ReviewerId: key.ReviewerId,
						DataKind: key.DataKind, RegistryRevisionAtCommit: key.RegistryRevision,
						AnswerId: caseValue.AnswerID, SearchId: caseValue.SearchID,
						AcceptedAnswerSha256: caseValue.AcceptedAnswerSHA256, TurnSha256: turnSHA,
						CitationPackRef: caseValue.CitationPackRef, CitationPackSha256: packSHA,
						TraceAuthorityStatus: "external_case_unverified", ReviewedAt: when.UTC().Format(time.RFC3339Nano)}
					event, raw, err := emitWithVersion(ctx, tx, "knowledge.answer.grounding.review.accepted.v1",
						"grounding-review/"+caseSHA, 1, struct {
							CaseSHA256           string `json:"case_sha256"`
							ReviewSHA256         string `json:"review_sha256"`
							KeyID                string `json:"key_id"`
							AnswerID             string `json:"answer_id"`
							SearchID             string `json:"search_id"`
							TraceAuthorityStatus string `json:"trace_authority_status"`
						}{caseSHA, out.ReviewSha256, req.KeyId, out.AnswerId, out.SearchId, out.TraceAuthorityStatus})
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					out.EventId, out.EventSha256 = event.EventID, object.Hash(raw)
					_, err = tx.Exec(ctx, `INSERT INTO knowledge_answer_grounding_reviews
 (case_sha256,key_id,case_json,review_json,review_sha256,reviewer_authority,reviewer_id,data_kind,
 registry_revision_at_commit,answer_id,search_id,accepted_answer_sha256,turn_sha256,
 citation_pack_ref,citation_pack_sha256,trace_authority_status,reviewed_at,event_id,event_json,event_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
						out.CaseSha256, out.KeyId, out.CaseJson, out.ReviewJson, out.ReviewSha256,
						out.ReviewerAuthority, out.ReviewerId, out.DataKind, out.RegistryRevisionAtCommit,
						out.AnswerId, out.SearchId, out.AcceptedAnswerSha256, out.TurnSha256,
						out.CitationPackRef, out.CitationPackSha256, out.TraceAuthorityStatus, when,
						out.EventId, string(raw), out.EventSha256)
					if err != nil {
						return types.GroundingReviewReceipt{}, err
					}
					return out, nil
				})
		})
}

func (s *Store) GetGroundingReview(ctx context.Context, caseSHA string) (types.GroundingReviewReceipt, error) {
	return observe(ctx, s, "knowledge.answer.grounding.review.get", "grounding-review:"+caseSHA,
		types.GroundingReviewCasePath{CaseSha256: caseSHA}, func(ctx context.Context) (types.GroundingReviewReceipt, error) {
			var out types.GroundingReviewReceipt
			if !citationHash(caseSHA) {
				return out, invalid("case SHA-256 required")
			}
			out.SchemaVersion = "sea.rtw.answer-grounding-review-receipt.v1"
			var reviewedAt time.Time
			var eventJSON string
			err := s.DB.QueryRow(ctx, `SELECT r.case_sha256,r.case_json,r.review_json,r.review_sha256,r.key_id,
 r.reviewer_authority,r.reviewer_id,r.data_kind,r.registry_revision_at_commit,
 r.answer_id,r.search_id,r.accepted_answer_sha256,r.turn_sha256,r.citation_pack_ref,
 r.citation_pack_sha256,r.trace_authority_status,r.reviewed_at,r.event_id,r.event_json,r.event_sha256
 FROM knowledge_answer_grounding_reviews r JOIN knowledge_outbox o ON o.event_id=r.event_id
 WHERE r.case_sha256=$1 AND o.event_type='knowledge.answer.grounding.review.accepted.v1'
 AND o.payload=r.event_json::jsonb`, caseSHA).
				Scan(&out.CaseSha256, &out.CaseJson, &out.ReviewJson, &out.ReviewSha256,
					&out.KeyId, &out.ReviewerAuthority, &out.ReviewerId, &out.DataKind,
					&out.RegistryRevisionAtCommit, &out.AnswerId, &out.SearchId,
					&out.AcceptedAnswerSha256, &out.TurnSha256, &out.CitationPackRef,
					&out.CitationPackSha256, &out.TraceAuthorityStatus, &reviewedAt,
					&out.EventId, &eventJSON, &out.EventSha256)
			if errors.Is(err, pgx.ErrNoRows) {
				return out, ErrNotFound
			}
			if err != nil {
				return out, err
			}
			out.ReviewedAt = reviewedAt.UTC().Format(time.RFC3339Nano)
			if object.Hash([]byte(out.CaseJson)) != caseSHA ||
				object.Hash([]byte(out.ReviewJson)) != out.ReviewSha256 ||
				object.Hash([]byte(eventJSON)) != out.EventSha256 ||
				out.TraceAuthorityStatus != "external_case_unverified" || out.RegistryRevisionAtCommit != 1 {
				return types.GroundingReviewReceipt{}, ErrArtifactUnavailable
			}
			return out, nil
		})
}
