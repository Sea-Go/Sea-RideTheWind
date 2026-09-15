package main

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// The actual admin/Worker HTTP process freezes a two-source declaration for
// one AI-accepted Wiki revision and then records each required Fact judgment
// as its own immutable Event. Completeness is the admin's assertion only.
func makeRealWikiFactSetSource(t *testing.T, request httpRequest, store *model.Store,
	module types.Module, sourceA types.Revision, adminToken, workerToken string) (
	types.WikiFactSetRecord, []types.WikiFactJudgmentRecord) {
	t.Helper()
	var sourceB types.Revision
	request("POST", "/v1/knowledge/modules/"+module.Id+"/sources", adminToken,
		types.CreateSourceReq{Title: "Book B original CRLF fact",
			Content:   "Book B\r\n\r\n    Evidence B\r\n    a distinct second fact",
			MediaType: "text/plain", Provenance: "synthetic original Source B",
			IdempotencyKey: "real-fact-set-source-b"}, &sourceB, 200)
	ids := []string{sourceA.RevisionId, sourceB.RevisionId}
	sort.Strings(ids)
	var compile types.Compile
	request("POST", "/v1/knowledge/modules/"+module.Id+"/compiles", adminToken,
		map[string]any{"page_id": "catalog-ai-page", "source_revision_ids": ids,
			"guidance":        "quote both immutable Source paragraphs",
			"idempotency_key": "real-fact-set-two-source-compile"}, &compile, 200)
	if compile.State != "BUILDING" || len(compile.SourceRevisionIds) != 2 ||
		compile.SourceRevisionIds[0] != ids[0] || compile.SourceRevisionIds[1] != ids[1] {
		t.Fatalf("RTW Compile did not freeze full canonical two-source scope: %+v", compile)
	}
	var claim types.Compile
	request("POST", "/internal/v1/knowledge/compiles/"+compile.CompileId+"/claim",
		workerToken, map[string]any{"generation": compile.Generation,
			"attempt_id": "real-fact-set-ai-attempt", "lease_epoch": 1,
			"cancel_version": compile.CancelVersion, "input_hash": compile.InputHash,
			"lease_expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)},
		&claim, 200)
	markdown := "Evidence is cited.\n\n    Evidence B is cited separately."
	key, hash, err := store.Objects.Put(context.Background(), []byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	var accepted types.Compile
	request("POST", "/internal/v1/knowledge/compiles/"+compile.CompileId+"/results",
		workerToken, map[string]any{"state": "READY", "generation": compile.Generation,
			"attempt_id": "real-fact-set-ai-attempt", "lease_epoch": 1,
			"cancel_version": compile.CancelVersion, "input_hash": compile.InputHash,
			"object_key": key, "content_hash": hash, "title": "双来源事实页",
			"source_refs": []types.SourceRef{
				{RevisionId: sourceA.RevisionId, Locator: "paragraph:2"},
				{RevisionId: sourceB.RevisionId, Locator: "paragraph:2"}}},
		&accepted, 200)
	if accepted.State != "ACCEPTED" || accepted.RevisionId == "" ||
		accepted.PageId != "catalog-ai-page" || accepted.CompileId != compile.CompileId {
		t.Fatalf("two-source Wiki revision was not accepted by RTW: %+v", accepted)
	}
	identities := []types.FactSetSourceRevision{
		{RevisionId: sourceA.RevisionId, ContentSha256: sourceA.ContentHash},
		{RevisionId: sourceB.RevisionId, ContentSha256: sourceB.ContentHash}}
	sort.Slice(identities, func(i, j int) bool { return identities[i].RevisionId < identities[j].RevisionId })
	proposed := []types.FactSetInputFact{
		{SourceRevisionId: sourceA.RevisionId, Locator: "paragraph:2",
			SourceQuote: "Evidence", SourceQuoteSha256: object.Hash([]byte("Evidence")), Required: true},
		{SourceRevisionId: sourceB.RevisionId, Locator: "paragraph:2",
			SourceQuote: "Evidence B", SourceQuoteSha256: object.Hash([]byte("Evidence B")), Required: true}}
	catalogPath := "/v1/knowledge/modules/" + module.Id + "/wiki-pages/catalog-ai-page/revisions/" +
		accepted.RevisionId + "/fact-sets"
	var catalog types.WikiFactSetRecord
	request("POST", catalogPath, adminToken, map[string]any{
		"origin_compile_id": compile.CompileId, "source_revisions": identities,
		"facts": proposed, "facts_complete": true,
		"reason":          "synthetic admin declares both approved Source Facts complete",
		"idempotency_key": "real-fact-set-catalog-frozen"}, &catalog, 200)
	if catalog.WikiRevisionId != accepted.RevisionId || catalog.PageId != "catalog-ai-page" ||
		catalog.WikiOriginKind != "ai_accepted" || catalog.OriginCompileId != compile.CompileId ||
		catalog.ActorId != "test-admin" || catalog.DeclarationSource != "admin_jwt_allowlist_declaration" ||
		!catalog.FactsComplete || catalog.FactSetRevision != "1" ||
		len(catalog.SourceRevisions) != 2 || len(catalog.Facts) != 2 ||
		catalog.EventId == "" || catalog.EventRawSha256 == "" ||
		catalog.EventJcsSha256 == "" || catalog.FactSetJcsSha256 == "" {
		t.Fatalf("admin did not freeze exact accepted Source FactCatalog: %+v", catalog)
	}
	byID := map[string]types.Revision{sourceA.RevisionId: sourceA, sourceB.RevisionId: sourceB}
	for _, fact := range catalog.Facts {
		source, ok := byID[fact.SourceRevisionId]
		if !ok || !fact.Required || fact.FactId == "" ||
			fact.SourceContentSha256 != source.ContentHash {
			t.Fatalf("FactCatalog required Fact escaped approved Source identity: %+v", fact)
		}
		original, err := store.Objects.Get(context.Background(), source.ObjectKey, source.ContentHash)
		start, e1 := strconv.Atoi(fact.SourceByteStart)
		end, e2 := strconv.Atoi(fact.SourceByteEnd)
		if err != nil || object.Hash(original) != source.ContentHash ||
			e1 != nil || e2 != nil || start < 0 || end > len(original) ||
			!bytes.Equal(original[start:end], []byte(fact.SourceQuote)) ||
			start != bytes.Index(original, []byte(fact.SourceQuote)) {
			t.Fatalf("FactCatalog quote span did not match original Source byte object: %+v %v", fact, err)
		}
	}
	var pinned types.WikiFactSetRecord
	request("GET", "/v1/knowledge/modules/"+module.Id+
		"/wiki-pages/catalog-ai-page/fact-set-revisions/"+catalog.FactSetRevisionId,
		adminToken, nil, &pinned, 200)
	if pinned.WikiRevisionId != accepted.RevisionId ||
		pinned.SourceScopeRevision != catalog.SourceScopeRevision ||
		pinned.FactSetJcsSha256 != catalog.FactSetJcsSha256 {
		t.Fatalf("history FactSet reader substituted current scope or Wiki: %+v", pinned)
	}
	var catalogEvent types.WikiFactSetEventReceipt
	request("GET", "/internal/v1/knowledge/wiki-fact-sets/events/"+catalog.EventId,
		"", nil, nil, 401)
	request("GET", "/internal/v1/knowledge/wiki-fact-sets/events/"+catalog.EventId,
		workerToken, nil, &catalogEvent, 200)
	canonical, err := jsoncanonicalizer.Transform([]byte(catalogEvent.EventJson))
	if err != nil || catalogEvent.EventId != catalog.EventId ||
		object.Hash([]byte(catalogEvent.EventJson)) != catalog.EventRawSha256 ||
		object.Hash(canonical) != catalog.EventJcsSha256 ||
		catalogEvent.FactSetJcsSha256 != catalog.FactSetJcsSha256 {
		t.Fatalf("actual Worker HTTP could not read RTW Catalog original Event: %+v %v", catalogEvent, err)
	}
	var emitted struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(catalogEvent.EventJson), &emitted) != nil {
		t.Fatal("Catalog source Event has no payload")
	}
	canonical, err = jsoncanonicalizer.Transform(emitted.Payload)
	if err != nil || object.Hash(canonical) != catalog.FactSetJcsSha256 {
		t.Fatalf("actual Worker Catalog payload JCS differs from frozen revision: %v", err)
	}
	qualityPath := "/v1/knowledge/modules/" + module.Id +
		"/wiki-pages/catalog-ai-page/revisions/" + accepted.RevisionId + "/quality-judgments"
	quality := make([]types.WikiFactJudgmentRecord, 0, len(catalog.Facts))
	for _, fact := range catalog.Facts {
		var judged types.WikiFactJudgmentRecord
		request("POST", qualityPath, adminToken, map[string]any{
			"origin_compile_id":     compile.CompileId,
			"source_revision_id":    fact.SourceRevisionId,
			"source_content_sha256": fact.SourceContentSha256,
			"locator":               fact.Locator, "source_quote": fact.SourceQuote,
			"source_quote_sha256": fact.SourceQuoteSha256,
			"wiki_claim_text":     fact.SourceQuote,
			"wiki_claim_sha256":   object.Hash([]byte(fact.SourceQuote)),
			"assessment":          "covered", "grade": "3",
			"rubric_version":  "sea.wiki.fact-coverage.v1",
			"reason":          "synthetic admin judges one required Source Fact in accepted Wiki",
			"idempotency_key": "real-fact-set-judge-" + fact.FactId}, &judged, 200)
		if judged.WikiRevisionId != catalog.WikiRevisionId ||
			judged.FactId != fact.FactId || judged.SourceRevisionId != fact.SourceRevisionId ||
			judged.SourceByteStart != fact.SourceByteStart ||
			judged.SourceByteEnd != fact.SourceByteEnd ||
			judged.SourceQuote != fact.SourceQuote ||
			judged.WikiContentSha256 != catalog.WikiContentSha256 ||
			judged.EventId == "" || judged.EventId == catalog.EventId ||
			judged.Grade != "3" || !judged.CitationPresent {
			t.Fatalf("required Fact lacks its own target Wiki quality Event: %+v", judged)
		}
		var sourceEvent types.WikiQualityEventReceipt
		request("GET", "/internal/v1/knowledge/wiki-quality/events/"+judged.EventId,
			workerToken, nil, &sourceEvent, 200)
		canonical, err := jsoncanonicalizer.Transform([]byte(sourceEvent.EventJson))
		if err != nil || sourceEvent.EventId != judged.EventId ||
			object.Hash([]byte(sourceEvent.EventJson)) != judged.EventRawSha256 ||
			object.Hash(canonical) != judged.EventJcsSha256 {
			t.Fatalf("actual Worker HTTP lost one required quality Event: %+v %v", sourceEvent, err)
		}
		quality = append(quality, judged)
	}
	if len(quality) != 2 || quality[0].EventId == quality[1].EventId {
		t.Fatal("two required Facts were merged into one quality revision/Event")
	}
	return catalog, quality
}
