package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// The opt-in real server keeps a preexisting manual Wiki and quality Event in
// the same isolated PG16, then verifies the new default-off FactSet routes.
// It proves admin JWT+allowlist authority only, not realtime UserCenter UID.
func runRealHTTPWikiFactSet(t *testing.T, request httpRequest, module types.Module,
	source, wiki types.Revision, oldQuality types.WikiFactJudgmentRecord,
	adminToken, nonAdminToken, workerToken string, oldQualityEvent types.WikiQualityEventReceipt) {
	t.Helper()
	path := "/v1/knowledge/modules/" + module.Id + "/wiki-pages/" + wiki.EntityId +
		"/revisions/" + wiki.RevisionId + "/fact-sets"
	input := map[string]any{"source_revisions": []types.FactSetSourceRevision{{
		RevisionId: source.RevisionId, ContentSha256: source.ContentHash}},
		"facts": []types.FactSetInputFact{{SourceRevisionId: source.RevisionId,
			Locator: "paragraph:2", SourceQuote: "Evidence",
			SourceQuoteSha256: object.Hash([]byte("Evidence")), Required: true}},
		"facts_complete": true, "reason": "administrator declares this exact Source paragraph complete",
		"idempotency_key": "real-http-fact-set-first"}
	request("POST", path, "", input, nil, 401)
	request("POST", path, nonAdminToken, input, nil, 403)
	bad := map[string]any{}
	for k, v := range input {
		bad[k] = v
	}
	bad["facts_complete"] = false
	request("POST", path, adminToken, bad, nil, 400)
	bad["facts_complete"] = true
	bad["source_revisions"] = []types.FactSetSourceRevision{{
		RevisionId: source.RevisionId, ContentSha256: strings.Repeat("0", 64)}}
	request("POST", path, adminToken, bad, nil, 400)
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := bytes.Replace(raw, []byte(`"required":true`),
		[]byte(`"required":true,"required":false`), 1)
	request("POST", path, adminToken, json.RawMessage(duplicate), nil, 400)
	unknown := bytes.Replace(raw, []byte(`"required":true`),
		[]byte(`"required":true,"grade":"3"`), 1)
	request("POST", path, adminToken, json.RawMessage(unknown), nil, 400)
	var frozen types.WikiFactSetRecord
	request("POST", path, adminToken, input, &frozen, 200)
	if frozen.SchemaVersion != "rtw.wiki.fact-set-revision.v1" ||
		frozen.FactSetRevision != "1" || frozen.WikiRevisionId != wiki.RevisionId ||
		frozen.WikiOriginKind != "manual_revision" || frozen.OriginCompileId != "" ||
		frozen.DeclarationSource != "admin_jwt_allowlist_declaration" ||
		frozen.ActorId != "test-admin" || !frozen.FactsComplete ||
		len(frozen.SourceRevisions) != 1 || len(frozen.Facts) != 1 ||
		frozen.Facts[0].FactId != oldQuality.FactId ||
		frozen.Facts[0].SourceByteStart != "8" || frozen.Facts[0].SourceByteEnd != "16" ||
		frozen.EventId == "" || frozen.FactSetJcsSha256 == "" ||
		frozen.EventRawSha256 == "" || frozen.EventJcsSha256 == "" {
		t.Fatalf("admin FactCatalog failed exact existing Source/quality Fact alignment: %+v", frozen)
	}
	var replay types.WikiFactSetRecord
	request("POST", path, adminToken, input, &replay, 200)
	if !reflect.DeepEqual(replay, frozen) {
		t.Fatalf("same FactSet request did not recover its original Event: %+v %+v", replay, frozen)
	}
	changed := map[string]any{}
	for k, v := range input {
		changed[k] = v
	}
	changed["reason"] = "same key changed declaration"
	request("POST", path, adminToken, changed, nil, 409)
	var current types.WikiFactSetRecord
	scopePath := "/v1/knowledge/modules/" + module.Id + "/wiki-pages/" + wiki.EntityId +
		"/fact-sets/" + frozen.SourceScopeRevision
	request("GET", scopePath, "", nil, nil, 401)
	request("GET", scopePath, nonAdminToken, nil, nil, 403)
	request("GET", scopePath, adminToken, nil, &current, 200)
	if current.FactSetRevisionId != frozen.FactSetRevisionId ||
		current.FactSetJcsSha256 != frozen.FactSetJcsSha256 {
		t.Fatalf("scope current head lost immutable FactSet revision: %+v", current)
	}
	var historical types.WikiFactSetRecord
	request("GET", "/v1/knowledge/modules/"+module.Id+"/wiki-pages/"+wiki.EntityId+
		"/fact-set-revisions/"+frozen.FactSetRevisionId,
		adminToken, nil, &historical, 200)
	if historical.WikiRevisionId != wiki.RevisionId ||
		historical.FactSetRevisionId != frozen.FactSetRevisionId ||
		historical.SourceScopeRevision != frozen.SourceScopeRevision {
		t.Fatalf("history reader substituted current scope head for pinned revision: %+v", historical)
	}
	workerPath := "/internal/v1/knowledge/wiki-fact-sets/events/" + frozen.EventId
	request("GET", workerPath, "", nil, nil, 401)
	var original types.WikiFactSetEventReceipt
	request("GET", workerPath, workerToken, nil, &original, 200)
	if original.EventId != frozen.EventId || original.FactSetJcsSha256 != frozen.FactSetJcsSha256 ||
		object.Hash([]byte(original.EventJson)) != frozen.EventRawSha256 {
		t.Fatalf("Worker lookup rebuilt FactSet Event instead of returning original bytes: %+v", original)
	}
	eventJCS, err := jsoncanonicalizer.Transform([]byte(original.EventJson))
	if err != nil || object.Hash(eventJCS) != frozen.EventJcsSha256 {
		t.Fatalf("Worker FactSet original Event JCS mismatch: %v", err)
	}
	var event struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(original.EventJson), &event) != nil {
		t.Fatal("original FactSet Event payload is invalid JSON")
	}
	setJCS, err := jsoncanonicalizer.Transform(event.Payload)
	if err != nil || object.Hash(setJCS) != frozen.FactSetJcsSha256 {
		t.Fatalf("FactSet payload JCS was conflated with Event hash: %v", err)
	}
	var oldAfter types.WikiQualityEventReceipt
	request("GET", "/internal/v1/knowledge/wiki-quality/events/"+oldQuality.EventId,
		workerToken, nil, &oldAfter, 200)
	if oldAfter.EventJson != oldQualityEvent.EventJson ||
		oldAfter.EventRawSha256 != oldQualityEvent.EventRawSha256 ||
		oldAfter.EventJcsSha256 != oldQualityEvent.EventJcsSha256 {
		t.Fatalf("new FactSet changed the old single-Fact quality Event: %+v", oldAfter)
	}
	var editingHead types.WikiPageHeadSnapshot
	request("GET", "/v1/knowledge/modules/"+module.Id+"/wiki-pages/"+wiki.EntityId+"/head",
		adminToken, nil, &editingHead, 200)
	if editingHead.RevisionId != wiki.RevisionId {
		t.Fatalf("FactSet head wrote Wiki editing head: %+v", editingHead)
	}
	var release types.ReleaseState
	request("GET", "/v1/knowledge/modules/"+module.Id+"/releases/current",
		adminToken, nil, &release, 200)
	if release.PointerRevision != 0 || release.ActiveReleaseId != "" {
		t.Fatalf("FactSet declaration auto-published a Release: %+v", release)
	}
	t.Logf("real admin FactSet HTTP/Worker: revision=%s scope=%s original_event_sha=%s",
		frozen.FactSetRevisionId, frozen.SourceScopeRevision, frozen.EventRawSha256)
}
