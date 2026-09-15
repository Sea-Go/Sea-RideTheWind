package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	knowledgeTelemetry "sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

func wikiCompileDCHTTP(t *testing.T, dc *dcJobPlatform, method, path, key string,
	input any, want int, out any) {
	t.Helper()
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, dc.BaseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+dc.Token)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		t.Fatalf("DC Jobs %s %s status %d want %d body=%s", method, path,
			response.StatusCode, want, limited)
	}
	if out != nil && want != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

// Same-run local PG16 + true DataCenter cmd/platform. This is a technical
// Submit/Cancel handoff, not AI compilation or a published Wiki release.
func TestWikiCompileDCJobDelivery(t *testing.T) {
	dcRoot := os.Getenv("SEA_DC_JOB_PLATFORM_ROOT")
	if os.Getenv("KNOWLEDGE_TEST_DSN") == "" || dcRoot == "" {
		t.Skip("use wiki_compile_job_acceptance.sh with disposable PostgreSQL 16 and DC source")
	}
	if os.Getenv("KNOWLEDGE_WIKI_JOB_CHILD") != "1" {
		parent, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		child := exec.CommandContext(parent, os.Args[0], "-test.run=^TestWikiCompileDCJobDelivery$", "-test.v")
		child.Env = append(os.Environ(), "KNOWLEDGE_WIKI_JOB_CHILD=1")
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("isolated Wiki Compile job/telemetry child: %v\n%s", err, output)
		}
		return
	}
	ctx := context.Background()
	var stageLogs bytes.Buffer
	meta := knowledgeTelemetry.Metadata{Service: "sea-rtw-wiki-job-test", Environment: "test",
		Version: "candidate-r1", Instance: "wiki-job-fixture"}
	writer, err := knowledgeTelemetry.NewWriter(&stageLogs, meta, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Install("info"); err != nil {
		t.Fatal(err)
	}
	observed, err := knowledgeTelemetry.New(ctx,
		knowledgeTelemetry.Config{SampleRatio: 1}, writer, meta)
	if err != nil {
		t.Fatal(err)
	}
	observed.Install()
	var closeOnce sync.Once
	closeObserved := func() {
		closeOnce.Do(func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := observed.Close(shutdown); err != nil {
				t.Error(err)
			}
			if err := writer.CloseContext(shutdown); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(closeObserved)
	base := testenv.Store(t)
	m, err := base.CreateModule(ctx, "admin-fixture", types.CreateModuleReq{
		Title: "Wiki Compile Job", IdempotencyKey: "wiki-job-module"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := base.CreateSource(ctx, "admin-fixture", types.CreateSourceReq{
		ModuleId: m.Id, Title: "Fixed source", Content: "Version r1 fixed body",
		MediaType: "text/plain", Provenance: "synthetic", IdempotencyKey: "wiki-job-source"})
	if err != nil {
		t.Fatal(err)
	}
	const guidance = "只根据指定修订编制候选，保留引用定位"
	c, err := base.CreateCompile(ctx, "admin-fixture", types.CreateCompileReq{
		ModuleId: m.Id, PageId: "wiki-page-1", SourceRevisionIds: []string{source.RevisionId},
		Guidance: guidance, IdempotencyKey: "wiki-job-compile"})
	if err != nil || c.State != "BUILDING" || c.Generation != 1 || c.CancelVersion != 0 {
		t.Fatalf("RTW source Compile was not committed: %+v %v", c, err)
	}
	var sourceEventID string
	var originalEvent []byte
	if err := base.DB.QueryRow(ctx, `SELECT event_id,payload FROM knowledge_outbox
	 WHERE event_type='knowledge.wiki.compile.requested.v1' AND payload->'payload'->>'compile_id'=$1`,
		c.CompileId).Scan(&sourceEventID, &originalEvent); err != nil {
		t.Fatal(err)
	}
	canonical, err := jsoncanonicalizer.Transform(originalEvent)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	sourceHash := hex.EncodeToString(sum[:])
	store := model.New(base.DB, base.Objects, model.WithObservability(observed), model.WithWikiCompileJobs())
	if err := store.CheckWikiCompileJobCandidate(ctx); err != nil {
		t.Fatal(err)
	}
	dc := startRealDCJobPlatform(t, t.TempDir(), dcRoot)
	var responseLost, cancelResponseLost, propagatedTrace atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" {
			propagatedTrace.Store(true)
		}
		forward, err := http.NewRequestWithContext(r.Context(), r.Method, dc.BaseURL+r.URL.Path, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		forward.Header = r.Header.Clone()
		upstream, err := (&http.Client{Timeout: 3 * time.Second}).Do(forward)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Body.Close()
		if r.Method == http.MethodPost && r.URL.Path == "/v1/jobs" &&
			upstream.StatusCode == http.StatusCreated && responseLost.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusServiceUnavailable) // DC already committed the original job.
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel") &&
			upstream.StatusCode == http.StatusOK && cancelResponseLost.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusServiceUnavailable) // DC cancellation already fenced the attempt.
			return
		}
		for key, values := range upstream.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(upstream.StatusCode)
		_, _ = io.Copy(w, upstream.Body)
	}))
	defer proxy.Close()
	transport := &mqs.WikiCompileHTTPTransport{Endpoint: proxy.URL + "/v1/jobs",
		Token: dc.Token, Client: &http.Client{Timeout: 3 * time.Second}}
	// DC commits the original job, then the HTTP receipt is lost. RTW keeps its
	// old Outbox pending, repeats the same operation/body and later GETs the
	// original UUID. This is distinct from the local PG ACK-loss test below.
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); !errors.Is(err, model.ErrWikiCompileJobUnavailable) || sent {
		t.Fatalf("lost DC HTTP receipt became RTW delivery: %t %v", sent, err)
	}
	var dcCount, rtwSidecars int64
	if err := dc.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs.job WHERE job_type=$1`, model.WikiCompileJobType).Scan(&dcCount); err != nil || dcCount != 1 {
		t.Fatalf("DC failed to persist job before HTTP response loss: %d %v", dcCount, err)
	}
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_compile_jobs").Scan(&rtwSidecars); err != nil || rtwSidecars != 0 {
		t.Fatalf("lost HTTP response created an RTW technical sidecar: %d %v", rtwSidecars, err)
	}
	if !propagatedTrace.Load() {
		t.Fatal("RTW Wiki job OTel traceparent did not reach the DC HTTP boundary")
	}
	// DC replay returns the one prior receipt, but RTW's local Outbox update
	// now fails. Both sidecar and delivered_at roll back in the same PG txn.
	_, err = base.DB.Exec(ctx, `CREATE FUNCTION reject_wiki_job_outbox_ack() RETURNS trigger
	 LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='knowledge.wiki.compile.requested.v1'
	 AND OLD.delivered_at IS NULL AND NEW.delivered_at IS NOT NULL THEN
	 RAISE EXCEPTION 'test RTW Outbox ACK lost'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER wiki_job_ack_loss BEFORE UPDATE ON knowledge_outbox
	 FOR EACH ROW EXECUTE FUNCTION reject_wiki_job_outbox_ack()`)
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err == nil || sent {
		t.Fatalf("RTW marked a job delivered despite local Outbox ACK loss: %t %v", sent, err)
	}
	if err := dc.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs.job WHERE job_type=$1`, model.WikiCompileJobType).Scan(&dcCount); err != nil || dcCount != 1 {
		t.Fatalf("DC failed to persist one original Compile job: %d %v", dcCount, err)
	}
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_compile_jobs").Scan(&rtwSidecars); err != nil || rtwSidecars != 0 {
		t.Fatalf("RTW sidecar escaped a failed Outbox ACK transaction: %d %v", rtwSidecars, err)
	}
	if _, err := base.DB.Exec(ctx, "DROP TRIGGER wiki_job_ack_loss ON knowledge_outbox; DROP FUNCTION reject_wiki_job_outbox_ack()"); err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("frozen source retry failed to recover original DC job: %t %v", sent, err)
	}
	var jobID, submitHash, savedSourceHash string
	if err := base.DB.QueryRow(ctx, `SELECT job_id::text,submit_input_hash,source_event_jcs_sha256
	 FROM knowledge_compile_jobs WHERE compile_id=$1`, c.CompileId).
		Scan(&jobID, &submitHash, &savedSourceHash); err != nil ||
		jobID == "" || savedSourceHash != sourceHash || len(submitHash) != 64 {
		t.Fatalf("RTW technical receipt not anchored to old EventSpec: job=%s source=%s submit=%s err=%v",
			jobID, savedSourceHash, submitHash, err)
	}
	if err := dc.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs.job WHERE producer=$1 AND operation_id=(
	 SELECT operation_id FROM jobs.job WHERE id=$2)`, "ridethewind.knowledge", jobID).Scan(&dcCount); err != nil || dcCount != 1 {
		t.Fatalf("retry fabricated a second job: %d %v", dcCount, err)
	}
	job, err := transport.Get(ctx, jobID)
	if err != nil || job.InputHash != submitHash || job.Request.JobType != model.WikiCompileJobType ||
		job.Request.ResourceProfile != model.WikiCompileResourceProfile ||
		job.Request.Producer != "ridethewind.knowledge" || job.Request.RunRef != "wiki-compile/"+c.CompileId {
		t.Fatalf("DC durable job differs from RTW sidecar: %+v %v", job, err)
	}
	var ticket model.WikiCompileTicket
	if err := json.Unmarshal(job.Request.Input, &ticket); err != nil {
		t.Fatal(err)
	}
	guidanceSum := sha256.Sum256([]byte(guidance))
	if ticket.SchemaVersion != "rtw.wiki.compile-ticket.v1" ||
		ticket.SourceEventID != sourceEventID || ticket.SourceEventJCSSHA256 != sourceHash ||
		ticket.CompileID != c.CompileId || ticket.ModuleID != c.ModuleId || ticket.PageID != c.PageId ||
		ticket.BaseRevisionID != c.BaseRevisionId ||
		!reflect.DeepEqual(ticket.SourceRevisionIDs, c.SourceRevisionIds) ||
		ticket.GuidanceSHA256 != hex.EncodeToString(guidanceSum[:]) ||
		ticket.CompileInputHash != c.InputHash || ticket.Generation != "1" || ticket.CancelVersion != "0" ||
		bytes.Contains(job.Request.Input, []byte(guidance)) ||
		bytes.Contains(job.Request.Input, []byte("Version r1 fixed body")) {
		t.Fatalf("DC ticket changed RTW source or carried original content/guidance: %+v", ticket)
	}
	// The duplicate operation key cannot create a second semantic job with a
	// different ticket, even though the same DC service token is supplied.
	changed := job.Request
	var modified map[string]any
	if err := json.Unmarshal(changed.Input, &modified); err != nil {
		t.Fatal(err)
	}
	modified["generation"] = "2"
	changed.Input, err = json.Marshal(modified)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Submit(ctx, changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("DC accepted same operation key with different Submit JCS: %v", err)
	}
	// Claim the one job and pass DC's actual attempt/epoch/expiry into the
	// RTW business fence. These are technical fields, not a Wiki revision.
	var claim struct {
		JobID         string `json:"job_id"`
		AttemptID     string `json:"attempt_id"`
		LeaseEpoch    int64  `json:"lease_epoch"`
		CancelVersion int64  `json:"cancel_version"`
		LeaseExpires  string `json:"lease_expires_at"`
		WorkerID      string `json:"worker_id"`
	}
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/claim", "",
		map[string]any{"worker_id": "wiki-local-worker", "job_type": model.WikiCompileJobType,
			"resource_profile": model.WikiCompileResourceProfile, "lease_seconds": 45},
		http.StatusOK, &claim)
	if claim.JobID != jobID || claim.AttemptID == "" || claim.LeaseEpoch != 1 ||
		claim.CancelVersion != 0 || claim.LeaseExpires == "" || claim.WorkerID != "wiki-local-worker" {
		t.Fatalf("DC real Claim fence missing: %+v", claim)
	}
	claimed, err := base.ClaimCompile(ctx, types.ClaimCompileReq{CompileId: c.CompileId,
		Generation: c.Generation, InputHash: c.InputHash, CancelVersion: claim.CancelVersion,
		AttemptId: claim.AttemptID, LeaseEpoch: claim.LeaseEpoch, LeaseExpiresAt: claim.LeaseExpires})
	if err != nil || claimed.AttemptId != claim.AttemptID || claimed.LeaseEpoch != claim.LeaseEpoch {
		t.Fatalf("RTW accepted a different execution fence: %+v %v", claimed, err)
	}
	cancelled, err := base.CancelCompile(ctx, "admin-fixture", types.CancelCompileReq{
		CompileId: c.CompileId, Reason: "newer approved draft", IdempotencyKey: "wiki-job-cancel"})
	if err != nil || cancelled.State != "CANCELLED" || cancelled.CancelVersion != 1 {
		t.Fatalf("RTW cancellation not business-authoritative: %+v %v", cancelled, err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); !errors.Is(err, model.ErrWikiCompileJobUnavailable) || sent {
		t.Fatalf("lost DC cancellation reply became RTW technical ACK: %t %v", sent, err)
	}
	var localCancelVersion int64
	if err := base.DB.QueryRow(ctx, "SELECT cancel_version FROM knowledge_compile_jobs WHERE compile_id=$1",
		c.CompileId).Scan(&localCancelVersion); err != nil || localCancelVersion != 0 {
		t.Fatalf("uncertain cancellation changed RTW sidecar: %d %v", localCancelVersion, err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("same-key DC cancellation replay failed to ACK RTW source: %t %v", sent, err)
	}
	job, err = transport.Get(ctx, jobID)
	if err != nil || job.CancelVersion != 1 || job.State != "cancel_requested" {
		t.Fatalf("DC old attempt survived RTW cancellation: %+v %v", job, err)
	}
	if _, err := base.AcceptCompile(ctx, types.AcceptCompileReq{CompileId: c.CompileId,
		Generation: c.Generation, InputHash: c.InputHash, CancelVersion: 0,
		AttemptId: claim.AttemptID, LeaseEpoch: claim.LeaseEpoch, State: "FAILED",
		ErrorCode: "fixture_stale_after_cancel"}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale RTW attempt wrote a Wiki result after cancellation: %v", err)
	}
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/"+jobID+"/complete", "",
		map[string]any{"worker_id": claim.WorkerID, "attempt_id": claim.AttemptID,
			"lease_epoch": claim.LeaseEpoch, "cancel_version": 0,
			"result": map[string]any{"technical_state": "failed",
				"error_code": "fixture_stale_after_cancel", "retryable": false}},
		http.StatusConflict, nil)
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/"+jobID+"/cancel/ack", "",
		map[string]any{"worker_id": claim.WorkerID, "attempt_id": claim.AttemptID,
			"lease_epoch": claim.LeaseEpoch, "cancel_version": 1}, http.StatusOK, &job)
	if job.State != "cancelled" {
		t.Fatalf("DC cancellation ACK did not finish job: %+v", job)
	}
	var sourceEventAfter []byte
	var outboxDelivered int64
	if err := base.DB.QueryRow(ctx, `SELECT payload,count(*) OVER() FROM knowledge_outbox
	 WHERE event_id=$1 AND delivered_at IS NOT NULL`, sourceEventID).
		Scan(&sourceEventAfter, &outboxDelivered); err != nil ||
		!bytes.Equal(sourceEventAfter, originalEvent) || outboxDelivered != 1 {
		t.Fatalf("RTW original Outbox EventSpec was changed by DC delivery: count=%d err=%v", outboxDelivered, err)
	}
	if _, err := base.DB.Exec(ctx, `UPDATE knowledge_outbox SET payload='{"forged":true}'::jsonb
	 WHERE event_id=$1`, sourceEventID); err == nil {
		t.Fatal("committed Wiki Compile Outbox body could be rewritten after DC acceptance")
	}
	current, err := base.GetCompile(ctx, c.CompileId)
	if err != nil || current.State != "CANCELLED" || current.RevisionId != "" || current.ResultHash != "" {
		t.Fatalf("DC technical receipts became a Wiki revision: %+v %v", current, err)
	}
	var wikiCount int64
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_revisions WHERE kind='wiki'").Scan(&wikiCount); err != nil || wikiCount != 0 {
		t.Fatalf("technical job created a Wiki business revision: count=%d err=%v", wikiCount, err)
	}
	if strings.TrimSpace(job.Request.OperationID) == "" || submitHash == c.InputHash {
		t.Fatal("DC Submit JCS and RTW Compile business hash were conflated")
	}
	metrics := httptest.NewRecorder()
	observed.Metrics().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	metricBody := metrics.Body.String()
	var submitFailed, submitSucceeded, cancelSucceeded bool
	for _, line := range strings.Split(metricBody, "\n") {
		if !strings.HasPrefix(line, "sea_knowledge_operations_total{") {
			continue
		}
		if strings.Contains(line, `operation="knowledge.compile.job.submit"`) {
			submitFailed = submitFailed || strings.Contains(line, `outcome="failed"`)
			submitSucceeded = submitSucceeded || strings.Contains(line, `outcome="succeeded"`)
		}
		if strings.Contains(line, `operation="knowledge.compile.job.cancel"`) {
			cancelSucceeded = cancelSucceeded || strings.Contains(line, `outcome="succeeded"`)
		}
	}
	if metrics.Code != http.StatusOK || !submitFailed || !submitSucceeded || !cancelSucceeded ||
		strings.Contains(metricBody, c.CompileId) || strings.Contains(metricBody, sourceEventID) ||
		strings.Contains(metricBody, `sea_knowledge_commits_total{operation="knowledge.compile.job.submit"}`) ||
		strings.Contains(metricBody, `sea_knowledge_commits_total{operation="knowledge.compile.job.cancel"}`) {
		t.Fatalf("closed-list Wiki job metrics missing or dynamic event ID became label: status=%d", metrics.Code)
	}
	if evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		fixture, err := json.Marshal(map[string]any{"schema_version": "rtw.wiki.compile-job-handoff.v1",
			"ticket": ticket, "submit": job.Request, "dc_submit_input_hash": submitHash,
			"rtw_compile_input_hash": c.InputHash, "source_event_jcs_sha256": sourceHash,
			"job_id": jobID, "claim_attempt_id": claim.AttemptID,
			"claim_lease_epoch": claim.LeaseEpoch, "claim_cancel_version": claim.CancelVersion,
			"claim_lease_expires_at": claim.LeaseExpires,
			"after_cancel_version":   job.CancelVersion, "after_cancel_state": job.State})
		if err != nil {
			t.Fatal(err)
		}
		fixture = append(fixture, '\n')
		if err := os.WriteFile(filepath.Join(evidence, "wiki-compile-job-ticket.jsonl"), fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	closeObserved()
	var loggedFailed, loggedSuccess, loggedCancel bool
	for _, line := range bytes.Split(bytes.TrimSpace(stageLogs.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("Wiki job observability record is not JSON: %s", line)
		}
		if record["event"] == "knowledge.compile.job.submit.failed" {
			loggedFailed = record["error_code"] != nil
		}
		if record["event"] == "knowledge.compile.job.submit.succeeded" {
			loggedSuccess = record["trace_id"] != nil && record["span_id"] != nil &&
				record["operation_id"] != nil
		}
		if record["event"] == "knowledge.compile.job.cancel.succeeded" {
			loggedCancel = record["trace_id"] != nil && record["span_id"] != nil
		}
	}
	if !loggedFailed || !loggedSuccess || !loggedCancel {
		t.Fatalf("Wiki job JSON/OTel terminal outcomes missing: failed=%t success=%t cancel=%t",
			loggedFailed, loggedSuccess, loggedCancel)
	}
}
