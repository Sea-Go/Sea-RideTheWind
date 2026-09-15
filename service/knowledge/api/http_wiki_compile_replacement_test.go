package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

// An old, already claimed Job cannot keep executing at the cost of a newer
// same-page Compile. RTW must emit the supersede fence before DC sees the new
// requested Job, all in the one source command's committed order.
func TestWikiCompileReplacementCancelsOldDCJobBeforeNewSubmit(t *testing.T) {
	dcRoot := os.Getenv("SEA_DC_JOB_PLATFORM_ROOT")
	if os.Getenv("KNOWLEDGE_TEST_DSN") == "" || dcRoot == "" {
		t.Skip("use wiki_compile_job_acceptance.sh with disposable PG16 and actual DC platform")
	}
	ctx := context.Background()
	base := testenv.Store(t)
	module, err := base.CreateModule(ctx, "admin-fixture", types.CreateModuleReq{
		Title: "Wiki generation fence", IdempotencyKey: "replace-module"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := base.CreateSource(ctx, "admin-fixture", types.CreateSourceReq{ModuleId: module.Id,
		Title: "source r1", Content: "version r1", MediaType: "text/plain",
		Provenance: "synthetic", IdempotencyKey: "replace-source"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := base.CreateCompile(ctx, "admin-fixture", types.CreateCompileReq{
		ModuleId: module.Id, PageId: "same-wiki-page", SourceRevisionIds: []string{source.RevisionId},
		Guidance: "first guidance", IdempotencyKey: "replace-first"})
	if err != nil || first.Generation != 1 {
		t.Fatalf("first RTW Compile not fixed: %+v %v", first, err)
	}
	var frozenFirst []byte
	if err := base.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_type=$1
	 AND payload->'payload'->>'compile_id'=$2`, "knowledge.wiki.compile.requested.v1",
		first.CompileId).Scan(&frozenFirst); err != nil {
		t.Fatal(err)
	}
	store := model.New(base.DB, base.Objects, model.WithWikiCompileJobs())
	if err := store.CheckWikiCompileJobCandidate(ctx); err != nil {
		t.Fatal(err)
	}
	dc := startRealDCJobPlatform(t, t.TempDir(), dcRoot)
	transport := &mqs.WikiCompileHTTPTransport{Endpoint: dc.BaseURL + "/v1/jobs",
		Token: dc.Token, Client: &http.Client{Timeout: 3 * time.Second}}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("old requested Job not submitted before replacement: %t %v", sent, err)
	}
	var oldJobID string
	if err := base.DB.QueryRow(ctx, "SELECT job_id::text FROM knowledge_compile_jobs WHERE compile_id=$1",
		first.CompileId).Scan(&oldJobID); err != nil {
		t.Fatal(err)
	}
	var claim struct {
		JobID         string `json:"job_id"`
		AttemptID     string `json:"attempt_id"`
		LeaseEpoch    int64  `json:"lease_epoch"`
		CancelVersion int64  `json:"cancel_version"`
		LeaseExpires  string `json:"lease_expires_at"`
		WorkerID      string `json:"worker_id"`
	}
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/claim", "",
		map[string]any{"worker_id": "wiki-old-worker", "job_type": model.WikiCompileJobType,
			"resource_profile": model.WikiCompileResourceProfile, "lease_seconds": 45},
		http.StatusOK, &claim)
	if claim.JobID != oldJobID || claim.AttemptID == "" || claim.LeaseEpoch != 1 ||
		claim.CancelVersion != 0 || claim.LeaseExpires == "" {
		t.Fatalf("old DC job lease before replacement: %+v", claim)
	}
	leaseExpiry, err := time.Parse(time.RFC3339Nano, claim.LeaseExpires)
	if err != nil || !leaseExpiry.After(time.Now()) {
		t.Fatalf("old DC Job lease expiry invalid: %q %v", claim.LeaseExpires, err)
	}
	if _, err := base.ClaimCompile(ctx, types.ClaimCompileReq{CompileId: first.CompileId,
		Generation: first.Generation, InputHash: first.InputHash, CancelVersion: 0,
		AttemptId: claim.AttemptID, LeaseEpoch: claim.LeaseEpoch,
		LeaseExpiresAt: claim.LeaseExpires}); err != nil {
		t.Fatal(err)
	}
	second, err := base.CreateCompile(ctx, "admin-fixture", types.CreateCompileReq{
		ModuleId: module.Id, PageId: first.PageId,
		SourceRevisionIds: []string{source.RevisionId}, Guidance: "new approved guidance",
		IdempotencyKey: "replace-second"})
	if err != nil || second.Generation != 2 || second.CompileId == first.CompileId {
		t.Fatalf("new RTW generation not fixed: %+v %v", second, err)
	}
	old, err := base.GetCompile(ctx, first.CompileId)
	if err != nil || old.State != "SUPERSEDED" || old.CancelVersion != 1 ||
		old.AttemptId != claim.AttemptID || old.LeaseEpoch != claim.LeaseEpoch {
		t.Fatalf("old RTW attempt was not fenced by source business transaction: %+v %v", old, err)
	}
	var supersedeEvent, newRequestEvent model.Event
	var supersedeRaw, newRequestRaw []byte
	if err := base.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_type=$1
	 AND payload->'payload'->'compile'->>'compile_id'=$2`,
		"knowledge.wiki.compile.superseded.v1", first.CompileId).Scan(&supersedeRaw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(supersedeRaw, &supersedeEvent); err != nil {
		t.Fatal(err)
	}
	if err := base.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_type=$1
	 AND payload->'payload'->>'compile_id'=$2`,
		"knowledge.wiki.compile.requested.v1", second.CompileId).Scan(&newRequestRaw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(newRequestRaw, &newRequestEvent); err != nil {
		t.Fatal(err)
	}
	if supersedeEvent.AggregateVersion >= newRequestEvent.AggregateVersion ||
		supersedeEvent.OperationID != newRequestEvent.OperationID ||
		supersedeEvent.AggregateID != module.Id ||
		!bytes.Contains(supersedeEvent.Payload, []byte(`"replacement_compile_id"`)) {
		t.Fatalf("RTW old fence did not precede new requested Event: old=%+v new=%+v",
			supersedeEvent, newRequestEvent)
	}
	// A second dispatcher can SKIP LOCKED past this old fence. The new
	// requested row is selectable, but the deterministic source dependency
	// must reject its DC Submit before any new technical Job exists.
	held, err := base.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Rollback(context.Background()) })
	var lockedEventID string
	if err := held.QueryRow(ctx, `SELECT event_id FROM knowledge_outbox WHERE event_type=$1
	 AND payload->'payload'->'compile'->>'compile_id'=$2 FOR UPDATE`,
		"knowledge.wiki.compile.superseded.v1", first.CompileId).Scan(&lockedEventID); err != nil || lockedEventID != supersedeEvent.EventID {
		t.Fatalf("could not hold old supersede Outbox row: %s %v", lockedEventID, err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); !errors.Is(err, model.ErrWikiCompilePredecessorPending) || sent {
		t.Fatalf("SKIP LOCKED submitted new Job before old Cancel receipt: %t %v", sent, err)
	}
	var prematureJobs int64
	if err := dc.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs.job WHERE job_type=$1",
		model.WikiCompileJobType).Scan(&prematureJobs); err != nil || prematureJobs != 1 {
		t.Fatalf("new Job appeared while old Cancel row was locked: %d %v", prematureJobs, err)
	}
	if err := held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("old superseded Job cancel did not win source order: %t %v", sent, err)
	}
	oldDC, err := transport.Get(ctx, oldJobID)
	if err != nil || oldDC.CancelVersion != 1 || oldDC.State != "cancel_requested" {
		t.Fatalf("old claimed DC Job continued after newer source generation: %+v %v", oldDC, err)
	}
	var newTechnicalRows int64
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_compile_jobs WHERE compile_id=$1",
		second.CompileId).Scan(&newTechnicalRows); err != nil || newTechnicalRows != 0 {
		t.Fatalf("new Job was submitted before old Job cancellation: %d %v", newTechnicalRows, err)
	}
	if _, err := base.AcceptCompile(ctx, types.AcceptCompileReq{CompileId: first.CompileId,
		Generation: first.Generation, InputHash: first.InputHash, CancelVersion: 0,
		AttemptId: claim.AttemptID, LeaseEpoch: claim.LeaseEpoch, State: "FAILED",
		ErrorCode: "late_old_generation"}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("late old RTW attempt accepted a result after replacement: %v", err)
	}
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/"+oldJobID+"/complete", "",
		map[string]any{"worker_id": claim.WorkerID, "attempt_id": claim.AttemptID,
			"lease_epoch": claim.LeaseEpoch, "cancel_version": 0,
			"result": map[string]any{"technical_state": "failed",
				"error_code": "late_old_generation", "retryable": false}},
		http.StatusConflict, nil)
	if sent, err := store.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("new generation was not submitted after old cancellation: %t %v", sent, err)
	}
	var newJobID string
	if err := base.DB.QueryRow(ctx, "SELECT job_id::text FROM knowledge_compile_jobs WHERE compile_id=$1",
		second.CompileId).Scan(&newJobID); err != nil || newJobID == oldJobID {
		t.Fatalf("new generation reused old Job: new=%s old=%s err=%v", newJobID, oldJobID, err)
	}
	newDC, err := transport.Get(ctx, newJobID)
	if err != nil || newDC.State != "queued" || newDC.CancelVersion != 0 ||
		newDC.Request.JobType != model.WikiCompileJobType ||
		newDC.Request.OperationID != newRequestEvent.OperationID {
		t.Fatalf("new source generation did not create one fresh DC Job: %+v %v", newDC, err)
	}
	var newTicket model.WikiCompileTicket
	if err := json.Unmarshal(newDC.Request.Input, &newTicket); err != nil ||
		newTicket.CompileID != second.CompileId || newTicket.Generation != "2" ||
		newTicket.CancelVersion != "0" {
		t.Fatalf("new DC Job ticket lost source generation: %+v %v", newTicket, err)
	}
	wikiCompileDCHTTP(t, dc, http.MethodPost, "/v1/jobs/"+oldJobID+"/cancel/ack", "",
		map[string]any{"worker_id": claim.WorkerID, "attempt_id": claim.AttemptID,
			"lease_epoch": claim.LeaseEpoch, "cancel_version": 1},
		http.StatusOK, nil)
	oldDC, err = transport.Get(ctx, oldJobID)
	if err != nil || oldDC.State != "cancelled" {
		t.Fatalf("old job lease was not cancelled after replacement: %+v %v", oldDC, err)
	}
	var afterOldRaw []byte
	if err := base.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_type=$1
	 AND payload->'payload'->>'compile_id'=$2`, "knowledge.wiki.compile.requested.v1",
		first.CompileId).Scan(&afterOldRaw); err != nil || !bytes.Equal(afterOldRaw, frozenFirst) {
		t.Fatalf("source replacement rewrote old requested Outbox EventSpec: %v", err)
	}
	var wikiCount, publicationCount, jobsCount int64
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_revisions WHERE kind='wiki'").Scan(&wikiCount); err != nil || wikiCount != 0 {
		t.Fatalf("old/new technical jobs became Wiki revisions: %d %v", wikiCount, err)
	}
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_publications").Scan(&publicationCount); err != nil || publicationCount != 0 {
		t.Fatalf("source replacement changed manual publication: %d %v", publicationCount, err)
	}
	if err := dc.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs.job WHERE job_type=$1",
		model.WikiCompileJobType).Scan(&jobsCount); err != nil || jobsCount != 2 {
		t.Fatalf("two source generations emitted duplicate semantic Jobs: %d %v", jobsCount, err)
	}
}
