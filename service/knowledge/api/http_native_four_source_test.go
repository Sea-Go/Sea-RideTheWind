package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

const realNativeQuery = "apple pie recipe"

var realNativeGuides = []struct {
	Name string
	Text string
}{
	{"apple", "Synthetic published passage about apple-guide; evidence is fixture only."},
	{"banana", "Synthetic published passage about banana-guide; evidence is fixture only."},
	{"orange", "Synthetic published passage about orange-guide; evidence is fixture only."},
	{"coffee", "Synthetic published passage about coffee-guide; evidence is fixture only."},
}

func nativeFourSourceMode(realUser bool) (bool, error) {
	selector := os.Getenv("SEA_RTW_NATIVE_FOUR_SOURCE")
	if selector == "" {
		return false, nil
	}
	if selector != "1" || realUser {
		return false, errors.New("Native four-Source test requires its sole explicit selector and the test User RPC")
	}
	btwProduct, btwFormal := os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT"),
		os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT")
	if btwProduct == "" || !filepath.IsAbs(btwProduct) || btwProduct != btwFormal {
		return false, errors.New("Native four-Source test requires one fixed BTW builder/formal API tree")
	}
	if info, err := os.Lstat(btwProduct); err != nil || !info.IsDir() {
		return false, errors.New("fixed BTW Native source tree unavailable")
	}
	if err := nativePinnedBTWSource(btwProduct, os.Getenv("SEA_BTW_NATIVE_SOURCE_SHA")); err != nil {
		return false, err
	}
	dcRuntime := os.Getenv("SEA_DC_BGE_RUNTIME")
	if !filepath.IsAbs(dcRuntime) {
		return false, errors.New("Native four-Source test requires fixed absolute DC BGE runtime")
	}
	if info, err := os.Lstat(dcRuntime); err != nil ||
		!info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false, errors.New("DC BGE runtime must be an ordinary private file")
	}
	for _, incompatible := range []string{"SEA_BGE_WORKER_PUBLISH_SEARCH",
		"SEA_BGE_WORKER_CANCEL_RELEASE", "SEA_BGE_WORKER_PROCESSES",
		"SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", "SEA_DC_EVENT_PLATFORM_ROOT",
		"SEA_BTW_SUMMARY_DC_RUNTIME_FILE"} {
		if os.Getenv(incompatible) != "" {
			return false, fmt.Errorf("Native four-Source test cannot share %s mode", incompatible)
		}
	}
	if os.Getenv("SEA_BTW_NATIVE_PUBLISHED_WITNESS_FIXTURE") != "" {
		return false, errors.New("Native witness fixture belongs only to the fixed BTW child")
	}
	if !filepath.IsAbs(os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")) {
		return false, errors.New("Native parent needs an absolute task-owned evidence directory for red/green outputs")
	}
	pythonPath := os.Getenv("SEA_RTW_NATIVE_PYTHON")
	if !filepath.IsAbs(pythonPath) {
		return false, errors.New("Native four-Source test requires one fixed absolute Python interpreter")
	}
	if info, err := os.Stat(pythonPath); err != nil ||
		!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return false, errors.New("pinned Native Python interpreter unavailable")
	}
	probe := exec.Command(pythonPath, "-c", "import importlib.metadata as m; print(m.version('milvus-lite'),m.version('faiss-cpu'))")
	probe.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	versions, err := probe.Output()
	if err != nil || strings.TrimSpace(string(versions)) != "3.2.1 1.15.0" {
		return false, errors.New("pinned Native Python dependencies differ from Lite3.2.1/FAISS1.15.0")
	}
	return true, nil
}

func nativePinnedBTWSource(root, expectedSHA string) error {
	if !filepath.IsAbs(root) || len(expectedSHA) != 40 ||
		strings.ToLower(expectedSHA) != expectedSHA {
		return errors.New("Native BTW source requires an absolute tree and fixed lowercase commit SHA")
	}
	if _, err := hex.DecodeString(expectedSHA); err != nil {
		return errors.New("Native BTW source SHA is not hexadecimal")
	}
	actual, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(actual)) != expectedSHA {
		return errors.New("Native BTW builder/formal source changed from fixed HEAD")
	}
	changes, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil || len(strings.TrimSpace(string(changes))) != 0 {
		return errors.New("Native BTW source tree contains uncommitted or untracked inputs")
	}
	return nil
}

type realNativeSource struct {
	Name     string
	Text     string
	Revision types.Revision
	Chunk    types.CitationChunk
}

func runRealNativeFourSourceHandoff(t *testing.T, store *model.Store,
	request httpRequest, dir, rtwBase, adminToken, workerToken, productToken string,
	relay *productSearchFixture, legacyModuleID, legacyReleaseID string) {
	t.Helper()
	btwRoot, dcRuntimePath := os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT"),
		os.Getenv("SEA_DC_BGE_RUNTIME")
	if btwRoot == "" || dcRuntimePath == "" || relay == nil || productToken == "" {
		t.Fatal("Native parent lacks its fixed RTW/DC/BTW product runtime")
	}
	if err := nativePinnedBTWSource(btwRoot, os.Getenv("SEA_BTW_NATIVE_SOURCE_SHA")); err != nil {
		t.Fatal(err)
	}
	var legacyReleaseBefore string
	if err := store.DB.QueryRow(context.Background(), `SELECT data->>'active_release_id'
 FROM knowledge_modules WHERE id=$1`, legacyModuleID).Scan(&legacyReleaseBefore); err != nil ||
		legacyReleaseBefore != legacyReleaseID {
		t.Fatal("legacy module's release pointer changed before Native test", err)
	}
	var module types.Module
	request("POST", "/v1/knowledge/modules", adminToken, types.CreateModuleReq{
		Title: "Native four approved Source passages", IdempotencyKey: "native-four-source-module"},
		&module, 200)
	if module.Id == "" || module.Id == legacyModuleID || module.ActiveReleaseId != "" {
		t.Fatalf("Native test did not create a distinct unpublished module: %+v", module)
	}
	sources := make([]realNativeSource, 0, 4)
	for _, guide := range realNativeGuides {
		var revision types.Revision
		request("POST", "/v1/knowledge/modules/"+module.Id+"/sources", adminToken,
			types.CreateSourceReq{Title: guide.Name + "-guide", Content: guide.Text,
				MediaType: "text/plain", Provenance: "synthetic fixed native corpus",
				IdempotencyKey: "native-four-source-" + guide.Name}, &revision, 200)
		if revision.RevisionId == "" || revision.EntityId == "" ||
			revision.Kind != "source" || revision.ModuleId != module.Id ||
			revision.ContentHash != object.Hash([]byte(guide.Text)) || revision.ObjectKey == "" {
			t.Fatalf("Native %s was not one authorized RTW SourceRevision: %+v", guide.Name, revision)
		}
		var original types.Revision
		request("GET", "/internal/v1/knowledge/revisions/"+revision.RevisionId,
			workerToken, nil, &original, 200)
		if original.Content != guide.Text || original.ObjectKey != revision.ObjectKey ||
			original.ContentHash != revision.ContentHash || original.Withdrawn {
			t.Fatalf("Native %s original Source bytes are not fixed: %+v", guide.Name, original)
		}
		sources = append(sources, realNativeSource{Name: guide.Name,
			Text: guide.Text, Revision: revision})
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Revision.RevisionId < sources[j].Revision.RevisionId
	})
	ids := make([]string, 0, 4)
	for _, source := range sources {
		ids = append(ids, source.Revision.RevisionId)
	}
	var release types.Release
	request("POST", "/v1/knowledge/modules/"+module.Id+"/releases", adminToken,
		types.CreateReleaseReq{SourceRevisionIds: ids, WikiRevisionIds: []string{},
			ChunkingProfile: "paragraph-v1", RetrievalProfiles: realBGEProfiles(t, dcRuntimePath),
			IdempotencyKey: "native-four-source-release"}, &release, 200)
	if release.ModuleId != module.Id || len(release.SourceRevisionIds) != 4 ||
		len(release.WikiRevisionIds) != 0 || len(release.RetrievalProfiles) != 3 ||
		!reflect.DeepEqual(release.SourceRevisionIds, ids) {
		t.Fatalf("Native Release changed four authorized Source identities: %+v", release)
	}
	var build types.Build
	request("POST", "/v1/knowledge/releases/"+release.ReleaseId+"/index-builds", adminToken,
		types.CreateBuildReq{IdempotencyKey: "native-four-source-build"}, &build, 200)
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/claim", workerToken,
		types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
			Generation: build.Generation, AttemptId: "native-four-source-attempt", LeaseEpoch: 1,
			ManifestHash: build.ManifestHash}, &build, 200)
	if build.ReleaseId != release.ReleaseId || build.ModuleId != module.Id ||
		build.State == "READY" || build.Generation < 1 || build.LeaseEpoch != 1 {
		t.Fatalf("Native build was not one claimed unpublished candidate: %+v", build)
	}
	var noSnapshot types.SearchSnapshot
	request("GET", "/internal/v1/knowledge/modules/"+module.Id+"/search-snapshot",
		workerToken, nil, &noSnapshot, 404)

	inputs := make([]any, 0, 4)
	chunks := make([]types.CitationChunk, 0, 4)
	for i := range sources {
		source := &sources[i]
		textSHA := object.Hash([]byte(source.Text))
		identity, err := json.Marshal([]any{source.Revision.RevisionId,
			release.ChunkingProfile, 1, 0, textSHA})
		if err != nil {
			t.Fatal(err)
		}
		encoding, err := json.Marshal([]any{release.ChunkingProfile, source.Text})
		if err != nil {
			t.Fatal(err)
		}
		source.Chunk = types.CitationChunk{ChunkId: object.Hash(identity),
			RevisionId: source.Revision.RevisionId, ContentId: source.Revision.EntityId,
			SourceKind: "source", Original: types.CitationObject{
				Key: source.Revision.ObjectKey, Sha256: source.Revision.ContentHash},
			Location: types.CitationLocation{Locator: "paragraph:1", OriginalByteStart: 0,
				OriginalByteEnd: len(source.Text), NormalizedRuneStart: 0,
				NormalizedRuneEnd: utf8.RuneCountInString(source.Text)},
			Text: source.Text, TextHash: textSHA,
			EncodingKey: object.Hash(encoding), Required: true}
		inputs = append(inputs, map[string]any{"revision_id": source.Revision.RevisionId,
			"content_id": source.Revision.EntityId, "source_kind": "source",
			"original": source.Chunk.Original, "chunk_count": 1})
		chunks = append(chunks, source.Chunk)
	}
	manifest := map[string]any{"schema_version": 1, "module_id": module.Id,
		"release_id": release.ReleaseId, "input_manifest_hash": release.ManifestHash,
		"profile": release.ChunkingProfile, "parser_version": "sea.paragraph.v1",
		"chunker_version": "trpc.fixed.v1.8.1", "chunk_size": 128,
		"overlap": 0, "inputs": inputs, "chunks": chunks}
	manifestKey, manifestSHA, manifestErr := store.Objects.Put(context.Background(),
		mustNativeJSON(t, manifest))
	manifestRef := model.ArtifactRef{Key: manifestKey, SHA256: manifestSHA}
	if manifestErr != nil || manifestRef.Key == "" || manifestRef.SHA256 == "" {
		t.Fatal("Native four-Source chunk manifest was not stored")
	}
	sources = readNativePinnedSourceChunks(t, store, manifestRef,
		module.Id, release.ReleaseId, sources)
	lite, litePath := startRealBTWNativeLite(t, dir, btwRoot)
	var apple, banana, orange realNativeSource
	for _, source := range sources {
		switch source.Name {
		case "apple":
			apple = source
		case "banana":
			banana = source
		case "orange":
			orange = source
		}
	}
	if apple.Chunk.ChunkId == "" || banana.Chunk.ChunkId == "" ||
		orange.Chunk.ChunkId == "" ||
		apple.Revision.RevisionId == orange.Revision.RevisionId {
		t.Fatal("four actual Source chunks did not pin independent Apple/Orange identities")
	}
	setup := &realIndexSetup{DCRuntime: dcRuntimePath, NativeRuntime: litePath,
		ArtifactDir: filepath.Join(dir, "objects"), ChunkManifest: manifestRef,
		BuildID: build.BuildId, ReleaseID: release.ReleaseId,
		Generation: build.Generation, ResultPath: filepath.Join(dir, "btw-native-index-result.json"),
		ExpectedQuote: apple.Text, ExpectedChunkID: apple.Chunk.ChunkId}
	builder := buildRealBTWIndexesOnly(t, dir, btwRoot, rtwBase, workerToken, module.Id, setup)
	if err := nativePinnedBTWSource(btwRoot, os.Getenv("SEA_BTW_NATIVE_SOURCE_SHA")); err != nil {
		t.Fatal(err)
	}
	resultInfo, err := os.Lstat(setup.ResultPath)
	if err != nil || !resultInfo.Mode().IsRegular() || resultInfo.Mode().Perm() != 0600 {
		t.Fatal("native builder omitted a private result before RTW READY")
	}
	resultRaw, err := os.ReadFile(setup.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	var built realIndexResult
	if json.Unmarshal(resultRaw, &built) != nil || built.ChunkCount != 4 ||
		len(built.Indexes) != 3 || len(built.APIIndexSettings) == 0 ||
		built.NativeProjection == nil || built.NativeProjection.PhysicalQualified ||
		built.NativeProjection.Status != "test_projection_before_RTW_READY" ||
		built.NativeProjection.Endpoint != lite.Endpoint ||
		built.NativeProjection.RuntimeSHA256 != lite.RawSHA256 ||
		built.NativeProjection.EnginePackageSHA256 != lite.EnginePackageSHA256 ||
		!validRealNativeSettings(built.NativeProjection.Settings) {
		t.Fatalf("BGE exact Ref/native ResumeIndex/Probe did not share four real chunks: %+v", built)
	}
	var physical struct {
		Namespace string `json:"namespace"`
	}
	if json.Unmarshal(built.NativeProjection.Settings, &physical) != nil ||
		physical.Namespace != "native_"+object.Hash([]byte(build.BuildId))[:12] {
		t.Fatal("native physical namespace is not fixed to this build")
	}
	indexRaw, err := store.Objects.Get(context.Background(),
		built.IndexManifest.Key, built.IndexManifest.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	var index model.IndexManifest
	if json.Unmarshal(indexRaw, &index) != nil || index.BuildID != build.BuildId ||
		index.ReleaseID != release.ReleaseId || index.Generation != build.Generation ||
		index.ChunkManifest != manifestRef || index.ChunkCount != 4 || len(index.Lanes) != 3 {
		t.Fatalf("actual Native three-lane manifest is not one RTW Build/Release: %+v", index)
	}
	for _, lane := range index.Lanes {
		if built.Indexes[lane.Profile.Lane] != lane.Artifact || !lane.ProbePassed {
			t.Fatalf("Hybrid lane changed signed exact Ref or skipped Probe: %+v", lane)
		}
	}
	var noReadySnapshot types.SearchSnapshot
	request("GET", "/internal/v1/knowledge/modules/"+module.Id+"/search-snapshot",
		workerToken, nil, &noReadySnapshot, 404)
	result := types.AcceptBuildReq{Generation: build.Generation, AttemptId: build.AttemptId,
		LeaseEpoch: build.LeaseEpoch, ManifestHash: build.ManifestHash, State: "READY",
		IndexManifestRef:  built.IndexManifest.Key,
		IndexManifestHash: built.IndexManifest.SHA256}
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/results",
		workerToken, result, &build, 200)
	if build.State != "READY" || build.IndexManifestHash != built.IndexManifest.SHA256 {
		t.Fatalf("RTW failed to accept this four-Source READY result: %+v", build)
	}
	request("GET", "/internal/v1/knowledge/modules/"+module.Id+"/search-snapshot",
		workerToken, nil, &noReadySnapshot, 404)
	var before types.ReleaseState
	request("GET", "/v1/knowledge/modules/"+module.Id+"/releases/current", adminToken,
		nil, &before, 200)
	if before.ActiveReleaseId != "" || before.BuildState != "READY" {
		t.Fatalf("Native READY auto-published a physical Release: %+v", before)
	}
	var activated types.ReleaseState
	request("PUT", "/v1/knowledge/modules/"+module.Id+"/activation", adminToken,
		types.ActivateReq{ReleaseId: release.ReleaseId, BuildId: build.BuildId,
			ExpectedPointerRevision: 0, Reason: "manual Native four-Source test publication"},
		&activated, 200)
	if activated.ActiveReleaseId != release.ReleaseId || activated.PointerRevision != 1 {
		t.Fatalf("four-Source Native Release was not manually published: %+v", activated)
	}
	var snapshot types.SearchSnapshot
	request("GET", "/internal/v1/knowledge/modules/"+module.Id+"/search-snapshot",
		workerToken, nil, &snapshot, 200)
	if snapshot.ModuleId != module.Id || snapshot.ReleaseId != release.ReleaseId ||
		snapshot.Generation != build.Generation || snapshot.PublicationRevision != "1" ||
		len(snapshot.Indexes) != 3 || len(snapshot.ValidRevisionIds) != 4 ||
		!reflect.DeepEqual(snapshot.ValidRevisionIds, ids) {
		t.Fatalf("published Native snapshot mixed in old Source/Wiki members: %+v", snapshot)
	}
	for _, lane := range index.Lanes {
		if snapshot.Indexes[lane.Profile.Lane].Key != lane.Artifact.Key ||
			snapshot.Indexes[lane.Profile.Lane].Sha256 != lane.Artifact.SHA256 {
			t.Fatalf("published Native %s ref differs from fixed READY", lane.Profile.Lane)
		}
	}
	witness, witnessRaw := runNativeIndependentWitness(t, dir, btwRoot, builder,
		rtwBase, workerToken, module.Id, dcRuntimePath, litePath, setup,
		orange.Revision.RevisionId, orange.Chunk.ChunkId)
	if err := nativePinnedBTWSource(btwRoot, os.Getenv("SEA_BTW_NATIVE_SOURCE_SHA")); err != nil {
		t.Fatal(err)
	}
	if witness.SchemaVersion != "sea.search.native-independent-discovery.v1" ||
		witness.ModuleID != module.Id || witness.ReleaseID != release.ReleaseId ||
		witness.Generation != build.Generation || witness.PublicationRevision != "1" ||
		witness.NativeRuntimeSHA256 != lite.RawSHA256 ||
		witness.EnginePackageSHA256 != lite.EnginePackageSHA256 ||
		witness.UniqueMultiChunkID != orange.Chunk.ChunkId ||
		witness.UniqueRevisionID != orange.Revision.RevisionId ||
		witness.UniqueQuoteSHA256 != orange.Chunk.TextHash ||
		witness.UniqueOriginalSHA256 != orange.Revision.ContentHash ||
		witness.UniqueLocator != "paragraph:1" ||
		!witness.RTWCurrentAndReadable || witness.PhysicalQualified ||
		witness.QrelEvaluable || witness.ProductionVerified ||
		!reflect.DeepEqual(witness.DenseChunkIDs,
			[]string{apple.Chunk.ChunkId, banana.Chunk.ChunkId}) ||
		!reflect.DeepEqual(witness.SparseChunkIDs,
			[]string{apple.Chunk.ChunkId}) ||
		!reflect.DeepEqual(witness.MultiVectorChunkIDs,
			[]string{apple.Chunk.ChunkId, orange.Chunk.ChunkId}) {
		t.Fatalf("Orange was not an independently readable Multi-only native hit: %+v", witness)
	}
	for _, id := range append(append([]string{}, witness.DenseChunkIDs...), witness.SparseChunkIDs...) {
		if id == orange.Chunk.ChunkId {
			t.Fatal("Orange also entered Dense/Sparse TopK2; no Multi-only evidence")
		}
	}
	formal := startRealBTWSearchAPIProcess(t, dir, btwRoot, rtwBase, workerToken,
		dcRuntimePath, built, apple.Text, litePath)
	relay.mu.Lock()
	relay.forwardURL = formal.URL
	relay.mu.Unlock()
	relay.stage.Store(4)
	searchPath := "/v1/knowledge/answer-sessions/search-facade-session/searches"
	searchBody := map[string]any{"module_id": module.Id, "query": realNativeQuery,
		"depth": "fast", "intelligence": "low",
		"idempotency_key": "native-four-source-product-search"}
	callsBefore := relay.calls.Load()
	var product types.ProductSearchResult
	request("POST", searchPath, productToken, searchBody, &product, 200)
	if product.Status != "succeeded" || product.SearchId == "" || product.AnswerId == "" ||
		product.Answer != "The published source states: "+apple.Text ||
		len(product.Citations) != 1 || product.CitationReceiptRef == "" ||
		product.Citations[0].RevisionId != apple.Revision.RevisionId ||
		product.Citations[0].ContentId != apple.Revision.EntityId ||
		product.Citations[0].Quote != apple.Text ||
		product.Citations[0].QuoteHash != apple.Chunk.TextHash ||
		relay.calls.Load() != callsBefore+1 {
		t.Fatalf("formal Native product did not use the active four-Source Release: %+v", product)
	}
	formal.AssertSignedSearch(t, product.SearchId, product.AnswerId)
	if err := nativePinnedBTWSource(btwRoot, os.Getenv("SEA_BTW_NATIVE_SOURCE_SHA")); err != nil {
		t.Fatal(err)
	}
	var replay types.ProductSearchResult
	request("POST", searchPath, productToken, searchBody, &replay, 200)
	if !reflect.DeepEqual(replay, product) || relay.calls.Load() != callsBefore+1 {
		t.Fatal("Native product replay changed an RTW accepted answer or recalled BTW")
	}
	request("GET", searchPath+"/"+product.SearchId, productToken, nil, &replay, 200)
	if !reflect.DeepEqual(replay, product) {
		t.Fatal("Native product operation GET differs from committed POST")
	}
	var durableCount, citationCount int
	if err := store.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_accepted_answers
 WHERE answer_id=$1 AND search_id=$2 AND session_id='search-facade-session'`,
		product.AnswerId, product.SearchId).Scan(&durableCount); err != nil || durableCount != 1 {
		t.Fatalf("Native model answer was not accepted in RTW PG: %d %v", durableCount, err)
	}
	if err := store.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_search_citations
 WHERE search_id=$1`, product.SearchId).Scan(&citationCount); err != nil || citationCount != 1 {
		t.Fatalf("Native cited evidence was not durably accepted: %d %v", citationCount, err)
	}
	var legacyReleaseAfter string
	if err := store.DB.QueryRow(context.Background(), `SELECT data->>'active_release_id'
 FROM knowledge_modules WHERE id=$1`, legacyModuleID).Scan(&legacyReleaseAfter); err != nil ||
		legacyReleaseAfter != legacyReleaseBefore {
		t.Fatal("new Native publication changed the legacy module pointer", err)
	}
	writeNativeParentEvidence(t, module, release, build, snapshot, lite, built,
		witness, witnessRaw, product, setup, manifestRef)
}

// The Source/Chunk identities handed to BTW come from the immutable object
// bytes the worker will actually read, not an old synthetic qrel document ID.
func readNativePinnedSourceChunks(t *testing.T, store *model.Store,
	ref model.ArtifactRef, moduleID, releaseID string,
	sources []realNativeSource) []realNativeSource {
	t.Helper()
	raw, err := store.Objects.Get(context.Background(), ref.Key, ref.SHA256)
	if err != nil || object.Hash(raw) != ref.SHA256 {
		t.Fatal("RTW could not independently reread four-Source manifest bytes", err)
	}
	var pinned struct {
		SchemaVersion int    `json:"schema_version"`
		ModuleID      string `json:"module_id"`
		ReleaseID     string `json:"release_id"`
		Inputs        []struct {
			RevisionID string               `json:"revision_id"`
			ContentID  string               `json:"content_id"`
			SourceKind string               `json:"source_kind"`
			Original   types.CitationObject `json:"original"`
			ChunkCount int                  `json:"chunk_count"`
		} `json:"inputs"`
		Chunks []types.CitationChunk `json:"chunks"`
	}
	if json.Unmarshal(raw, &pinned) != nil || pinned.SchemaVersion != 1 ||
		pinned.ModuleID != moduleID || pinned.ReleaseID != releaseID ||
		len(pinned.Inputs) != 4 || len(pinned.Chunks) != 4 || len(sources) != 4 {
		t.Fatalf("RTW fixed Source object is not exactly four Input/Chunk: %+v", pinned)
	}
	byRevision := make(map[string]int, 4)
	for i, source := range sources {
		if _, exists := byRevision[source.Revision.RevisionId]; exists {
			t.Fatal("four Source revisions contain a duplicate")
		}
		byRevision[source.Revision.RevisionId] = i
	}
	seenInput, seenChunk := make(map[string]bool, 4), make(map[string]bool, 4)
	for _, input := range pinned.Inputs {
		index, ok := byRevision[input.RevisionID]
		if !ok || seenInput[input.RevisionID] || input.ContentID != sources[index].Revision.EntityId ||
			input.SourceKind != "source" || input.Original != sources[index].Chunk.Original ||
			input.ChunkCount != 1 {
			t.Fatalf("persisted Native Input mixed an unauthorized Source: %+v", input)
		}
		seenInput[input.RevisionID] = true
	}
	for _, chunk := range pinned.Chunks {
		index, ok := byRevision[chunk.RevisionId]
		if !ok || seenChunk[chunk.RevisionId] ||
			!reflect.DeepEqual(chunk, sources[index].Chunk) ||
			chunk.Text != sources[index].Text ||
			chunk.Location.Locator != "paragraph:1" ||
			chunk.TextHash != object.Hash([]byte(sources[index].Text)) {
			t.Fatalf("persisted Native Chunk mixed original bytes or identity: %+v", chunk)
		}
		seenChunk[chunk.RevisionId] = true
		sources[index].Chunk = chunk
	}
	if len(seenInput) != 4 || len(seenChunk) != 4 {
		t.Fatal("persisted Native manifest did not cover each approved Source")
	}
	return sources
}

func mustNativeJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type realNativeUniqueResult struct {
	SchemaVersion         string   `json:"schema_version"`
	ModuleID              string   `json:"module_id"`
	ReleaseID             string   `json:"release_id"`
	Generation            int64    `json:"generation"`
	PublicationRevision   string   `json:"publication_revision"`
	Query                 string   `json:"query"`
	NativeRuntimeSHA256   string   `json:"native_runtime_sha256"`
	EnginePackageSHA256   string   `json:"engine_package_sha256"`
	DenseChunkIDs         []string `json:"dense_chunk_ids"`
	SparseChunkIDs        []string `json:"sparse_chunk_ids"`
	MultiVectorChunkIDs   []string `json:"multivector_chunk_ids"`
	UniqueMultiChunkID    string   `json:"unique_multi_chunk_id"`
	UniqueRevisionID      string   `json:"unique_revision_id"`
	UniqueQuoteSHA256     string   `json:"unique_quote_sha256"`
	UniqueOriginalSHA256  string   `json:"unique_original_sha256"`
	UniqueLocator         string   `json:"unique_locator"`
	RTWCurrentAndReadable bool     `json:"rtw_current_and_readable"`
	PhysicalQualified     bool     `json:"physical_qualified"`
	QrelEvaluable         bool     `json:"qrel_evaluable"`
	ProductionVerified    bool     `json:"production_verified"`
}

func runNativeIndependentWitness(t *testing.T, dir, btwRoot, builder,
	rtwBase, workerToken, moduleID, dcRuntimePath, litePath string,
	setup *realIndexSetup, orangeRevisionID, orangeChunkID string) (realNativeUniqueResult, []byte) {
	t.Helper()
	fixturePath := filepath.Join(dir, "btw-native-independent-fixture.json")
	resultPath := filepath.Join(dir, "btw-native-independent-result.json")
	fixture := struct {
		RTWBase                  string `json:"rtw_base"`
		WorkerToken              string `json:"worker_token"`
		ModuleID                 string `json:"module_id"`
		DCRuntime                string `json:"dc_runtime"`
		NativeRuntime            string `json:"native_runtime"`
		ArtifactDir              string `json:"artifact_dir"`
		BuildResultPath          string `json:"build_result_path"`
		Query                    string `json:"query"`
		ExpectedOrangeRevisionID string `json:"expected_orange_revision_id"`
		ExpectedOrangeChunkID    string `json:"expected_orange_chunk_id"`
		ResultPath               string `json:"result_path"`
	}{rtwBase, workerToken, moduleID, dcRuntimePath, litePath, setup.ArtifactDir,
		setup.ResultPath, realNativeQuery, orangeRevisionID, orangeChunkID, resultPath}
	if err := writeNativeOnce(fixturePath, mustNativeJSON(t, fixture)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, builder,
		"-test.run=^TestRTWNativePublishedIndependentMultiDiscovery$", "-test.v")
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(), "SEA_BTW_NATIVE_PUBLISHED_WITNESS_FIXTURE="+fixturePath)
	output, childErr := cmd.CombinedOutput()
	if err := writeNativeOnce(filepath.Join(dir, "btw-native-independent-child.log"), output); err != nil {
		t.Fatal(err)
	}
	persistNativeTestBytes(t, "btw-native-independent-child.log", output)
	if childErr != nil {
		t.Fatalf("BTW Native published witness failed: %v; private log retained", childErr)
	}
	info, err := os.Lstat(resultPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("BTW Native witness did not write one private result")
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result realNativeUniqueResult
	if json.Unmarshal(raw, &result) != nil || result.Query != realNativeQuery {
		t.Fatalf("Native physical witness returned another query or invalid bytes: %+v", result)
	}
	return result, raw
}

func writeNativeParentEvidence(t *testing.T, module types.Module,
	release types.Release, build types.Build, snapshot types.SearchSnapshot,
	lite realNativeLiteRuntime, built realIndexResult,
	witness realNativeUniqueResult, witnessRaw []byte,
	product types.ProductSearchResult, setup *realIndexSetup,
	chunkManifest model.ArtifactRef) {
	t.Helper()
	if product.SearchId == "" || product.AnswerId == "" ||
		witness.UniqueMultiChunkID == "" || built.NativeProjection == nil {
		t.Fatal("Native parent evidence requires accepted product and independent discovery")
	}
	base := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")
	if base == "" {
		return
	}
	if !filepath.IsAbs(base) {
		t.Fatal("Native parent evidence directory must be absolute")
	}
	var report = struct {
		SchemaVersion       string   `json:"schema_version"`
		ModuleID            string   `json:"module_id"`
		ReleaseID           string   `json:"release_id"`
		BuildID             string   `json:"build_id"`
		Generation          int64    `json:"generation"`
		PublicationRevision string   `json:"publication_revision"`
		SourceRevisionIDs   []string `json:"source_revision_ids"`
		ChunkManifestSHA256 string   `json:"chunk_manifest_sha256"`
		IndexManifestSHA256 string   `json:"index_manifest_sha256"`
		NativeRuntimeSHA256 string   `json:"native_runtime_sha256"`
		EnginePackageSHA256 string   `json:"engine_package_sha256"`
		WitnessSHA256       string   `json:"witness_sha256"`
		Query               string   `json:"query"`
		SearchID            string   `json:"search_id"`
		AnswerID            string   `json:"answer_id"`
		PhysicalQualified   bool     `json:"physical_qualified"`
		QrelEvaluable       bool     `json:"qrel_evaluable"`
		ProductionVerified  bool     `json:"production_verified"`
	}{SchemaVersion: "sea.rtw.native-four-source-parent.v1", ModuleID: module.Id,
		ReleaseID: release.ReleaseId, BuildID: build.BuildId,
		Generation: build.Generation, PublicationRevision: snapshot.PublicationRevision,
		SourceRevisionIDs:   release.SourceRevisionIds,
		ChunkManifestSHA256: chunkManifest.SHA256,
		IndexManifestSHA256: built.IndexManifest.SHA256,
		NativeRuntimeSHA256: lite.RawSHA256,
		EnginePackageSHA256: lite.EnginePackageSHA256,
		WitnessSHA256:       object.Hash(witnessRaw), Query: realNativeQuery,
		SearchID: product.SearchId, AnswerID: product.AnswerId}
	persistNativeTestBytes(t, "native-four-source-parent.json",
		append(mustNativeJSON(t, report), '\n'))
	persistNativeTestBytes(t, "native-independent-result.json", witnessRaw)
	if raw, err := os.ReadFile(setup.ResultPath); err == nil {
		persistNativeTestBytes(t, "native-index-result.json", raw)
	} else {
		t.Fatal(err)
	}
}
