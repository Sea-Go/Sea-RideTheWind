package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/object"
	knowledgeTelemetry "sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"
)

type wikiExternalConsumerRuntime struct {
	SchemaVersion    string `json:"schema_version"`
	RTWBaseURL       string `json:"rtw_base_url"`
	RTWWorkerToken   string `json:"rtw_worker_token"`
	DCBaseURL        string `json:"dc_base_url"`
	DCJobsToken      string `json:"dc_jobs_token"`
	ObjectRoot       string `json:"object_root"`
	CompileID        string `json:"compile_id"`
	SourceRevisionID string `json:"source_revision_id"`
	JobID            string `json:"job_id"`
	JobInputHash     string `json:"job_input_hash"`
	ReleaseFile      string `json:"release_file"`
}

type wikiExternalConsumerReport struct {
	SchemaVersion       string `json:"schema_version"`
	CompileID           string `json:"compile_id"`
	SourceRevisionID    string `json:"source_revision_id"`
	WikiRevisionID      string `json:"wiki_revision_id"`
	RTWAcceptResultHash string `json:"rtw_accept_result_hash"`
	DCJobID             string `json:"dc_job_id"`
	DCJobInputHash      string `json:"dc_job_input_hash"`
	DCAttemptID         string `json:"dc_attempt_id"`
	DCLeaseEpoch        string `json:"dc_lease_epoch"`
	DCCancelVersion     string `json:"dc_cancel_version"`
	DCState             string `json:"dc_state"`
	CandidateContentSHA string `json:"candidate_content_sha256"`
	ManifestSHA         string `json:"manifest_sha256"`
	DCResultHash        string `json:"dc_result_hash"`
	RTWEditHead         string `json:"rtw_edit_head"`
	DCCompletedAttempts int    `json:"dc_completed_attempts"`
	PublishedReleases   int    `json:"published_releases"`
	FixedModelFixture   bool   `json:"fixed_model_fixture"`
	ProductionVerified  bool   `json:"production_verified"`
}

// This explicit holder keeps an actual RTW REST process, DC Jobs process and
// their task-owned PostgreSQL/datastore alive for one external BTW consumer.
// The ordinary private HTTP test takes the deterministic manual fixture path.
func holdWikiCompileExternalConsumer(t *testing.T, source *model.Store,
	directory, objectRoot, rtwBase, workerToken string, c types.Compile, original types.Revision) {
	t.Helper()
	dcRoot := os.Getenv("SEA_DC_JOB_PLATFORM_ROOT")
	if dcRoot == "" || os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR") == "" {
		t.Fatal("external Wiki holder needs explicit DC development checkout and retained task evidence")
	}
	ctx := context.Background()
	var logs bytes.Buffer
	metadata := knowledgeTelemetry.Metadata{Service: "sea-rtw-wiki-holder", Environment: "test",
		Version: "wiki-holder-v1", Instance: "isolated-consumer"}
	writer, err := knowledgeTelemetry.NewWriter(&logs, metadata, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Install("info"); err != nil {
		t.Fatal(err)
	}
	observed, err := knowledgeTelemetry.New(ctx, knowledgeTelemetry.Config{SampleRatio: 1}, writer, metadata)
	if err != nil {
		t.Fatal(err)
	}
	observed.Install()
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := observed.Close(shutdown); err != nil {
			t.Error(err)
		}
		if err := writer.CloseContext(shutdown); err != nil {
			t.Error(err)
		}
		evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")
		if err := os.WriteFile(filepath.Join(evidence, "wiki-holder.jsonl"), logs.Bytes(), 0600); err != nil {
			t.Error(err)
		}
	})
	jobsOwner := model.New(source.DB, source.Objects,
		model.WithObservability(observed), model.WithWikiCompileJobs())
	if err := jobsOwner.CheckWikiCompileJobCandidate(ctx); err != nil {
		t.Fatal(err)
	}
	dc := startRealDCJobPlatform(t, directory, dcRoot)
	transport := &mqs.WikiCompileHTTPTransport{Endpoint: dc.BaseURL + "/v1/jobs",
		Token: dc.Token, Client: &http.Client{Timeout: 10 * time.Second}}
	if sent, err := jobsOwner.DispatchWikiCompileJobOnce(ctx, transport); err != nil || !sent {
		t.Fatalf("committed RTW Compile did not submit one DC Job: sent=%v err=%v", sent, err)
	}
	var jobID, jobInputHash string
	if err := source.DB.QueryRow(ctx, `SELECT job_id::text,submit_input_hash FROM knowledge_compile_jobs
        WHERE compile_id=$1`, c.CompileId).Scan(&jobID, &jobInputHash); err != nil {
		t.Fatal("RTW technical sidecar did not retain DC original Job")
	}
	var technicalState, trueHash string
	if err := dc.Pool.QueryRow(ctx, `SELECT state,input_hash FROM jobs.job WHERE id=$1`, jobID).
		Scan(&technicalState, &trueHash); err != nil || technicalState != "queued" || trueHash != jobInputHash {
		t.Fatalf("DC did not own the frozen queued Wiki Job: state=%q err=%v", technicalState, err)
	}
	evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(evidence, "wiki-external-consumer-runtime.json")
	releasePath := filepath.Join(evidence, "wiki-external-consumer-release")
	runtime := wikiExternalConsumerRuntime{SchemaVersion: "sea.wiki.external-consumer.v1",
		RTWBaseURL: rtwBase, RTWWorkerToken: workerToken, DCBaseURL: dc.BaseURL,
		DCJobsToken: dc.Token, ObjectRoot: objectRoot, CompileID: c.CompileId,
		SourceRevisionID: original.RevisionId, JobID: jobID,
		JobInputHash: jobInputHash, ReleaseFile: releasePath}
	// Only this private task file gives the outside worker authority and root.
	encoded, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("live RTW/DC Wiki consumer ready: %s", runtimePath)
	for deadline := time.Now().Add(5 * time.Minute); ; time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(releasePath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("external BTW Wiki consumer did not release task-owned services")
		}
	}
	accepted, err := source.GetCompile(ctx, c.CompileId)
	if err != nil || accepted.State != "ACCEPTED" || accepted.RevisionId == "" ||
		accepted.InputHash != c.InputHash || accepted.ResultHash == "" {
		t.Fatalf("external BTW failed RTW immutable Wiki business acceptance: state=%q err=%v", accepted.State, err)
	}
	revision, err := source.GetRevision(ctx, accepted.RevisionId)
	if err != nil || revision.Kind != "wiki" || revision.ModuleId != c.ModuleId ||
		revision.EntityId != c.PageId || revision.CreatedBy != "btw.compile/"+c.CompileId ||
		revision.Withdrawn ||
		revision.ContentHash != object.Hash([]byte(revision.Content)) ||
		!strings.HasPrefix(revision.Content, "#") || len(revision.SourceRefs) != 1 ||
		revision.SourceRefs[0].RevisionId != original.RevisionId ||
		revision.SourceRefs[0].Locator != "paragraph:1" {
		t.Fatalf("external BTW Wiki revision did not retain the original RTW source: revision=%s err=%v", accepted.RevisionId, err)
	}
	var uri, manifestHash, media, state string
	if err := dc.Pool.QueryRow(ctx, `SELECT state,result->'result_ref'->>'uri',
        result->'result_ref'->>'sha256',result->'result_ref'->>'media_type'
        FROM jobs.job WHERE id=$1`, jobID).Scan(&state, &uri, &manifestHash, &media); err != nil ||
		state != "succeeded" || manifestHash == "" || uri != "sha256:"+manifestHash ||
		media != "application/vnd.sea.wiki-compile-result+json" {
		t.Fatalf("external BTW Wiki DC technical completion not current: state=%q err=%v", state, err)
	}
	manifestBytes, err := source.Objects.Get(ctx, object.Key(manifestHash), manifestHash)
	if err != nil || object.Hash(manifestBytes) != manifestHash {
		t.Fatal("DC result ref cannot read shared immutable result bytes")
	}
	canonical, err := jsoncanonicalizer.Transform(manifestBytes)
	if err != nil || !bytes.Equal(canonical, manifestBytes) {
		t.Fatal("shared Wiki result object was not the fixed JCS storage bytes")
	}
	var manifest struct {
		SchemaVersion       string `json:"schema_version"`
		CompileID           string `json:"compile_id"`
		CompileInputHash    string `json:"compile_input_hash"`
		Generation          string `json:"generation"`
		JobID               string `json:"job_id"`
		JobInputHash        string `json:"job_input_hash"`
		AttemptID           string `json:"attempt_id"`
		LeaseEpoch          string `json:"lease_epoch"`
		CancelVersion       string `json:"cancel_version"`
		WikiRevisionID      string `json:"wiki_revision_id"`
		CandidateContentSHA string `json:"candidate_content_sha256"`
		RTWAcceptResultHash string `json:"rtw_accept_result_hash"`
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest.SchemaVersion != "sea.wiki.compile-result.v1" ||
		manifest.CompileID != c.CompileId || manifest.JobID != jobID ||
		manifest.JobInputHash != jobInputHash || manifest.CompileInputHash != c.InputHash ||
		manifest.Generation != strconv.FormatInt(c.Generation, 10) ||
		manifest.WikiRevisionID != revision.RevisionId ||
		manifest.CandidateContentSHA != revision.ContentHash ||
		manifest.RTWAcceptResultHash != accepted.ResultHash {
		t.Fatal("DC technical result object does not bind RTW accepted Wiki bytes")
	}
	var manifestFields map[string]json.RawMessage
	if json.Unmarshal(manifestBytes, &manifestFields) != nil || len(manifestFields) != 13 {
		t.Fatal("DC shared result object lost the fixed thirteen-key Wiki contract")
	}
	var editHead string
	if err := source.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
        WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, c.ModuleId, c.PageId).Scan(&editHead); err != nil || editHead != revision.RevisionId {
		t.Fatal("accepted Wiki revision did not move RTW editing head")
	}
	var published int
	if err := source.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_publications WHERE module_id=$1`, c.ModuleId).Scan(&published); err != nil || published != 0 {
		t.Fatal("technical Wiki completion moved the manual published Release")
	}
	var acceptedAttempts int
	if err := dc.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs.attempt WHERE job_id=$1
        AND result_hash IS NOT NULL`, jobID).Scan(&acceptedAttempts); err != nil || acceptedAttempts != 1 {
		t.Fatalf("Wiki Job technical completion lacked one durable attempt: count=%d err=%v", acceptedAttempts, err)
	}
	var dcResultHash, dcAttemptID string
	var dcLeaseEpoch, dcCancelVersion int64
	if err := dc.Pool.QueryRow(ctx, `SELECT result_hash,attempt_id::text,lease_epoch,
        cancel_version FROM jobs.attempt WHERE job_id=$1 AND result_hash IS NOT NULL`, jobID).
		Scan(&dcResultHash, &dcAttemptID, &dcLeaseEpoch, &dcCancelVersion); err != nil ||
		len(dcResultHash) != 64 {
		t.Fatal("one DC technical Result JCS receipt hash was not retained")
	}
	if dcAttemptID != accepted.AttemptId || dcLeaseEpoch != accepted.LeaseEpoch ||
		dcCancelVersion != accepted.CancelVersion || manifest.AttemptID != dcAttemptID ||
		manifest.LeaseEpoch != strconv.FormatInt(dcLeaseEpoch, 10) ||
		manifest.CancelVersion != strconv.FormatInt(dcCancelVersion, 10) {
		t.Fatal("DC technical result attempt differs from RTW accepted Wiki lease")
	}
	report := wikiExternalConsumerReport{SchemaVersion: "sea.wiki.cross-acceptance.v1",
		CompileID: c.CompileId, SourceRevisionID: original.RevisionId,
		WikiRevisionID: revision.RevisionId, RTWAcceptResultHash: accepted.ResultHash,
		DCJobID: jobID, DCJobInputHash: jobInputHash, DCState: state,
		DCAttemptID: dcAttemptID, DCLeaseEpoch: strconv.FormatInt(dcLeaseEpoch, 10),
		DCCancelVersion:     strconv.FormatInt(dcCancelVersion, 10),
		CandidateContentSHA: revision.ContentHash, ManifestSHA: manifestHash,
		DCResultHash: dcResultHash, RTWEditHead: editHead,
		DCCompletedAttempts: acceptedAttempts, PublishedReleases: published,
		FixedModelFixture: true, ProductionVerified: false}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "wiki-holder-result.json"), append(reportBytes, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "wiki-result-manifest-jcs.json"), manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("external Wiki consumer completed RTW revision %s and DC result %s", revision.RevisionId, manifestHash)
}
