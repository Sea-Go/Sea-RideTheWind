package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

type httpReviewEvidence struct {
	ID          string `json:"evidence_id"`
	Quote       string `json:"quote"`
	QuoteSHA256 string `json:"quote_sha256"`
}
type httpReviewCase struct {
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
	Evidence                []httpReviewEvidence `json:"evidence"`
	AcceptedAnswer          string               `json:"accepted_answer"`
	AcceptedAnswerSHA256    string               `json:"accepted_answer_sha256"`
	PriorQualityObservation string               `json:"prior_quality_observation"`
}
type httpReviewClaim struct {
	StartByte   int      `json:"start_byte"`
	EndByte     int      `json:"end_byte"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
}
type httpReviewPayload struct {
	SchemaVersion     string            `json:"schema_version"`
	CaseSHA256        string            `json:"case_sha256"`
	PolicyRevision    string            `json:"policy_revision"`
	DataKind          string            `json:"data_kind"`
	ReviewerAuthority string            `json:"reviewer_authority"`
	ReviewerID        string            `json:"reviewer_id"`
	ReviewedAt        string            `json:"reviewed_at"`
	CoverageComplete  bool              `json:"coverage_complete"`
	Claims            []httpReviewClaim `json:"claims"`
}

func runGroundingReviewHTTP(t *testing.T, request func(string, string, string, any, any, int), base, adminToken,
	nonAdminToken, workerToken, dir, searchID, answerID, answer, evidenceID, quote, quoteHash, packRef string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	register := types.RegisterReviewerKeyReq{KeyId: "http-synthetic-reviewer", DataKind: "synthetic_fixture",
		PublicKeyEd25519Hex: hex.EncodeToString(public), IdempotencyKey: "http-reviewer-register"}
	request("POST", "/v1/knowledge/reviewer-keys", "", register, nil, 401)
	request("POST", "/v1/knowledge/reviewer-keys", nonAdminToken, register, nil, 403)
	var key types.ReviewerKeyRecord
	request("POST", "/v1/knowledge/reviewer-keys", adminToken, register, &key, 200)
	if key.Status != "active" || key.RegistryRevision != 1 || key.ReviewerId != "test-admin" || key.RegistrationEventId == "" {
		t.Fatalf("HTTP registration lost authority: %+v", key)
	}
	keyPath := "/internal/v1/knowledge/reviewer-keys/" + key.KeyId
	request("GET", keyPath, "", nil, nil, 401)
	request("GET", keyPath, nonAdminToken, nil, nil, 401)
	request("GET", "/internal/v1/knowledge/reviewer-keys/missing-reviewer", workerToken, nil, nil, 404)
	var keyRead types.ReviewerKeyRecord
	request("GET", keyPath, workerToken, nil, &keyRead, 200)
	if keyRead != key {
		t.Fatalf("Worker key read differs: %+v %+v", key, keyRead)
	}
	traceID := strings.Repeat("a", 32)
	traceLines := make([]string, 0, 3)
	for _, name := range []string{"knowledge.product.search.succeeded",
		"knowledge.search.citations.accept.succeeded", "knowledge.answer.accept.succeeded"} {
		entry := map[string]any{"event": name, "search_id": searchID, "answer_id": answerID,
			"operation_id": "search-citations:" + searchID, "trace_id": traceID, "outcome": "succeeded"}
		raw, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		traceLines = append(traceLines, string(raw))
	}
	traceRaw := []byte(strings.Join(traceLines, "\n") + "\n")
	rtwRaw, err := json.Marshal(map[string]any{"schema_version": "sea.search.live-summary-rtw.v1",
		"delivery_execution": "observed", "search_id": searchID, "answer_id": answerID,
		"answer": answer, "evidence_id": evidenceID, "quote": quote, "quote_hash": quoteHash,
		"citation_receipt_ref": packRef, "rtw_answer_rows": 1, "rtw_answer_citation_rows": 1,
		"rtw_search_citation_rows": 1, "rtw_product_history_present": true})
	if err != nil {
		t.Fatal(err)
	}
	modelSHA := object.Hash([]byte("synthetic-http-model"))
	usageRaw, err := json.Marshal(map[string]any{"schema_version": "sea.search.live-summary-usage.v1",
		"status": "functional_chain_passed", "search_id": searchID, "answer_id": answerID,
		"rtw_answer_sha256": object.Hash(rtwRaw), "evidence_quote": quote,
		"evidence_quote_sha256": quoteHash, "rtw_accepted_answer": answer,
		"citation_receipt_ref": packRef, "model_interaction_id": "synthetic-http-interaction",
		"model_response_sha256": modelSHA, "quality_gate": "synthetic-only", "activation": "none"})
	if err != nil {
		t.Fatal(err)
	}
	c := httpReviewCase{SchemaVersion: "sea.search.answer-grounding-case.v1",
		PolicyRevision: "sea.search.answer-grounding-human-review.v1", ReviewStatus: "pending_human_review",
		Activation: "none", SearchID: searchID, AnswerID: answerID,
		ModelInteractionID: "synthetic-http-interaction", ModelResponseSHA256: modelSHA,
		RTWAnswerReportSHA256: object.Hash(rtwRaw), DCUsageReportSHA256: object.Hash(usageRaw),
		RTWTraceLogSHA256: object.Hash(traceRaw),
		RTWTraceID:        traceID, TraceScope: "rtw_structured_log_not_otlp_collector",
		CitationPackRef: packRef, Evidence: []httpReviewEvidence{{ID: evidenceID, Quote: quote, QuoteSHA256: quoteHash}},
		AcceptedAnswer: answer, AcceptedAnswerSHA256: object.Hash([]byte(answer)),
		PriorQualityObservation: "synthetic-only"}
	identity, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	c.CaseID = "grounding-" + object.Hash(identity)
	caseBytes, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	caseRaw := string(append(caseBytes, '\n'))
	p := httpReviewPayload{SchemaVersion: "sea.search.answer-grounding-review.v1",
		CaseSHA256: object.Hash([]byte(caseRaw)), PolicyRevision: "sea.search.answer-grounding-human-review.v1",
		DataKind: key.DataKind, ReviewerAuthority: key.ReviewerAuthority, ReviewerID: key.ReviewerId,
		ReviewedAt: time.Now().UTC().Format(time.RFC3339Nano), CoverageComplete: true,
		Claims: []httpReviewClaim{{StartByte: 0, EndByte: len(answer), Text: answer, EvidenceIDs: []string{evidenceID},
			Label: "unsupported", Reason: "synthetic HTTP reviewer test"}}}
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	reviewBytes, err := json.Marshal(struct {
		Payload   httpReviewPayload `json:"payload"`
		Signature string            `json:"signature"`
	}{p, hex.EncodeToString(ed25519.Sign(private, payload))})
	if err != nil {
		t.Fatal(err)
	}
	req := types.SubmitGroundingReviewReq{CaseJson: caseRaw, ReviewJson: string(reviewBytes), KeyId: key.KeyId,
		IdempotencyKey: "http-grounding-review"}
	bad := req
	bad.ReviewJson = strings.Replace(req.ReviewJson, "unsupported", "supported", 1)
	request("POST", "/v1/knowledge/answer-grounding/reviews", "", req, nil, 401)
	request("POST", "/v1/knowledge/answer-grounding/reviews", adminToken, bad, nil, 400)
	request("POST", "/v1/knowledge/answer-grounding/reviews", nonAdminToken, req, nil, 403)
	var accepted types.GroundingReviewReceipt
	request("POST", "/v1/knowledge/answer-grounding/reviews", adminToken, req, &accepted, 200)
	if accepted.CaseSha256 != p.CaseSHA256 || accepted.ReviewSha256 != object.Hash(reviewBytes) ||
		accepted.TraceAuthorityStatus != "external_case_unverified" || accepted.EventSha256 == "" {
		t.Fatalf("HTTP review receipt lacks authority boundary: %+v", accepted)
	}
	reviewPath := "/internal/v1/knowledge/answer-grounding/reviews/" + accepted.CaseSha256
	request("GET", reviewPath, "", nil, nil, 401)
	request("GET", reviewPath, nonAdminToken, nil, nil, 401)
	request("GET", "/internal/v1/knowledge/answer-grounding/reviews/"+strings.Repeat("f", 64), workerToken, nil, nil, 404)
	var retrieved types.GroundingReviewReceipt
	request("GET", reviewPath, workerToken, nil, &retrieved, 200)
	if retrieved != accepted {
		t.Fatalf("Worker review receipt changed: %+v %+v", retrieved, accepted)
	}
	if fixture := os.Getenv("SEA_GROUNDING_REVIEW_BRIDGE_FILE"); fixture != "" {
		casePath := filepath.Join(dir, "grounding-case.json")
		if err := os.WriteFile(casePath, []byte(caseRaw), 0600); err != nil {
			t.Fatal(err)
		}
		usagePath := filepath.Join(dir, "grounding-usage.json")
		if err := os.WriteFile(usagePath, usageRaw, 0600); err != nil {
			t.Fatal(err)
		}
		rtwReportPath := filepath.Join(dir, "grounding-rtw-report.json")
		if err := os.WriteFile(rtwReportPath, rtwRaw, 0600); err != nil {
			t.Fatal(err)
		}
		tracePath := filepath.Join(dir, "grounding-trace.jsonl")
		if err := os.WriteFile(tracePath, traceRaw, 0600); err != nil {
			t.Fatal(err)
		}
		releaseFile := filepath.Join(dir, "grounding-bridge-release")
		revokedReadyFile := filepath.Join(dir, "grounding-bridge-revoked-ready")
		revokedReleaseFile := filepath.Join(dir, "grounding-bridge-revoked-release")
		bridge := struct {
			RTWBase             string `json:"rtw_base"`
			WorkerToken         string `json:"worker_token"`
			CaseSHA256          string `json:"case_sha256"`
			CasePath            string `json:"case_path"`
			UsagePath           string `json:"usage_path"`
			RTWReportPath       string `json:"rtw_report_path"`
			TracePath           string `json:"trace_path"`
			ReviewReceiptSHA256 string `json:"review_receipt_sha256"`
			KeyID               string `json:"key_id"`
			ReleaseFile         string `json:"release_file"`
			RevokedReadyFile    string `json:"revoked_ready_file"`
			RevokedReleaseFile  string `json:"revoked_release_file"`
		}{base, workerToken, accepted.CaseSha256, casePath, usagePath, rtwReportPath, tracePath,
			object.Hash([]byte(accepted.ReviewJson)), key.KeyId, releaseFile, revokedReadyFile, revokedReleaseFile}
		raw, err := json.Marshal(bridge)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, raw, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(90 * time.Second)
		for {
			if _, err := os.Stat(releaseFile); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("BTW bridge did not release RTW HTTP test")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	revoke := types.RevokeReviewerKeyReq{KeyId: key.KeyId, Reason: "synthetic key revoked after review",
		IdempotencyKey: "http-reviewer-revoke"}
	request("POST", "/v1/knowledge/reviewer-keys/"+key.KeyId+"/revoke", "", revoke, nil, 401)
	request("POST", "/v1/knowledge/reviewer-keys/"+key.KeyId+"/revoke", nonAdminToken, revoke, nil, 403)
	var revoked types.ReviewerKeyRecord
	request("POST", "/v1/knowledge/reviewer-keys/"+key.KeyId+"/revoke", adminToken, revoke, &revoked, 200)
	request("GET", keyPath, workerToken, nil, &keyRead, 200)
	if keyRead != revoked || revoked.Status != "revoked" || revoked.RegistryRevision != 2 {
		t.Fatalf("HTTP revocation not visible: %+v %+v", keyRead, revoked)
	}
	request("GET", reviewPath, workerToken, nil, &retrieved, 200)
	if retrieved != accepted {
		t.Fatal("revocation erased historical signed receipt")
	}
	if os.Getenv("SEA_GROUNDING_REVIEW_BRIDGE_FILE") != "" {
		ready := filepath.Join(dir, "grounding-bridge-revoked-ready")
		if err := os.WriteFile(ready, []byte("ready\n"), 0600); err != nil {
			t.Fatal(err)
		}
		release := filepath.Join(dir, "grounding-bridge-revoked-release")
		deadline := time.Now().Add(90 * time.Second)
		for {
			if _, err := os.Stat(release); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("BTW bridge did not release revoked RTW HTTP test")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
