package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	workerhandler "sea-try-go/service/knowledge/api/internal/handler/worker"
	"sea-try-go/service/knowledge/api/internal/middleware"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

func wikiSourceVersion(t *testing.T, store *model.Store, moduleID string) int64 {
	t.Helper()
	var version int64
	if err := store.DB.QueryRow(ctx, `SELECT event_sequence FROM knowledge_modules WHERE id=$1`,
		moduleID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func wikiSourceDeliverPending(t *testing.T, store *model.Store, moduleID string) {
	t.Helper()
	if _, err := store.DB.Exec(ctx, `UPDATE knowledge_outbox SET delivered_at=clock_timestamp()
 WHERE aggregate_id=$1 AND delivered_at IS NULL`, moduleID); err != nil {
		t.Fatal(err)
	}
}

func wikiSourceEvents(candidate model.WikiQualitySourceVersionCandidate) []model.WikiQualitySourceVersionEvent {
	var events []model.WikiQualitySourceVersionEvent
	for _, page := range candidate.Pages {
		events = append(events, page.Events...)
	}
	return events
}

func TestWikiQualitySourceVersionCandidatePinsAllModuleOutboxAndWithdrawals(t *testing.T) {
	ordinary, sets, module, sources, _, wiki, request := wikiFactSetFixture(t)
	quality := model.New(ordinary.DB, ordinary.Objects,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	catalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", request)
	catalog = must(t, catalog, err)
	first := make([]types.WikiFactJudgmentRecord, 0, 2)
	for i, fact := range catalog.Facts {
		judged, judgeErr := quality.JudgeWikiFact(ctx, "test-admin",
			snapshotQualityRequest(catalog, fact, "version-first-"+strconv.Itoa(i)))
		first = append(first, must(t, judged, judgeErr))
	}
	target := snapshotTarget(catalog)
	version := wikiSourceVersion(t, ordinary, module.Id)
	// A higher version's technical receipt can arrive before a lower one.
	// The source witness must refuse the partial list rather than infer a
	// continuous watermark from delivered_at or max(version).
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_outbox SET delivered_at=clock_timestamp()
 WHERE aggregate_id=$1 AND payload->>'aggregate_version'=$2`, module.Id,
		strconv.FormatInt(version, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("out-of-order technical receipt manufactured a full source version: %v", err)
	}
	wikiSourceDeliverPending(t, ordinary, module.Id)
	candidate, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version)
	candidate = must(t, candidate, err)
	if candidate.SchemaVersion != "rtw.wiki-quality-source-version-candidate.v1" ||
		candidate.SourceVersion != version || candidate.EventSequenceAtRead != version ||
		candidate.EventCount != int(version) || candidate.ModuleLifecycleAtRead != "ENABLED" ||
		candidate.WikiWithdrawnAtRead || !candidate.WikiAndSourcesAvailableAtRead ||
		!candidate.AllRequiredHaveHead || len(candidate.RequiredHeads) != 2 ||
		candidate.Catalog.EventJCSSHA256 != catalog.EventJcsSha256 ||
		candidate.Catalog.FactSetJCSSHA256 != catalog.FactSetJcsSha256 {
		t.Fatalf("source V lost one-RR Catalog/head/Outbox identity: %+v", candidate)
	}
	all := wikiSourceEvents(candidate)
	if len(all) != int(version) || candidate.Pages[0].FromVersion != 1 ||
		candidate.Pages[len(candidate.Pages)-1].ToVersion != version {
		t.Fatalf("module versions did not cover 1..V: %+v", candidate.Pages)
	}
	seenCatalog, seenJudgment := 0, 0
	for i, event := range all {
		if event.AggregateVersion != int64(i+1) || event.EventID == "" ||
			event.EventJCSSHA256 == "" || event.DeliveredAt == "" ||
			event.TargetKind != "" || event.TargetID != "" {
			t.Fatalf("wrong source version or borrowed withdrawal target at %d: %+v", i, event)
		}
		var raw []byte
		if err := ordinary.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_id=$1`,
			event.EventID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		canonical, err := jsoncanonicalizer.Transform(raw)
		if err != nil || object.Hash(canonical) != event.EventJCSSHA256 {
			t.Fatalf("source Event JCS differs from frozen Outbox JSONB: %s %v", event.EventID, err)
		}
		switch event.EventType {
		case "knowledge.wiki.fact-set.frozen.v1":
			seenCatalog++
			if event.EventID != catalog.EventId || event.JCSSource != "original_fact_set_event_sidecar" {
				t.Fatalf("catalog JCS was not checked against original sidecar: %+v", event)
			}
		case "knowledge.wiki.quality.judged.v1":
			seenJudgment++
			if event.JCSSource != "original_quality_event_sidecar" {
				t.Fatalf("judgment JCS was not checked against original sidecar: %+v", event)
			}
		default:
			if event.JCSSource != "outbox_jsonb_canonical_at_read" {
				t.Fatalf("JSONB-only Event claimed original raw bytes: %+v", event)
			}
		}
	}
	if seenCatalog != 1 || seenJudgment != 2 {
		t.Fatalf("FactSet and required judgments escaped 1..V: catalog=%d judgments=%d",
			seenCatalog, seenJudgment)
	}
	for i, head := range candidate.RequiredHeads {
		if !head.Present || head.FactID != catalog.Facts[i].FactId ||
			head.JudgeRevisionID != first[i].JudgeRevisionId ||
			head.EventID != first[i].EventId || head.EventJCSSHA256 != first[i].EventJcsSha256 {
			t.Fatalf("required current Judge head was read separately from Catalog: %+v", head)
		}
	}
	// Source V is current at its read point. A later current head must never
	// be retrofitted into an earlier requested source version.
	rejudge := snapshotQualityRequest(catalog, catalog.Facts[0], "version-head-after-v")
	rejudge.BaseJudgeRevisionId = first[0].JudgeRevisionId
	rejudge.Reason = "later current Judge head after V"
	after, err := quality.JudgeWikiFact(ctx, "test-admin", rejudge)
	after = must(t, after, err)
	wikiSourceDeliverPending(t, ordinary, module.Id)
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("old V took a newer current Judge head: %v", err)
	}
	version = wikiSourceVersion(t, ordinary, module.Id)
	candidate, err = quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version)
	candidate = must(t, candidate, err)
	latest := map[string]model.WikiQualitySourceVersionRequiredHead{}
	for _, head := range candidate.RequiredHeads {
		latest[head.FactID] = head
	}
	if latest[after.FactId].JudgeRevisionID != after.JudgeRevisionId ||
		latest[after.FactId].EventID != after.EventId {
		t.Fatalf("new source V did not take latest current Judge head: %+v", latest)
	}
	_, err = ordinary.Withdraw(ctx, "test-admin", types.WithdrawReq{
		ModuleId: module.Id, TargetKind: "revision", TargetId: sources[0].RevisionId,
		Reason: "withdraw the first immutable Source", IdempotencyKey: "version-source-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ordinary.Withdraw(ctx, "test-admin", types.WithdrawReq{
		ModuleId: module.Id, TargetKind: "revision", TargetId: wiki.RevisionId,
		Reason: "withdraw the immutable Wiki", IdempotencyKey: "version-wiki-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ordinary.Withdraw(ctx, "test-admin", types.WithdrawReq{
		ModuleId: module.Id, TargetKind: "module", TargetId: module.Id,
		Reason: "withdraw the entire module", IdempotencyKey: "version-module-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	wikiSourceDeliverPending(t, ordinary, module.Id)
	version = wikiSourceVersion(t, ordinary, module.Id)
	candidate, err = quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version)
	candidate = must(t, candidate, err)
	if candidate.ModuleLifecycleAtRead != "WITHDRAWN" || !candidate.WikiWithdrawnAtRead ||
		candidate.WikiAndSourcesAvailableAtRead {
		t.Fatalf("module/Wiki withdrawal was hidden by immutable FactSet: %+v", candidate)
	}
	withdrawn := map[string]bool{}
	for _, source := range candidate.Sources {
		withdrawn[source.RevisionID] = source.Withdrawn
	}
	if !withdrawn[sources[0].RevisionId] || withdrawn[sources[1].RevisionId] {
		t.Fatalf("Source withdrawal status was not pinned: %+v", candidate.Sources)
	}
	targets := map[string]string{}
	for _, event := range wikiSourceEvents(candidate) {
		if event.EventType == "knowledge.content.withdrawn.v1" {
			targets[event.TargetKind+":"+event.TargetID] = event.JCSSource
		}
	}
	for _, id := range []string{sources[0].RevisionId, wiki.RevisionId} {
		if targets["revision:"+id] != "outbox_jsonb_canonical_at_read" {
			t.Fatalf("withdrawal source Event lacks immutable typed target %s: %+v", id, targets)
		}
	}
	if targets["module:"+module.Id] != "outbox_jsonb_canonical_at_read" {
		t.Fatalf("module withdrawal source Event missing: %+v", targets)
	}
}

func TestWikiQualitySourceVersionRejectsOutboxMutationAndVersionGaps(t *testing.T) {
	ordinary, sets, module, sources, _, _, request := wikiFactSetFixture(t)
	quality := model.New(ordinary.DB, ordinary.Objects,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	catalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", request)
	catalog = must(t, catalog, err)
	var qualityEventID string
	for i, fact := range catalog.Facts {
		judged, judgeErr := quality.JudgeWikiFact(ctx, "test-admin",
			snapshotQualityRequest(catalog, fact, "version-guard-"+strconv.Itoa(i)))
		judged = must(t, judged, judgeErr)
		qualityEventID = judged.EventId
	}
	wikiSourceDeliverPending(t, ordinary, module.Id)
	version := wikiSourceVersion(t, ordinary, module.Id)
	target := snapshotTarget(catalog)
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := ordinary.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_id=$1`,
		catalog.EventId).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE knowledge_outbox SET payload='{"forged":true}'::jsonb WHERE event_id=$1`,
		`UPDATE knowledge_outbox SET correlation='{"forged":true}'::jsonb WHERE event_id=$1`,
		`UPDATE knowledge_outbox SET delivered_at=NULL WHERE event_id=$1`,
		`DELETE FROM knowledge_outbox WHERE event_id=$1`,
	} {
		if _, err := ordinary.DB.Exec(ctx, query, catalog.EventId); err == nil {
			t.Fatalf("frozen Outbox accepted mutation/deletion: %s", query)
		}
	}
	var after []byte
	if err := ordinary.DB.QueryRow(ctx, `SELECT payload FROM knowledge_outbox WHERE event_id=$1`,
		catalog.EventId).Scan(&after); err != nil || string(after) != string(before) {
		t.Fatalf("failed business mutation changed original Outbox: %v", err)
	}
	// The explicit migration may be applied twice over existing rows without
	// replacing an old payload or removing a technical receipt.
	migration, err := os.ReadFile("../../../scripts/migrate-wiki-quality-source-version-witness.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := ordinary.DB.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("Outbox guard migration replay %d: %v", i, err)
		}
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); err != nil {
		t.Fatalf("reentrant guard migration invalidated old Event source: %v", err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_revisions SET withdrawn=true WHERE id=$1`,
		sources[0].RevisionId); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("direct Source withdrawal without source Event was signed: %v", err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_revisions SET withdrawn=false WHERE id=$1`,
		sources[0].RevisionId); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_modules
 SET data=jsonb_set(data,'{lifecycle}','"WITHDRAWN"'::jsonb) WHERE id=$1`, module.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("direct module withdrawal without source Event was signed: %v", err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_modules
 SET data=jsonb_set(data,'{lifecycle}','"ENABLED"'::jsonb) WHERE id=$1`, module.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_modules
 SET event_sequence=event_sequence+1 WHERE id=$1`, module.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version+1); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("event_sequence V+1 with missing Outbox version manufactured full source: %v", err)
	}
	// Repaired count but duplicate aggregate_version also fails rather than
	// silently selecting one EventID. This is a synthetic tamper fixture.
	var duplicate model.Event
	duplicate = model.Event{AggregateVersion: version, OperationID: "command:tamper",
		EventID: "evt_duplicate_source_version", EventType: "knowledge.synthetic.tamper.v1",
		SchemaVersion: 1, Producer: "ridethewind.knowledge", AggregateID: module.Id,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Payload: json.RawMessage(`{}`)}
	raw, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.DB.Exec(ctx, `INSERT INTO knowledge_outbox
 (event_id,event_type,aggregate_id,payload,delivered_at) VALUES($1,$2,$3,$4,clock_timestamp())`,
		duplicate.EventID, duplicate.EventType, duplicate.AggregateID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version+1); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("two EventIDs with aggregate_version V manufactured a source candidate: %v", err)
	}
	// A legacy/out-of-band edit is detectable for the two Event families with
	// original byte sidecars. Re-enable the guard before the isolated schema
	// is dropped; non-sidecar types only claim read-time JSONB canonical JCS.
	for _, trigger := range []string{"knowledge_outbox_business_immutable",
		"wiki_fact_set_outbox_identity_immutable", "wiki_quality_outbox_identity_immutable"} {
		if _, err := ordinary.DB.Exec(ctx, `ALTER TABLE knowledge_outbox DISABLE TRIGGER `+trigger); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, trigger := range []string{"knowledge_outbox_business_immutable",
			"wiki_fact_set_outbox_identity_immutable", "wiki_quality_outbox_identity_immutable"} {
			_, _ = ordinary.DB.Exec(context.Background(), `ALTER TABLE knowledge_outbox ENABLE TRIGGER `+trigger)
		}
	}()
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_outbox
 SET payload=jsonb_set(payload,'{operation_id}','"forged"'::jsonb)
 WHERE event_id=$1`, catalog.EventId); err != nil {
		t.Fatal(err)
	}
	// Return event_sequence/count to a valid V before testing sidecar drift.
	if _, err := ordinary.DB.Exec(ctx, `DELETE FROM knowledge_outbox WHERE event_id=$1`,
		duplicate.EventID); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_modules SET event_sequence=$2 WHERE id=$1`,
		module.Id, version); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) && !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("drifted Catalog Outbox passed original sidecar verification: %v", err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_outbox SET payload=$2 WHERE event_id=$1`,
		catalog.EventId, before); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); err != nil {
		t.Fatalf("restored Catalog Outbox did not recover source candidate: %v", err)
	}
	if _, err := ordinary.DB.Exec(ctx, `UPDATE knowledge_outbox
 SET payload=jsonb_set(payload,'{operation_id}','"forged-quality"'::jsonb)
 WHERE event_id=$1`, qualityEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.ReadWikiQualitySourceVersionCandidate(ctx, target, version); !errors.Is(err, model.ErrArtifactUnavailable) && !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("drifted quality Outbox passed original sidecar verification: %v", err)
	}
}

func TestWikiQualitySourceVersionWorkerTokenHTTPUsesOneSourceCandidate(t *testing.T) {
	ordinary, sets, module, _, _, _, request := wikiFactSetFixture(t)
	quality := model.New(ordinary.DB, ordinary.Objects,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	catalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", request)
	catalog = must(t, catalog, err)
	for i, fact := range catalog.Facts {
		_, err := quality.JudgeWikiFact(ctx, "test-admin",
			snapshotQualityRequest(catalog, fact, "version-http-"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	wikiSourceDeliverPending(t, ordinary, module.Id)
	version := wikiSourceVersion(t, ordinary, module.Id)
	input := types.WikiQualitySourceVersionReq{ModuleId: module.Id,
		PageId: catalog.PageId, FactSetRevisionId: catalog.FactSetRevisionId,
		WikiRevisionId:      catalog.WikiRevisionId,
		SourceScopeRevision: catalog.SourceScopeRevision, SourceVersion: version}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/internal/v1/knowledge/wiki-quality/source-version-witness/read"
	handler := middleware.NewWorkerMiddleware("source-worker-token").Handle(
		workerhandler.ReadWikiQualitySourceVersionCandidateHandler(&svc.ServiceContext{Store: quality}))
	requestHTTP := func(token string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler(response, req)
		return response
	}
	for _, token := range []string{"", "admin-user-jwt", "other-worker-token"} {
		if result := requestHTTP(token, raw); result.Code != http.StatusUnauthorized {
			t.Fatalf("non-Worker identity read private source version: token=%q status=%d",
				token, result.Code)
		}
	}
	response := requestHTTP("source-worker-token", raw)
	if response.Code != http.StatusOK {
		t.Fatalf("private Worker source witness failed: %d %s", response.Code, response.Body.String())
	}
	var envelope types.WikiQualitySourceVersionEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil ||
		envelope.Code != 200 || envelope.Data.SourceVersion != version ||
		envelope.Data.EventSequenceAtRead != version || envelope.Data.EventCount != int(version) ||
		envelope.Data.Catalog.FactSetRevisionId != catalog.FactSetRevisionId ||
		envelope.Data.ModuleLifecycleAtRead != "ENABLED" || len(envelope.Data.RequiredHeads) != 2 ||
		len(envelope.Data.Pages) == 0 || !envelope.Data.AllRequiredHaveHead {
		t.Fatalf("Worker wire lost pinned source candidate: %+v %v", envelope, err)
	}
	stale := input
	stale.SourceVersion = version - 1
	staleBody, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if result := requestHTTP("source-worker-token", staleBody); result.Code == http.StatusOK {
		t.Fatal("Worker source endpoint accepted caller's stale source V")
	}
}
