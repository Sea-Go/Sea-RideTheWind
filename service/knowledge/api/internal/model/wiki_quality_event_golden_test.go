package model

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
)

// This one fixed synthetic EventSpec is shared with downstream warehouse and
// evaluation owners. Raw emitted bytes and RFC8785 JCS have different hashes.
func TestWikiQualityEventSpecStaticGolden(t *testing.T) {
	const sourceID = "revision_11111111-1111-4111-8111-111111111111"
	const wikiID = "revision_22222222-2222-4222-8222-222222222222"
	const moduleID = "module_33333333-3333-4333-8333-333333333333"
	const quote = "short fact"
	sourceText := []byte("header\r\n\r\n    prefix short fact\r\n    suffix short fact")
	start := bytes.Index(sourceText, []byte(quote))
	grade := 3
	revision := wikiQualityRevision{SchemaVersion: wikiQualityRevisionSchema,
		JudgmentSource:  wikiQualityJudgmentSource,
		JudgmentID:      "wiki_judgment_44444444-4444-4444-8444-444444444444",
		FactID:          wikiQualityFactID(sourceID, "paragraph:2", object.Hash([]byte(quote))),
		JudgeRevisionID: "judge_revision_55555555-5555-4555-8555-555555555555",
		JudgeRevision:   "1", ModuleID: moduleID, PageID: "《缩进事实》",
		WikiRevisionID: wikiID, WikiOriginKind: "manual_revision",
		WikiContentSHA256: object.Hash([]byte("短句：short fact")),
		SourceRevisionID:  sourceID, SourceContentSHA256: object.Hash(sourceText),
		Locator: "paragraph:2", SourceByteStart: strconv.Itoa(start),
		SourceByteEnd: strconv.Itoa(start + len(quote)),
		SourceQuote:   quote, SourceQuoteSHA256: object.Hash([]byte(quote)),
		WikiClaimText: quote, WikiClaimSHA256: object.Hash([]byte(quote)),
		CitationPresent: true, Assessment: "covered", Grade: &grade,
		RubricVersion: wikiQualityRubric, Reason: "human review of original short quote",
		ActorID: "42", JudgedAt: "2026-09-16T00:00:00Z"}
	payload, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	event := Event{AggregateVersion: 1,
		OperationID: "command:" + object.Hash([]byte("wiki-quality/"+wikiID+"/42/human-quality-golden")),
		EventID:     "evt_66666666-6666-4666-8666-666666666666", EventType: wikiQualityEventType,
		SchemaVersion: 1, Producer: "ridethewind.knowledge", AggregateID: moduleID,
		OccurredAt: "2026-09-16T00:00:00Z", Payload: payload}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	jcsSHA, err := wikiQualityJCSHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	const wantRawSHA = "b8a7f76b4abd10555f2b3f450963bbf518b9919491b714cc909ecb9508b3ac5f"
	const wantJCSSHA = "90e12c15843c7a1ad848da83edf97d4bce218f95eaa16d90afc2f26af02cd7a6"
	if object.Hash(raw) != wantRawSHA || jcsSHA != wantJCSSHA {
		t.Fatalf("new EventSpec golden needs a fixed cross-language hash: raw_sha=%s jcs_sha=%s raw=%s",
			object.Hash(raw), jcsSHA, raw)
	}
	fixture, err := os.ReadFile("../../../testdata/wiki-quality-event-v1.json")
	if err != nil || !bytes.Equal(bytes.TrimSuffix(fixture, []byte{'\n'}), raw) {
		t.Fatalf("original EventSpec fixture bytes differ from the typed emitter: %v", err)
	}
}
