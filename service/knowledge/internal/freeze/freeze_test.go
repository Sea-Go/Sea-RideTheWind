package freeze

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/knowledge/internal/citation"
	"sea-try-go/service/knowledge/internal/release"
	"sea-try-go/service/knowledge/internal/structure"
)

func mkFrozen(module, revID, docKey, source string) FrozenRevision {
	return FrozenRevision{
		RevisionID:    revID,
		ModuleID:      module,
		DocKey:        docKey,
		Source:        []byte(source),
		ContentSHA256: sha256Hex([]byte(source)),
	}
}

func docA() FrozenRevision {
	return mkFrozen("mod-1", "rev-a",
		"source:11111111-2222-3333-4444-555555555555@v1",
		"# Book A\n\nEvidence paragraph one.\n\n## Chapter\n\nEvidence paragraph two.\n")
}

func docB() FrozenRevision {
	return mkFrozen("mod-1", "rev-b",
		"page:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@v2",
		"# 页面 B\n\n第一段：整篇不切割。\n\n## 背景\n\n背景段落，含中文与 \"引号\"。\n")
}

func docC() FrozenRevision {
	return mkFrozen("mod-1", "rev-c",
		"summary:00000000-1111-2222-3333-444444444444@v3",
		"# Summary C\n\nSummary body.\n")
}

func expectedTreeSHA(t *testing.T, fr FrozenRevision) string {
	t.Helper()
	tree, err := structure.Derive(fr.RevisionID, fr.Source)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return sha256Hex(canonicalTreeJSON(tree))
}

func TestFreezeRejectsSHAMismatch(t *testing.T) {
	svc := NewMemoryService()
	fr := docA()
	fr.ContentSHA256 = strings.Repeat("0", 64)
	if _, err := svc.Freeze(context.Background(), fr); err == nil {
		t.Fatal("expected sha mismatch to be rejected")
	} else if !strings.Contains(err.Error(), "does not match source") {
		t.Fatalf("unexpected error: %v", err)
	}

	// A correct sha in non-canonical form is rejected before hashing.
	fr.ContentSHA256 = strings.ToUpper(sha256Hex(fr.Source))
	if _, err := svc.Freeze(context.Background(), fr); err == nil || !strings.Contains(err.Error(), "lowercase hex") {
		t.Fatalf("expected lowercase-hex rejection, got %v", err)
	}

	// Nothing was persisted.
	if _, _, err := svc.structures.LoadTree(context.Background(), fr.RevisionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no tree stored, got %v", err)
	}
}

func TestFreezeIdempotentReplay(t *testing.T) {
	svc := NewMemoryService()
	ctx := context.Background()
	fr := docA()

	sha1, err := svc.Freeze(ctx, fr)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if sha1 != expectedTreeSHA(t, fr) {
		t.Fatalf("tree sha %s != expected %s", sha1, expectedTreeSHA(t, fr))
	}
	sha2, err := svc.Freeze(ctx, fr)
	if err != nil {
		t.Fatalf("replay freeze: %v", err)
	}
	if sha2 != sha1 {
		t.Fatalf("replay changed tree sha: %s vs %s", sha2, sha1)
	}
	if got := len(svc.structures.(*MemoryStructureStore).trees); got != 1 {
		t.Fatalf("replay stored %d trees, want 1", got)
	}
	// The revision survives a reload (the replay ensured it).
	loaded, err := svc.revisions.Load(ctx, fr.RevisionID)
	if err != nil || !sameRevision(loaded, fr) {
		t.Fatalf("revision not preserved: %v %+v", err, loaded)
	}

	// A divergent source under the same revision id is a conflict.
	divergent := mkFrozen("mod-1", fr.RevisionID, fr.DocKey, "# Book A\n\nDifferent bytes entirely.\n")
	if _, err := svc.Freeze(ctx, divergent); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict on divergent replay, got %v", err)
	}
}

func TestFreezeReplayRejectsEnvelopeDrift(t *testing.T) {
	svc := NewMemoryService()
	ctx := context.Background()
	fr := docA()
	if _, err := svc.Freeze(ctx, fr); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	// Same revID + same source derive the identical treeSHA, so the tree
	// check alone cannot see the drift — replaying with a different DocKey
	// or ModuleID must hit the revision immutability check instead.
	drift := fr
	drift.DocKey = "page:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@v2"
	if _, err := svc.Freeze(ctx, drift); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for drifted doc key, got %v", err)
	}
	drift = fr
	drift.ModuleID = "mod-2"
	if _, err := svc.Freeze(ctx, drift); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for drifted module id, got %v", err)
	}
	// The stored record keeps the original envelope.
	loaded, err := svc.revisions.Load(ctx, fr.RevisionID)
	if err != nil || !sameRevision(loaded, fr) {
		t.Fatalf("stored revision drifted: %v %+v", err, loaded)
	}
}

func TestReleaseDeterministicEventID(t *testing.T) {
	ctx := context.Background()
	docs := []FrozenRevision{docA(), docB(), docC()}
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	ob1 := release.NewMemoryStore()
	id1, err := NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), ob1).Release(ctx, "mod-1", "rel-1", docs, at)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !isLowerHex64(id1) {
		t.Fatalf("event id %q is not 64 lowercase hex", id1)
	}

	// Same inputs on a fresh service (no shared state) → same id.
	ob2 := release.NewMemoryStore()
	id2, err := NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), ob2).Release(ctx, "mod-1", "rel-1", docs, at)
	if err != nil {
		t.Fatalf("second release: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("same inputs produced different event ids: %s vs %s", id1, id2)
	}

	// The same instant in a different zone is the same canonical fact.
	id3, err := NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), release.NewMemoryStore()).
		Release(ctx, "mod-1", "rel-1", docs, time.Date(2026, 10, 6, 20, 0, 0, 0, time.FixedZone("+08", 8*3600)))
	if err != nil {
		t.Fatalf("zoned release: %v", err)
	}
	if id3 != id1 {
		t.Fatalf("same instant in +08 produced a different event id: %s vs %s", id3, id1)
	}

	// Different release ids and different doc order are different facts.
	id4, err := NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), release.NewMemoryStore()).
		Release(ctx, "mod-1", "rel-2", docs, at)
	if err != nil || id4 == id1 {
		t.Fatalf("different release_id must change the event id: %v %s", err, id4)
	}
	id5, err := NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), release.NewMemoryStore()).
		Release(ctx, "mod-1", "rel-1", []FrozenRevision{docs[1], docs[0], docs[2]}, at)
	if err != nil || id5 == id1 {
		t.Fatalf("doc order is part of the published fact: %v %s", err, id5)
	}

	pending, err := ob1.Pending()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("outbox holds %d events, want 1", len(pending))
	}
	e := pending[0]
	if e.EventID != id1 || e.ModuleID != "mod-1" || e.ReleaseID != "rel-1" {
		t.Fatalf("event envelope mismatch: %+v", e)
	}
	if !e.PublishedAt.Equal(at) {
		t.Fatalf("published_at %v != %v", e.PublishedAt, at)
	}
	for i, d := range docs {
		got := e.Docs[i]
		if got.DocKey != d.DocKey || got.RevisionID != d.RevisionID {
			t.Fatalf("docs[%d] identity mismatch: %+v", i, got)
		}
		if got.StructureRef != expectedTreeSHA(t, d) {
			t.Fatalf("docs[%d] structure_ref %s != tree sha %s", i, got.StructureRef, expectedTreeSHA(t, d))
		}
		if want := S3RefPrefix + d.ContentSHA256; got.S3Ref != want {
			t.Fatalf("docs[%d] s3_ref %s != placeholder %s", i, got.S3Ref, want)
		}
		if got.ContentSHA256 != d.ContentSHA256 || got.CharLen != len(d.Source) {
			t.Fatalf("docs[%d] fingerprint mismatch: %+v", i, got)
		}
	}
}

func TestReleaseReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	docs := []FrozenRevision{docA(), docB(), docC()}
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	svc := NewMemoryService()

	id1, err := svc.Release(ctx, "mod-1", "rel-1", docs, at)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	id2, err := svc.Release(ctx, "mod-1", "rel-1", docs, at)
	if err != nil {
		t.Fatalf("replay release: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("replay produced a new event id: %s vs %s", id2, id1)
	}
	pending, err := svc.outbox.Pending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("replay must not duplicate the outbox entry: %v %d", err, len(pending))
	}
}

func TestReleaseBadInputs(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	svc := NewMemoryService()

	cases := []struct {
		name      string
		moduleID  string
		releaseID string
		docs      []FrozenRevision
		at        time.Time
		wantSub   string
	}{
		{"no docs", "mod-1", "rel-1", nil, at, "at least one doc"},
		{"zero time", "mod-1", "rel-1", []FrozenRevision{docA()}, time.Time{}, "published_at"},
		{"module mismatch", "mod-1", "rel-1", []FrozenRevision{docA(), mkFrozen("mod-2", "rev-x", "source:11111111-2222-3333-4444-555555555555@v9", "x")}, at, "does not match release module"},
		{"bad doc key", "mod-1", "rel-1", []FrozenRevision{mkFrozen("mod-1", "rev-x", "not-a-doc-key", "x")}, at, "doc key"},
		{"bad sha", "mod-1", "rel-1", []FrozenRevision{{RevisionID: "rev-x", ModuleID: "mod-1", DocKey: "source:11111111-2222-3333-4444-555555555555@v9", Source: []byte("x"), ContentSHA256: strings.Repeat("a", 63)}}, at, "lowercase hex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Release(ctx, tc.moduleID, tc.releaseID, tc.docs, tc.at)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestReleaseThenAcceptRoundtrip(t *testing.T) {
	ctx := context.Background()
	docs := []FrozenRevision{docA(), docB(), docC()}
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	svc := NewMemoryService()

	if _, err := svc.Release(ctx, "mod-1", "rel-1", docs, at); err != nil {
		t.Fatalf("release: %v", err)
	}

	// Pick a real quote from a paragraph of the frozen Chinese doc.
	target := docs[1]
	tree, err := structure.Derive(target.RevisionID, target.Source)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	var para *structure.Node
	for i := range tree.Nodes {
		n := tree.Nodes[i]
		if n.Level == structure.LevelParagraph && strings.Contains(string(target.Source[n.CharStart:n.CharEnd]), "背景段落") {
			para = &tree.Nodes[i]
			break
		}
	}
	if para == nil {
		t.Fatal("no paragraph found for the quote")
	}
	quote := "背景段落，含中文与 \"引号\"。"

	req := citation.AcceptRequest{
		AnswerID: "ans-1",
		SearchID: "search-1",
		Citations: []citation.Citation{
			{DocKey: target.DocKey, RevisionID: target.RevisionID,
				Locator: structure.Locator{SectionPath: []string{"背景"}, ParaIndex: para.ParaIndex, Quote: quote}},
			// Same evidence twice → merged, not an error.
			{DocKey: target.DocKey, RevisionID: target.RevisionID,
				Locator: structure.Locator{ParaIndex: para.ParaIndex, Quote: quote}},
		},
	}
	receipt, err := svc.Accept(ctx, req, target.RevisionID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if receipt.AnswerID != "ans-1" || receipt.SearchID != "search-1" {
		t.Fatalf("receipt identity mismatch: %+v", receipt)
	}
	if len(receipt.Verified) != 1 {
		t.Fatalf("expected the duplicate citation to be merged, got %d verified", len(receipt.Verified))
	}
	v := receipt.Verified[0]
	if v.Anchored.NodeID != para.NodeID {
		t.Fatalf("anchored node %s != expected %s", v.Anchored.NodeID, para.NodeID)
	}
	if got := string(target.Source[v.Anchored.CharStart:v.Anchored.CharEnd]); got != quote {
		t.Fatalf("anchored span bytes %q != quote %q", got, quote)
	}
	if receipt.TreeSHA != expectedTreeSHA(t, target) {
		t.Fatalf("receipt tree sha %s != frozen tree sha %s (canonical encoder drift)", receipt.TreeSHA, expectedTreeSHA(t, target))
	}
}

func TestAcceptRejectsForeignDocKey(t *testing.T) {
	ctx := context.Background()
	svc := NewMemoryService()
	fr := docA()
	if _, err := svc.Freeze(ctx, fr); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	// The locator is valid for the frozen revision, but the citation names
	// another document: a receipt must not anchor this revision's evidence
	// under a foreign DocKey.
	req := citation.AcceptRequest{
		AnswerID: "ans-x",
		SearchID: "search-x",
		Citations: []citation.Citation{{
			DocKey:     "page:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@v2",
			RevisionID: fr.RevisionID,
			Locator:    structure.Locator{ParaIndex: 0, Quote: "Evidence paragraph one."},
		}},
	}
	_, err := svc.Accept(ctx, req, fr.RevisionID)
	if err == nil || !strings.Contains(err.Error(), "does not match frozen document") {
		t.Fatalf("expected doc key mismatch rejection, got %v", err)
	}
}

func TestAcceptUnknownRevision(t *testing.T) {
	svc := NewMemoryService()
	_, err := svc.Accept(context.Background(), citation.AcceptRequest{AnswerID: "a", SearchID: "s",
		Citations: []citation.Citation{{DocKey: "source:11111111-2222-3333-4444-555555555555@v1", RevisionID: "rev-missing",
			Locator: structure.Locator{ParaIndex: 0, Quote: "x"}}}}, "rev-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestConcurrentFreeze(t *testing.T) {
	ctx := context.Background()
	svc := NewMemoryService()
	base := docA()

	var wg sync.WaitGroup
	shas := make([]string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			shas[i], err = svc.Freeze(ctx, base)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}(i)
	}
	// Concurrent freezes of distinct revisions race along the same stores.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fr := mkFrozen("mod-1", fmt.Sprintf("rev-race-%d", i), "page:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@v1",
				strings.Repeat("paragraph.\n\n", i+1))
			if _, err := svc.Freeze(ctx, fr); err != nil {
				t.Errorf("distinct goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	for i, s := range shas {
		if s != shas[0] {
			t.Fatalf("goroutine %d saw tree sha %s, first saw %s", i, s, shas[0])
		}
	}
	if got := len(svc.structures.(*MemoryStructureStore).trees); got != 9 {
		t.Fatalf("store holds %d trees, want 9", got)
	}
}

func TestCanonicalRoundtripViaFreeze(t *testing.T) {
	// The nasty document: quotes, backslashes, tabs, control bytes, CRLF
	// mid-line, closing ATX hashes, and multibyte runes.
	source := "# 导论 \"引号\" 与 \\ 反斜杠 ##\r\n\r\n" +
		"首段：整篇不切割，段落是最小定位单位。\n\n" +
		"## 背景 \"历史\"\n\n" +
		"背景段含\t制表符、\\反斜杠、\"引号\"与控制字符\x01。\n\n" +
		"### 细节层\n\n" +
		"细节段落 mixed 中英 byte span — 校验字节区间。\n"
	tree, err := structure.Derive("rev-nasty", []byte(source))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	canonical := canonicalTreeJSON(tree)
	parsed, err := parseCanonicalTree(canonical)
	if err != nil {
		t.Fatalf("parse canonical: %v", err)
	}
	if !reflect.DeepEqual(tree, parsed) {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", parsed, tree)
	}
	if again := canonicalTreeJSON(parsed); string(again) != string(canonical) {
		t.Fatalf("canonical bytes changed across roundtrip:\n got %q\nwant %q", again, canonical)
	}
}
