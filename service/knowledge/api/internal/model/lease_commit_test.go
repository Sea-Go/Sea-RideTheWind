package model_test

import (
	"context"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

type delayedRead struct {
	object.Store
	key   string
	until time.Time
}

func (s delayedRead) Get(ctx context.Context, key, hash string) ([]byte, error) {
	if key == s.key {
		time.Sleep(time.Until(s.until))
	}
	return s.Store.Get(ctx, key, hash)
}

func TestBuildLeaseExpiresDuringArtifactRead(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "lease-expiry-during-object-read")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := build(t, s, r, "b")
	manifest := testenv.Index(t, s, b, r)
	ref := testenv.Put(t, s, manifest)
	expires := time.Now().Add(150 * time.Millisecond)
	_, err := s.ClaimBuild(ctx, types.ClaimBuildReq{BuildId: b.BuildId, Generation: b.Generation,
		ManifestHash: b.ManifestHash, AttemptId: "attempt", LeaseEpoch: 1,
		LeaseExpiresAt: expires.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = delayedRead{Store: s.Objects, key: ref.Key, until: expires.Add(50 * time.Millisecond)}
	_, err = s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: b.BuildId, Generation: b.Generation,
		ManifestHash: b.ManifestHash, AttemptId: "attempt", LeaseEpoch: 1, State: "READY",
		IndexManifestRef: ref.Key, IndexManifestHash: ref.SHA256})
	expectError(t, err, model.ErrConflict)
	current, err := s.GetBuild(ctx, b.BuildId)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "BUILDING" {
		t.Fatalf("expired lease committed %s", current.State)
	}
	var events int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE event_type='knowledge.index.build.accepted.v1' AND aggregate_id=$1", m.Id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatal("expired attempt emitted acceptance event")
	}
}

func TestCompileLeaseExpiresDuringArtifactRead(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "compile-lease-expiry-during-object-read")
	a := source(t, s, m, "A")
	c, err := s.CreateCompile(ctx, "admin", types.CreateCompileReq{ModuleId: m.Id, PageId: "page",
		SourceRevisionIds: []string{a.RevisionId}, Guidance: "interpret", IdempotencyKey: "compile"})
	if err != nil {
		t.Fatal(err)
	}
	key, hash, err := s.Objects.Put(ctx, []byte("Compiled source interpretation"))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(150 * time.Millisecond)
	_, err = s.ClaimCompile(ctx, types.ClaimCompileReq{CompileId: c.CompileId, Generation: c.Generation,
		InputHash: c.InputHash, AttemptId: "attempt", LeaseEpoch: 1, LeaseExpiresAt: expires.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = delayedRead{Store: s.Objects, key: key, until: expires.Add(50 * time.Millisecond)}
	_, err = s.AcceptCompile(ctx, types.AcceptCompileReq{CompileId: c.CompileId, Generation: c.Generation,
		InputHash: c.InputHash, AttemptId: "attempt", LeaseEpoch: 1, State: "READY", ObjectKey: key,
		ContentHash: hash, Title: "Generated", SourceRefs: []types.SourceRef{{RevisionId: a.RevisionId, Locator: "paragraph:2"}}})
	expectError(t, err, model.ErrConflict)
	current, err := s.GetCompile(ctx, c.CompileId)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "BUILDING" || current.RevisionId != "" {
		t.Fatalf("expired compile committed: %+v", current)
	}
	var revisions int
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_revisions WHERE module_id=$1 AND kind='wiki'", m.Id).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatal("expired compile persisted a wiki revision")
	}
}
