package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

type citationFixture struct {
	module  types.Module
	source  types.Revision
	release types.Release
	build   types.Build
	chunk   types.CitationChunk
	index   model.IndexManifest
}

func makeCitationFixture(t *testing.T, store *model.Store) citationFixture {
	t.Helper()
	m := createModule(t, store, "citation fixture")
	source := source(t, store, m, "Citation book")
	loaded, err := store.GetRevision(ctx, source.RevisionId)
	source = must(t, loaded, err)
	release := release(t, store, m, []string{source.RevisionId}, nil, "citation-release")
	build := build(t, store, release, "citation-build")
	claimed, err := store.ClaimBuild(ctx, types.ClaimBuildReq{BuildId: build.BuildId,
		Generation: build.Generation, AttemptId: "citation-attempt", LeaseEpoch: 1,
		ManifestHash: build.ManifestHash, LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano)})
	claimed = must(t, claimed, err)
	quote := "First paragraph."
	start := strings.Index(source.Content, quote)
	if start < 0 {
		t.Fatal("fixture source lacks quote")
	}
	quoteHash := object.Hash([]byte(quote))
	identity, err := json.Marshal([]any{source.RevisionId, release.ChunkingProfile, 2, 0, quoteHash})
	if err != nil {
		t.Fatal(err)
	}
	chunk := types.CitationChunk{ChunkId: object.Hash(identity), RevisionId: source.RevisionId,
		ContentId: source.EntityId, SourceKind: source.Kind, Original: types.CitationObject{Key: source.ObjectKey, Sha256: source.ContentHash},
		Location: types.CitationLocation{Locator: "paragraph:2", OriginalByteStart: start,
			OriginalByteEnd: start + len(quote), NormalizedRuneStart: 0, NormalizedRuneEnd: len([]rune(quote))},
		Text: quote, TextHash: quoteHash, EncodingKey: object.Hash([]byte(quote)), Required: true}
	manifest := struct {
		SchemaVersion     int                   `json:"schema_version"`
		ModuleID          string                `json:"module_id"`
		ReleaseID         string                `json:"release_id"`
		InputManifestHash string                `json:"input_manifest_hash"`
		Profile           string                `json:"profile"`
		ParserVersion     string                `json:"parser_version"`
		ChunkerVersion    string                `json:"chunker_version"`
		ChunkSize         int                   `json:"chunk_size"`
		Overlap           int                   `json:"overlap"`
		Inputs            []any                 `json:"inputs"`
		Chunks            []types.CitationChunk `json:"chunks"`
	}{1, m.Id, release.ReleaseId, release.ManifestHash, release.ChunkingProfile,
		"sea.paragraph.v1", "trpc.fixed.v1.8.1", 32, 0,
		[]any{map[string]any{"revision_id": source.RevisionId, "content_id": source.EntityId,
			"source_kind": source.Kind, "original": chunk.Original, "chunk_count": 1}}, []types.CitationChunk{chunk}}
	index := testenv.Index(t, store, claimed, release)
	index.ChunkManifest = testenv.Put(t, store, manifest)
	ref := testenv.Put(t, store, index)
	ready, err := store.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: claimed.BuildId, Generation: claimed.Generation,
		AttemptId: claimed.AttemptId, LeaseEpoch: claimed.LeaseEpoch, ManifestHash: claimed.ManifestHash,
		State: "READY", IndexManifestRef: ref.Key, IndexManifestHash: ref.SHA256})
	ready = must(t, ready, err)
	activate(t, store, m, release, ready, 0)
	return citationFixture{m, source, release, ready, chunk, index}
}

type testCitationKey struct {
	SourceKind string `json:"source_kind"`
	ContentID  string `json:"content_id"`
	RevisionID string `json:"revision_id"`
	ChunkID    string `json:"chunk_id"`
}

func makeCitationRequest(t *testing.T, f citationFixture, searchID string) types.AcceptSearchCitationsReq {
	t.Helper()
	key := testCitationKey{f.chunk.SourceKind, f.chunk.ContentId, f.chunk.RevisionId, f.chunk.ChunkId}
	idRaw, err := json.Marshal(struct {
		SearchID string
		Key      testCitationKey
		Hash     string
	}{searchID, key, f.chunk.TextHash})
	if err != nil {
		t.Fatal(err)
	}
	indexes := map[string]model.ArtifactRef{}
	for _, lane := range f.index.Lanes {
		indexes[lane.Profile.Lane] = lane.Artifact
	}
	pack := struct {
		SearchID string `json:"search_id"`
		Snapshot struct {
			ModuleID            string                       `json:"module_id"`
			ReleaseID           string                       `json:"release_id"`
			Generation          int64                        `json:"generation"`
			PublicationRevision string                       `json:"publication_revision"`
			Indexes             map[string]model.ArtifactRef `json:"indexes"`
			ValidRevisionIDs    []string                     `json:"valid_revision_ids"`
		} `json:"snapshot"`
		Profile        map[string]any `json:"profile"`
		Status         string         `json:"status"`
		StopReason     string         `json:"stop_reason"`
		CoverageStatus string         `json:"coverage_status"`
		Gaps           []string       `json:"gaps"`
		Evidence       []any          `json:"evidence"`
	}{SearchID: searchID, Profile: map[string]any{}, Status: "complete",
		CoverageStatus: "covered", Gaps: []string{}, Evidence: []any{map[string]any{
			"evidence_id": "ev_" + object.Hash(idRaw)[:24], "key": key, "locator": f.chunk.Location,
			"original": f.chunk.Original, "quote": f.chunk.Text, "quote_hash": f.chunk.TextHash,
			"relevance": 0.03, "sources": []any{},
		}}}
	pack.Snapshot.ModuleID = f.module.Id
	pack.Snapshot.ReleaseID = f.release.ReleaseId
	pack.Snapshot.Generation = f.build.Generation
	pack.Snapshot.PublicationRevision = "1"
	pack.Snapshot.Indexes = indexes
	pack.Snapshot.ValidRevisionIDs = []string{f.source.RevisionId}
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	return types.AcceptSearchCitationsReq{SearchId: searchID, PackJson: string(raw), PackHash: object.Hash(raw)}
}

func TestSearchCitationOriginalAndDurableReplay(t *testing.T) {
	store := testenv.Store(t)
	f := makeCitationFixture(t, store)
	read := types.ReadSearchSourceReq{ModuleId: f.module.Id, ReleaseId: f.release.ReleaseId,
		Generation: f.build.Generation, PublicationRevision: "1",
		RevisionId: f.source.RevisionId, ChunkId: f.chunk.ChunkId}
	chunk, err := store.ReadSearchSource(ctx, read)
	chunk = must(t, chunk, err)
	if chunk != f.chunk {
		t.Fatalf("source chunk changed: %+v", chunk)
	}
	for name, mutate := range map[string]func(*types.ReadSearchSourceReq){
		"wrong generation":  func(r *types.ReadSearchSourceReq) { r.Generation++ },
		"wrong publication": func(r *types.ReadSearchSourceReq) { r.PublicationRevision = "2" },
		"wrong revision":    func(r *types.ReadSearchSourceReq) { r.RevisionId = "rev_other" },
		"wrong chunk":       func(r *types.ReadSearchSourceReq) { r.ChunkId = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := read
			mutate(&bad)
			if _, err := store.ReadSearchSource(ctx, bad); err == nil {
				t.Fatal("mismatched fixed source accepted")
			}
		})
	}
	req := makeCitationRequest(t, f, "search-citation-1")
	receipt, err := store.AcceptSearchCitations(ctx, req)
	receipt = must(t, receipt, err)
	if receipt.SearchId != req.SearchId || receipt.PackHash != req.PackHash || receipt.DurableRef == "" {
		t.Fatalf("invalid durable receipt: %+v", receipt)
	}
	var storedHash, storedPack string
	if err = store.DB.QueryRow(ctx, "SELECT pack_hash,pack_json FROM knowledge_search_citations WHERE search_id=$1", req.SearchId).
		Scan(&storedHash, &storedPack); err != nil || storedHash != req.PackHash || storedPack != req.PackJson {
		t.Fatalf("receipt preceded durable pack: %s %v", storedHash, err)
	}
	again, err := store.AcceptSearchCitations(ctx, req)
	again = must(t, again, err)
	if again != receipt {
		t.Fatalf("idempotent replay changed receipt: %+v %+v", receipt, again)
	}
	record, err := store.GetSearchCitations(ctx, req.SearchId)
	record = must(t, record, err)
	if record.DurableRef != receipt.DurableRef || len(record.Evidence) != 1 ||
		record.Evidence[0].State != "available" || record.Evidence[0].QuoteHash != f.chunk.TextHash {
		t.Fatalf("citation lookup differs from committed pack: %+v", record)
	}
	bad := req
	bad.PackJson = strings.Replace(req.PackJson, "First paragraph.", "Fake paragraph.", 1)
	bad.PackHash = object.Hash([]byte(bad.PackJson))
	if _, err = store.AcceptSearchCitations(ctx, bad); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same search ID with changed pack: %v", err)
	}
	_, err = store.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: f.module.Id,
		TargetKind: "revision", TargetId: f.source.RevisionId, Reason: "test withdrawal", IdempotencyKey: "citation-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReadSearchSource(ctx, read); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("withdrawn source was readable: %v", err)
	}
	again, err = store.AcceptSearchCitations(ctx, req)
	again = must(t, again, err)
	if again != receipt {
		t.Fatal("withdrawal destroyed cancellation recovery receipt")
	}
	record, err = store.GetSearchCitations(ctx, req.SearchId)
	record = must(t, record, err)
	if len(record.Evidence) != 1 || record.Evidence[0].State != "unavailable" {
		t.Fatalf("history silently supplied withdrawn citation: %+v", record)
	}
}

func TestSearchCitationRejectsForgedQuoteBeforeInsert(t *testing.T) {
	store := testenv.Store(t)
	f := makeCitationFixture(t, store)
	req := makeCitationRequest(t, f, "search-forgery")
	for _, scenario := range [][3]string{
		{"quote", "First paragraph.", "Other paragraph."},
		{"locator", "paragraph:2", "paragraph:3"},
		{"generation", fmt.Sprintf(`"generation":%d`, f.build.Generation), `"generation":999`},
		{"index", f.index.Lanes[0].Artifact.SHA256, strings.Repeat("a", 64)},
	} {
		name, old, next := scenario[0], scenario[1], scenario[2]
		t.Run(name, func(t *testing.T) {
			bad := req
			bad.PackJson = strings.Replace(bad.PackJson, old, next, 1)
			bad.PackHash = object.Hash([]byte(bad.PackJson))
			if _, err := store.AcceptSearchCitations(ctx, bad); err == nil {
				t.Fatal("forged evidence accepted")
			}
			var count int
			if err := store.DB.QueryRow(context.Background(), "SELECT COUNT(*) FROM knowledge_search_citations WHERE search_id=$1", req.SearchId).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected pack was stored: %d %v", count, err)
			}
		})
	}
}

func TestSearchCitationConcurrentCommitHasOneImmutableWinner(t *testing.T) {
	store := testenv.Store(t)
	f := makeCitationFixture(t, store)
	first := makeCitationRequest(t, f, "search-race")
	second := first
	second.PackJson = strings.Replace(first.PackJson, `"coverage_status":"covered"`, `"coverage_status":"partial"`, 1)
	if second.PackJson == first.PackJson {
		t.Fatal("concurrent fixture did not change the pack")
	}
	second.PackHash = object.Hash([]byte(second.PackJson))
	requests := []types.AcceptSearchCitationsReq{first, second}
	var receipts [2]types.SearchCitationReceipt
	var failures [2]error
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipts[i], failures[i] = store.AcceptSearchCitations(ctx, requests[i])
		}(i)
	}
	wg.Wait()
	succeeded, conflicted := 0, 0
	for i := range requests {
		if failures[i] == nil {
			succeeded++
			if receipts[i].PackHash != requests[i].PackHash {
				t.Fatalf("winner receipt mismatch: %+v", receipts[i])
			}
		} else if errors.Is(failures[i], model.ErrConflict) {
			conflicted++
		} else {
			t.Fatalf("unexpected concurrent outcome: %v", failures[i])
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("outcomes: success=%d conflict=%d", succeeded, conflicted)
	}
	var count int
	if err := store.DB.QueryRow(ctx, "SELECT COUNT(*) FROM knowledge_search_citations WHERE search_id=$1", first.SearchId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent writes left %d rows: %v", count, err)
	}
}

func TestSearchCitationWithdrawalDuringSourceReadCannotCommit(t *testing.T) {
	store := testenv.Store(t)
	f := makeCitationFixture(t, store)
	req := makeCitationRequest(t, f, "search-withdraw-during-read")
	original := store.Objects
	store.Objects = &interceptRead{Store: original, key: f.source.ObjectKey, action: func() {
		_, err := store.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: f.module.Id,
			TargetKind: "revision", TargetId: f.source.RevisionId,
			Reason: "withdraw during search source read", IdempotencyKey: "search-withdraw-interleaved"})
		if err != nil {
			t.Error(err)
		}
	}}
	_, err := store.AcceptSearchCitations(ctx, req)
	if !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("citation survived withdrawal during object I/O: %v", err)
	}
	var count int
	if err = store.DB.QueryRow(ctx, "SELECT COUNT(*) FROM knowledge_search_citations WHERE search_id=$1", req.SearchId).Scan(&count); err != nil || count != 0 {
		t.Fatalf("withdrawn citation committed: %d %v", count, err)
	}
}
