package model

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

// This fixed two-source EventSpec gives downstream owners the separate
// FactSet payload JCS, original Event bytes, Event JCS and quote spans.
func TestWikiFactSetEventSpecStaticGolden(t *testing.T) {
	const moduleID = "module_11111111-1111-4111-8111-111111111111"
	const sourceAID = "revision_22222222-2222-4222-8222-222222222222"
	const sourceBID = "revision_33333333-3333-4333-8333-333333333333"
	const wikiID = "revision_44444444-4444-4444-8444-444444444444"
	const pageID = "《双来源完整事实页》"
	sourceA := []byte("header\r\n\r\n    prefix short fact\r\n    suffix short fact")
	sourceB := []byte("header\r\n\r\ncontradicting short fact")
	quoteA, quoteB := "short fact", "contradicting short fact"
	sources := []types.FactSetSourceRevision{
		{RevisionId: sourceAID, ContentSha256: object.Hash(sourceA)},
		{RevisionId: sourceBID, ContentSha256: object.Hash(sourceB)},
	}
	scope, err := wikiFactSetScopeRevision(moduleID, pageID, sources)
	if err != nil {
		t.Fatal(err)
	}
	facts := []types.FactSetFact{
		{FactId: wikiQualityFactID(sourceAID, "paragraph:2", object.Hash([]byte(quoteA))),
			SourceRevisionId: sourceAID, SourceContentSha256: sources[0].ContentSha256,
			Locator: "paragraph:2", SourceByteStart: strconv.Itoa(bytes.Index(sourceA, []byte(quoteA))),
			SourceByteEnd: strconv.Itoa(bytes.Index(sourceA, []byte(quoteA)) + len(quoteA)),
			SourceQuote: quoteA, SourceQuoteSha256: object.Hash([]byte(quoteA)),
			Required: true, ConflictGroup: "conflict_1"},
		{FactId: wikiQualityFactID(sourceBID, "paragraph:2", object.Hash([]byte(quoteB))),
			SourceRevisionId: sourceBID, SourceContentSha256: sources[1].ContentSha256,
			Locator: "paragraph:2", SourceByteStart: strconv.Itoa(bytes.Index(sourceB, []byte(quoteB))),
			SourceByteEnd: strconv.Itoa(bytes.Index(sourceB, []byte(quoteB)) + len(quoteB)),
			SourceQuote: quoteB, SourceQuoteSha256: object.Hash([]byte(quoteB)),
			Required: true, ConflictGroup: "conflict_1"},
	}
	sortWikiFactSetFacts(facts)
	r := wikiFactSetRevision{SchemaVersion: wikiFactSetRevisionSchema,
		FactSetID: "fact_set_55555555-5555-4555-8555-555555555555",
		FactSetRevisionID: "fact_set_revision_66666666-6666-4666-8666-666666666666",
		FactSetRevision: "1", ModuleID: moduleID, PageID: pageID,
		WikiRevisionID: wikiID, WikiOriginKind: "manual_revision",
		WikiContentSHA256: object.Hash([]byte("Two frozen Source facts.")),
		SourceScopeRevision: scope, SourceRevisions: sources, Facts: facts,
		FactsComplete: true, DeclarationSource: wikiFactSetDeclarationSource,
		ActorID: "test-admin", Reason: "human declares this approved scope complete",
		FrozenAt: "2026-09-16T00:00:00Z"}
	payload, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	setSHA, err := wikiQualityJCSHash(payload)
	if err != nil || validateFrozenWikiFactSet(r, setSHA) != nil {
		t.Fatalf("fixed FactSet revision did not pass its own versioned contract: %v", err)
	}
	event := Event{AggregateVersion: 1,
		OperationID: "command:" + object.Hash([]byte("wiki-fact-set/"+wikiID+"/test-admin/fact-set-golden")),
		EventID: "evt_77777777-7777-4777-8777-777777777777",
		EventType: wikiFactSetEventType, SchemaVersion: 1,
		Producer: "ridethewind.knowledge", AggregateID: moduleID,
		OccurredAt: "2026-09-16T00:00:00Z", Payload: payload}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventJCS, err := wikiQualityJCSHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	const wantSetJCS = "3be3a92f9307419911cccf2c72986b6f6af264b9a28537159da461aab0c8ef1a"
	const wantRawSHA = "44050320a905bf0d4dc6bb3a6a8affe36314257577cb972a340023ec8b4136d8"
	const wantEventJCS = "661394315e235904139dea20e9b92f8071cfeb2f9c26ea1e06d4cbb14a3e5b1b"
	if setSHA != wantSetJCS || object.Hash(raw) != wantRawSHA || eventJCS != wantEventJCS {
		t.Fatalf("freeze cross-language FactSet/Event Golden: set_jcs=%s event_raw=%s event_jcs=%s raw=%s",
			setSHA, object.Hash(raw), eventJCS, raw)
	}
	fixture, err := os.ReadFile("../../../testdata/wiki-fact-set-event-v1.json")
	if err != nil || !bytes.Equal(bytes.TrimSuffix(fixture, []byte{'\n'}), raw) {
		t.Fatalf("original FactSet EventSpec fixture bytes differ from typed source: %v", err)
	}
}
