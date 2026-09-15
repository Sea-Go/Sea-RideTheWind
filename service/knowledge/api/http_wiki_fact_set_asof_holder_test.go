package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const realWikiAsOfWitnessSchema = "sea.wiki.dc-current-source-alignment.v1"
const realWikiSourceVersionSchema = "rtw.wiki-quality-source-version-candidate.v1"
const realWikiAsOfWitnessDirectory = "wiki-asof-source-witness"

type realWikiAsOfAlignment struct {
	SchemaVersion               string   `json:"schema_version"`
	ModuleID                    string   `json:"module_id"`
	FactSetRevisionID           string   `json:"fact_set_revision_id"`
	WikiRevisionID              string   `json:"wiki_revision_id"`
	SourceScopeRevision         string   `json:"source_scope_revision"`
	SourceVersion               int64    `json:"source_version"`
	DCCutoffOffset              int64    `json:"dc_cutoff_offset"`
	RTWCandidateJCSSHA256       string   `json:"rtw_candidate_jcs_sha256"`
	DCIndexSHA256               string   `json:"dc_index_sha256"`
	ODSEvidenceSHA256           string   `json:"ods_evidence_sha256"`
	CurrentHeadJudgeRevisionIDs []string `json:"current_head_judge_revision_ids"`
	CurrentHeadEventIDs         []string `json:"current_head_event_ids"`
	RTWSourceAndWikiAvailable   bool     `json:"rtw_source_and_wiki_available"`
	QualityState                string   `json:"quality_state"`
	HumanCatalogVerified        bool     `json:"human_catalog_verified"`
	D07Evaluable                bool     `json:"d07_evaluable"`
	ProductionVerified          bool     `json:"production_verified"`
}

type realWikiAsOfEvidence struct {
	SourceVersion   int64
	CandidateSHA256 string
	ReceiptSHA256   string
	QualityState    string
}

// The request closure talks to the actual go-zero subprocess. A model or
// handler-only test would not establish Worker middleware on this new route.
func readRealWorkerWikiAsOfCandidate(t *testing.T, request httpRequest,
	workerToken, moduleID string, catalog types.WikiFactSetRecord,
	required []types.WikiFactJudgmentRecord, version int64) types.WikiQualitySourceVersionCandidate {
	t.Helper()
	input := types.WikiQualitySourceVersionReq{ModuleId: moduleID,
		PageId: catalog.PageId, FactSetRevisionId: catalog.FactSetRevisionId,
		WikiRevisionId:      catalog.WikiRevisionId,
		SourceScopeRevision: catalog.SourceScopeRevision, SourceVersion: version}
	const path = "/internal/v1/knowledge/wiki-quality/source-version-witness/read"
	request("POST", path, "", input, nil, 401)
	var candidate types.WikiQualitySourceVersionCandidate
	request("POST", path, workerToken, input, &candidate, 200)
	if candidate.SchemaVersion != realWikiSourceVersionSchema ||
		candidate.SourceVersion != version || candidate.EventSequenceAtRead != version ||
		candidate.EventCount != int(version) || candidate.Catalog.FactSetRevisionId != catalog.FactSetRevisionId ||
		candidate.Catalog.WikiRevisionId != catalog.WikiRevisionId ||
		candidate.Catalog.SourceScopeRevision != catalog.SourceScopeRevision ||
		candidate.Catalog.EventId != catalog.EventId ||
		candidate.Catalog.EventRawSha256 != catalog.EventRawSha256 ||
		candidate.Catalog.EventJcsSha256 != catalog.EventJcsSha256 ||
		candidate.Catalog.FactSetJcsSha256 != catalog.FactSetJcsSha256 ||
		!candidate.AllRequiredHaveHead || !candidate.WikiAndSourcesAvailableAtRead ||
		candidate.WikiWithdrawnAtRead || candidate.ModuleLifecycleAtRead != "ENABLED" ||
		len(candidate.Sources) != len(catalog.SourceRevisions) ||
		len(candidate.RequiredHeads) != len(required) {
		t.Fatalf("Worker private source V was not the fixed current Wiki/FactSet/required heads: %+v", candidate)
	}
	if version != 14 { // This Holder pins the fourteen-event source fixture.
		t.Fatalf("as-of Holder expected exactly RTW source V14, got %d", version)
	}
	for i, head := range candidate.RequiredHeads {
		if !head.Present || head.FactId != required[i].FactId ||
			head.JudgeRevisionId != required[i].JudgeRevisionId ||
			head.EventId != required[i].EventId ||
			head.EventRawSha256 != required[i].EventRawSha256 ||
			head.EventJcsSha256 != required[i].EventJcsSha256 ||
			head.SourceWithdrawnAtRead {
			t.Fatalf("Worker V14 current required head %d differed from original judgment: %+v", i, head)
		}
	}
	var seen int64
	for _, page := range candidate.Pages {
		if page.FromVersion != seen+1 || page.ToVersion-page.FromVersion+1 != int64(len(page.Events)) {
			t.Fatal("Worker V14 source pages omitted a committed module version")
		}
		for _, event := range page.Events {
			seen++
			if event.AggregateVersion != seen || event.EventId == "" ||
				!realDCSHA(event.EventJcsSha256) || event.DeliveredAt == "" {
				t.Fatal("Worker V14 source event has a hole or unsigned delivery")
			}
		}
	}
	if seen != version {
		t.Fatal("Worker V14 source candidate truncated the delivered prefix")
	}
	return candidate
}

func createRealWikiAsOfWitnessDirectory(t *testing.T, evidenceDir string) string {
	t.Helper()
	if !filepath.IsAbs(evidenceDir) {
		t.Fatal("as-of Holder needs caller-owned absolute Parent observability directory")
	}
	if err := os.MkdirAll(evidenceDir, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Lstat(evidenceDir)
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		t.Fatal("Parent observability evidence is not an ordinary private directory")
	}
	directory := filepath.Join(evidenceDir, realWikiAsOfWitnessDirectory)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatalf("as-of witness directory must be new for this exact Parent generation: %v", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("as-of witness directory is not ordinary private 0700")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("as-of witness directory was not empty before the BTW subprocess")
	}
	return directory
}

func readCanonicalRealWikiAsOfFile(t *testing.T, path string) ([]byte, string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("as-of witness must be an exact ordinary 0600 file: %s %v", path, err)
	}
	raw, err := readRealFactSetResult(path)
	if err != nil || len(raw) < 2 {
		t.Fatalf("as-of witness original bytes absent or oversized: %s %v", path, err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("as-of witness original bytes are not the exact canonical JSON")
	}
	return raw, object.Hash(raw)
}

func readStrictRealWikiAsOfObject[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var result T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("as-of witness has extra, unknown or truncated JSON fields")
	}
	return result
}

func verifyRealWikiAsOfEvidence(t *testing.T, directory string,
	workerCandidate types.WikiQualitySourceVersionCandidate,
	moduleID string, catalog types.WikiFactSetRecord,
	required []types.WikiFactJudgmentRecord,
	sourceProof realWikiFactSetSourceProofResult,
	dc realDCPrefixProof, cutoff int64) realWikiAsOfEvidence {
	t.Helper()
	var evidence realWikiAsOfEvidence
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("Parent as-of evidence is not the ordinary private directory it created")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 || entries[0].Name() != "dc-current-source-alignment.json" ||
		entries[1].Name() != "rtw-source-version-candidate.json" {
		t.Fatal("as-of witness directory has an unexpected or incomplete generation")
	}
	candidateRaw, candidateSHA := readCanonicalRealWikiAsOfFile(t,
		filepath.Join(directory, "rtw-source-version-candidate.json"))
	receiptRaw, receiptSHA := readCanonicalRealWikiAsOfFile(t,
		filepath.Join(directory, "dc-current-source-alignment.json"))
	candidate := readStrictRealWikiAsOfObject[types.WikiQualitySourceVersionCandidate](t, candidateRaw)
	receipt := readStrictRealWikiAsOfObject[realWikiAsOfAlignment](t, receiptRaw)
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receiptCanonical, err := jsoncanonicalizer.Transform(receiptJSON)
	if err != nil || !bytes.Equal(receiptRaw, receiptCanonical) {
		t.Fatal("lasting as-of receipt has duplicate or aliased fields")
	}
	workerRaw, err := json.Marshal(workerCandidate)
	if err != nil {
		t.Fatal(err)
	}
	workerCanonical, err := jsoncanonicalizer.Transform(workerRaw)
	if err != nil || !bytes.Equal(candidateRaw, workerCanonical) ||
		!reflect.DeepEqual(candidate, workerCandidate) {
		t.Fatal("BTW lasting Candidate did not equal RTW Worker subprocess source V")
	}
	if receipt.SchemaVersion != realWikiAsOfWitnessSchema ||
		receipt.ModuleID != moduleID ||
		receipt.FactSetRevisionID != catalog.FactSetRevisionId ||
		receipt.WikiRevisionID != catalog.WikiRevisionId ||
		receipt.SourceScopeRevision != catalog.SourceScopeRevision ||
		receipt.SourceVersion != cutoff || receipt.DCCutoffOffset != cutoff ||
		receipt.RTWCandidateJCSSHA256 != candidateSHA ||
		receipt.DCIndexSHA256 != dc.IndexJCSSHA ||
		receipt.DCIndexSHA256 != sourceProof.DCIndexSHA256 ||
		receipt.ODSEvidenceSHA256 != sourceProof.ODSEvidenceSHA256 ||
		!receipt.RTWSourceAndWikiAvailable || receipt.QualityState != "not_evaluable" ||
		receipt.HumanCatalogVerified || receipt.D07Evaluable ||
		receipt.ProductionVerified || len(receipt.CurrentHeadJudgeRevisionIDs) != len(required) ||
		len(receipt.CurrentHeadEventIDs) != len(required) {
		t.Fatalf("lasting as-of witness did not align RTW V and DC C without human claims: %+v", receipt)
	}
	for i, judged := range required {
		if receipt.CurrentHeadJudgeRevisionIDs[i] != judged.JudgeRevisionId ||
			receipt.CurrentHeadEventIDs[i] != judged.EventId ||
			candidate.RequiredHeads[i].JudgeRevisionId != judged.JudgeRevisionId ||
			candidate.RequiredHeads[i].EventId != judged.EventId {
			t.Fatalf("as-of current head %d did not equal the target Fact source: %+v", i, judged)
		}
	}
	if len(sourceProof.SelectedEventIDs) != 3 ||
		!sameRealFactSetEventIDs(receipt.CurrentHeadEventIDs, sourceProof.SelectedEventIDs[1:]) {
		t.Fatal("as-of required heads did not equal SourceProof's already transported target judgments")
	}
	evidence = realWikiAsOfEvidence{SourceVersion: receipt.SourceVersion,
		CandidateSHA256: candidateSHA, ReceiptSHA256: receiptSHA,
		QualityState: receipt.QualityState}
	return evidence
}
