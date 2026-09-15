package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

type h06PublishedBaseline struct {
	Source  types.Revision
	Release types.Release
	Build   types.Build
	Built   realIndexResult
	Quote   string
}

// A previous publication must be executable by the formal BGE cmd/api. This
// task-only Build uses the actual locked model and Go three-lane builder; it is
// never presented as a production/quality acceptance.
func buildH06PublishedBaseline(t *testing.T, s *model.Store,
	request func(string, string, string, any, any, int), dir, btwRoot, rtwBase,
	workerToken, adminToken, dcRuntime, moduleID string,
	source types.Revision, release types.Release, build types.Build) h06PublishedBaseline {
	t.Helper()
	const original = "baseline\n\npublic"
	var frozen types.Revision
	request("GET", "/internal/v1/knowledge/revisions/"+source.RevisionId, workerToken,
		nil, &frozen, 200)
	if frozen.Content != original || frozen.RevisionId != source.RevisionId ||
		frozen.ContentHash != source.ContentHash || release.ModuleId != moduleID || build.ReleaseId != release.ReleaseId ||
		build.State != "BUILDING" || build.Generation != 1 {
		t.Fatalf("BGE baseline identity differs: source=%+v frozen=%+v release=%+v build=%+v",
			source, frozen, release, build)
	}
	source = frozen
	chunks := make([]types.CitationChunk, 0, 2)
	for i, text := range []string{"baseline", "public"} {
		ordinal := i + 1
		hash := object.Hash([]byte(text))
		identity, err := json.Marshal([]any{source.RevisionId, release.ChunkingProfile, ordinal, 0, hash})
		if err != nil {
			t.Fatal(err)
		}
		encoding, err := json.Marshal([]any{release.ChunkingProfile, text})
		if err != nil {
			t.Fatal(err)
		}
		start := 0
		if i == 1 {
			start = len("baseline\n\n")
		}
		chunks = append(chunks, types.CitationChunk{
			ChunkId: object.Hash(identity), RevisionId: source.RevisionId,
			ContentId: source.EntityId, SourceKind: source.Kind,
			Original: types.CitationObject{Key: source.ObjectKey, Sha256: source.ContentHash},
			Location: types.CitationLocation{Locator: "paragraph:" + strconv.Itoa(ordinal),
				OriginalByteStart: start, OriginalByteEnd: start + len(text),
				NormalizedRuneStart: 0, NormalizedRuneEnd: len([]rune(text))},
			Text: text, TextHash: hash, EncodingKey: object.Hash(encoding), Required: true,
		})
	}
	chunkManifest := map[string]any{
		"schema_version": 1, "module_id": moduleID, "release_id": release.ReleaseId,
		"input_manifest_hash": release.ManifestHash, "profile": release.ChunkingProfile,
		"parser_version": "sea.paragraph.v1", "chunker_version": "trpc.fixed.v1.8.1",
		"chunk_size": 64, "overlap": 0,
		"inputs": []any{map[string]any{"revision_id": source.RevisionId,
			"content_id": source.EntityId, "source_kind": source.Kind,
			"original": chunks[0].Original, "chunk_count": 2}},
		"chunks": chunks,
	}
	chunkRef := testenv.Put(t, s, chunkManifest)
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/claim", workerToken,
		types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano),
			Generation: build.Generation, AttemptId: "h06-baseline-bge", LeaseEpoch: 0,
			ManifestHash: build.ManifestHash}, &build, 200)
	setup := &realIndexSetup{DCRuntime: dcRuntime, ArtifactDir: filepath.Join(dir, "objects"),
		ChunkManifest: chunkRef, BuildID: build.BuildId, ReleaseID: release.ReleaseId,
		Generation: build.Generation, ResultPath: filepath.Join(dir, "h06-baseline-bge-index.json"),
		ExpectedQuote: "public", ExpectedChunkID: chunks[1].ChunkId}
	buildRealBTWIndexesOnly(t, dir, btwRoot, rtwBase, workerToken, moduleID, setup)
	raw, err := os.ReadFile(setup.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	var built realIndexResult
	if err := json.Unmarshal(raw, &built); err != nil || built.ChunkCount != 2 ||
		len(built.Indexes) != 3 || len(built.APIIndexSettings) == 0 {
		t.Fatalf("actual baseline BGE index incomplete: %+v err=%v", built, err)
	}
	indexRaw, err := s.Objects.Get(context.Background(), built.IndexManifest.Key, built.IndexManifest.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	var index model.IndexManifest
	if err := json.Unmarshal(indexRaw, &index); err != nil || index.ChunkManifest != chunkRef ||
		index.ReleaseID != release.ReleaseId || len(index.Lanes) != 3 {
		t.Fatalf("baseline immutable BGE index differs: %+v err=%v", index, err)
	}
	for _, lane := range index.Lanes {
		if !lane.ProbePassed || built.Indexes[lane.Profile.Lane] != lane.Artifact {
			t.Fatalf("baseline BGE lane ref/probe differs: %+v", lane)
		}
	}
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/results", workerToken,
		types.AcceptBuildReq{Generation: build.Generation, AttemptId: build.AttemptId,
			LeaseEpoch: build.LeaseEpoch, ManifestHash: build.ManifestHash, State: "READY",
			IndexManifestRef: built.IndexManifest.Key, IndexManifestHash: built.IndexManifest.SHA256}, &build, 200)
	if build.State != "READY" || build.IndexManifestHash != built.IndexManifest.SHA256 {
		t.Fatalf("baseline BGE READY not accepted: %+v", build)
	}
	var published types.ReleaseState
	request("PUT", "/v1/knowledge/modules/"+moduleID+"/activation", adminToken,
		types.ActivateReq{ReleaseId: release.ReleaseId, BuildId: build.BuildId,
			ExpectedPointerRevision: 0, Reason: "isolated actual-BGE baseline"}, &published, 200)
	if published.ActiveReleaseId != release.ReleaseId || published.ActiveBuildId != build.BuildId ||
		published.PointerRevision != 1 {
		t.Fatalf("actual BGE baseline publication failed: %+v", published)
	}
	return h06PublishedBaseline{Source: source, Release: release, Build: build, Built: built, Quote: "public"}
}

func runH06PublishedSearchCycle(t *testing.T, s *model.Store,
	request func(string, string, string, any, any, int), relay *productSearchFixture,
	dir, btwRoot, rtwBase, workerToken, adminToken, productToken, otherToken,
	searchPath, historyPath, dcRuntime, moduleID string, baseline h06PublishedBaseline,
	newRelease types.Release, newBuild types.Build, newSource types.Revision,
	builtNew realIndexResult, realUserCenter bool, ownerUID int64) {
	t.Helper()
	if newRelease.ReleaseId == baseline.Release.ReleaseId || newBuild.Generation != 2 ||
		newBuild.ReleaseId != newRelease.ReleaseId ||
		builtNew.IndexManifest.SHA256 == baseline.Built.IndexManifest.SHA256 ||
		len(builtNew.APIIndexSettings) == 0 || len(builtNew.Indexes) != 3 {
		t.Fatalf("new published candidate lacks independently built BGE refs: release=%+v build=%+v index=%+v",
			newRelease, newBuild, builtNew)
	}
	snapshotPath := "/internal/v1/knowledge/modules/" + moduleID + "/search-snapshot"
	verifySnapshot := func(releaseID string, generation int64, publication string,
		built realIndexResult, revisionID string) types.SearchSnapshot {
		t.Helper()
		var snapshot types.SearchSnapshot
		request("GET", snapshotPath, workerToken, nil, &snapshot, 200)
		if snapshot.ReleaseId != releaseID || snapshot.Generation != generation ||
			snapshot.PublicationRevision != publication || len(snapshot.Indexes) != 3 ||
			len(snapshot.ValidRevisionIds) != 1 || snapshot.ValidRevisionIds[0] != revisionID {
			t.Fatalf("signed SearchSnapshot identity differs: %+v", snapshot)
		}
		for lane, ref := range built.Indexes {
			got := snapshot.Indexes[lane]
			if got.Key != ref.Key || got.Sha256 != ref.SHA256 {
				t.Fatalf("%s SearchSnapshot index Ref differs: got=%+v want=%+v", lane, got, ref)
			}
		}
		return snapshot
	}
	oldBefore := verifySnapshot(baseline.Release.ReleaseId, baseline.Build.Generation, "1",
		baseline.Built, baseline.Source.RevisionId)
	signed := func(endpoint, query, key, quote, revisionID, contentID string,
		expectedSnapshot types.SearchSnapshot) types.ProductSearchResult {
		t.Helper()
		relay.mu.Lock()
		relay.forwardURL = endpoint
		relay.mu.Unlock()
		relay.stage.Store(4)
		var result types.ProductSearchResult
		request("POST", searchPath, productToken, map[string]any{"module_id": moduleID,
			"query": query, "depth": "fast", "intelligence": "low", "idempotency_key": key}, &result, 200)
		relay.mu.Lock()
		if len(relay.scopes) == 0 {
			relay.mu.Unlock()
			t.Fatal("RTW did not sign the formal search")
		}
		scope := relay.scopes[len(relay.scopes)-1]
		relay.mu.Unlock()
		if scope.SearchID != result.SearchId || scope.AnswerID != result.AnswerId ||
			scope.Subject.SubjectId != strconv.FormatInt(ownerUID, 10) ||
			!reflect.DeepEqual(scope.Snapshot, expectedSnapshot) {
			t.Fatalf("signed owner/SearchID/AnswerID/snapshot differs: scope=%+v result=%+v", scope, result)
		}
		if result.Status != "succeeded" || result.SearchId == "" || result.AnswerId == "" ||
			len(result.Citations) != 1 || result.Citations[0].Quote != quote ||
			result.Citations[0].RevisionId != revisionID ||
			result.Citations[0].ContentId != contentID || result.CitationReceiptRef == "" ||
			!strings.Contains(result.Answer, quote) {
			t.Fatalf("formal signed Go search did not cite fixed published revision %s: %+v", revisionID, result)
		}
		var rows, foreign int
		err := s.DB.QueryRow(context.Background(),
			"SELECT count(*) FILTER (WHERE revision_id=$2),count(*) FILTER (WHERE revision_id<>$2) FROM knowledge_answer_citations WHERE answer_id=$1",
			result.AnswerId, revisionID).Scan(&rows, &foreign)
		if err != nil || rows != 1 || foreign != 0 {
			t.Fatalf("RTW answer citation persisted wrong revision: rows=%d foreign=%d err=%v", rows, foreign, err)
		}
		var other types.AcceptedAnswersPage
		request("GET", historyPath, otherToken, nil, &other, 200)
		for _, item := range other.Items {
			if item.AnswerId == result.AnswerId {
				t.Fatal("signed result leaked to another subject")
			}
		}
		return result
	}
	oldAPI := startRealBTWSearchAPIProcess(t, dir, btwRoot, rtwBase, workerToken,
		dcRuntime, baseline.Built, baseline.Quote)
	oldResult := signed(oldAPI.URL, baseline.Quote, "h06-baseline-before", baseline.Quote,
		baseline.Source.RevisionId, baseline.Source.EntityId, oldBefore)
	oldAPI.AssertSignedSearch(t, oldResult.SearchId, oldResult.AnswerId)

	var activated types.ReleaseState
	request("PUT", "/v1/knowledge/modules/"+moduleID+"/activation", adminToken,
		types.ActivateReq{ReleaseId: newRelease.ReleaseId, BuildId: newBuild.BuildId,
			ExpectedPointerRevision: 1, Reason: "isolated new BGE release publication"}, &activated, 200)
	if activated.ActiveReleaseId != newRelease.ReleaseId || activated.ActiveBuildId != newBuild.BuildId ||
		activated.PointerRevision != 2 {
		t.Fatalf("admin publication of new READY index failed: %+v", activated)
	}
	newSnapshot := verifySnapshot(newRelease.ReleaseId, newBuild.Generation, "2",
		builtNew, newSource.RevisionId)
	newAPI := startRealBTWSearchAPIProcess(t, dir, btwRoot, rtwBase, workerToken, dcRuntime, builtNew, "south")
	newResult := signed(newAPI.URL, "south", "h06-new-release-south", "south",
		newSource.RevisionId, newSource.EntityId, newSnapshot)
	newAPI.AssertSignedSearch(t, newResult.SearchId, newResult.AnswerId)
	var history types.AcceptedAnswersPage
	request("GET", historyPath+"?limit=20", productToken, nil, &history, 200)
	foundOld, foundNew := false, false
	for _, item := range history.Items {
		if item.AnswerId == oldResult.AnswerId {
			foundOld = true
		}
		if item.AnswerId == newResult.AnswerId {
			foundNew = true
		}
	}
	if !foundOld || !foundNew {
		t.Fatalf("RTW owner history omitted signed old/new answers: old=%t new=%t", foundOld, foundNew)
	}
	var newCitationState types.ProductAnswerCitationStates
	request("GET", historyPath+"/"+newResult.AnswerId+"/citations", productToken, nil, &newCitationState, 200)
	if newCitationState.ReleaseId != newRelease.ReleaseId ||
		newCitationState.PublicationRevision != "2" ||
		len(newCitationState.Citations) != 1 ||
		newCitationState.Citations[0].EvidenceId != newResult.Citations[0].EvidenceId {
		t.Fatalf("new answer history/citation state mixed old revision: %+v", newCitationState)
	}

	request("PUT", "/v1/knowledge/modules/"+moduleID+"/activation", adminToken,
		types.ActivateReq{ReleaseId: baseline.Release.ReleaseId, BuildId: baseline.Build.BuildId,
			ExpectedPointerRevision: 2, Reason: "isolated rollback to baseline BGE release"}, &activated, 200)
	if activated.ActiveReleaseId != baseline.Release.ReleaseId ||
		activated.ActiveBuildId != baseline.Build.BuildId || activated.PointerRevision != 3 {
		t.Fatalf("admin rollback did not restore old published index: %+v", activated)
	}
	oldAfter := verifySnapshot(baseline.Release.ReleaseId, baseline.Build.Generation, "3",
		baseline.Built, baseline.Source.RevisionId)
	rollbackAPI := startRealBTWSearchAPIProcess(t, dir, btwRoot, rtwBase, workerToken,
		dcRuntime, baseline.Built, baseline.Quote)
	rollbackResult := signed(rollbackAPI.URL, baseline.Quote, "h06-baseline-after", baseline.Quote,
		baseline.Source.RevisionId, baseline.Source.EntityId, oldAfter)
	rollbackAPI.AssertSignedSearch(t, rollbackResult.SearchId, rollbackResult.AnswerId)
	request("GET", historyPath+"?limit=20", productToken, nil, &history, 200)
	foundOld, foundNew, foundRollback := false, false, false
	for _, item := range history.Items {
		if item.AnswerId == oldResult.AnswerId {
			foundOld = true
		}
		if item.AnswerId == newResult.AnswerId {
			foundNew = true
		}
		if item.AnswerId == rollbackResult.AnswerId {
			foundRollback = true
		}
	}
	if !foundOld || !foundNew || !foundRollback {
		t.Fatalf("rollback erased immutable accepted answer history: %t %t %t",
			foundOld, foundNew, foundRollback)
	}
	if oldBefore.Indexes["dense"] != oldAfter.Indexes["dense"] ||
		oldBefore.PublicationRevision == oldAfter.PublicationRevision ||
		oldBefore.Indexes["dense"] == newSnapshot.Indexes["dense"] {
		t.Fatal("rollback search did not restore old immutable Dense Ref")
	}
	report, err := json.Marshal(map[string]any{
		"baseline_release_id": baseline.Release.ReleaseId, "baseline_build_id": baseline.Build.BuildId,
		"baseline_index_sha256":       baseline.Built.IndexManifest.SHA256,
		"baseline_source_revision_id": baseline.Source.RevisionId,
		"baseline_lane_refs":          oldBefore.Indexes,
		"new_release_id":              newRelease.ReleaseId, "new_build_id": newBuild.BuildId,
		"new_build_generation": newBuild.Generation, "new_index_sha256": builtNew.IndexManifest.SHA256,
		"new_source_revision_id":        newSource.RevisionId,
		"new_lane_refs":                 newSnapshot.Indexes,
		"before_publication_revision":   oldBefore.PublicationRevision,
		"new_publication_revision":      newSnapshot.PublicationRevision,
		"rollback_publication_revision": oldAfter.PublicationRevision,
		"rollback_lane_refs":            oldAfter.Indexes,
		"old_before_search_id":          oldResult.SearchId, "new_search_id": newResult.SearchId,
		"old_after_search_id":  rollbackResult.SearchId,
		"old_before_answer_id": oldResult.AnswerId, "new_answer_id": newResult.AnswerId,
		"old_after_answer_id":      rollbackResult.AnswerId,
		"old_before_quote":         oldResult.Citations[0].Quote,
		"new_quote":                newResult.Citations[0].Quote,
		"old_after_quote":          rollbackResult.Citations[0].Quote,
		"new_answer_revision_only": true, "history_old_new_rollback_present": true,
		"real_usercenter_login_rpc": realUserCenter,
		"real_usercenter_uid":       strconv.FormatInt(ownerUID, 10),
		"subject_ref": map[string]string{"authority_id": "rtw.identity", "tenant_id": "platform",
			"subject_id": strconv.FormatInt(ownerUID, 10)},
		"other_subject_history_empty": true,
		"old_before_api_log":          oldAPI.LogEvidencePath,
		"new_api_log":                 newAPI.LogEvidencePath,
		"old_after_api_log":           rollbackAPI.LogEvidencePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidence, "h06-publish-search-rollback.json"), report, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("formal cmd/api signed BGE search before=%s new=%s rollback=%s, publication 1->2->3",
		oldResult.Citations[0].Quote, newResult.Citations[0].Quote, rollbackResult.Citations[0].Quote)
}
