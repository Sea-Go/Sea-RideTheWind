package model_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func compile(t *testing.T, s *model.Store, m types.Module, a types.Revision, key string) types.Compile {
	t.Helper()
	v, err := s.CreateCompile(ctx, "admin", types.CreateCompileReq{ModuleId: m.Id, PageId: key, SourceRevisionIds: []string{a.RevisionId}, Guidance: "Explain book", IdempotencyKey: key})
	return must(t, v, err)
}

func TestProductReaderScopesAndStablePages(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Product reader")
	other := createModule(t, s, "Other reader")
	a := source(t, s, m, "Book A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "initial")
	b := build(t, s, r, "initial")
	c := compile(t, s, m, a, "initial")
	var err error
	_, err = s.GetModuleRevision(ctx, other.Id, a.RevisionId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetModuleRelease(ctx, other.Id, r.ReleaseId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetModuleBuild(ctx, other.Id, b.BuildId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetModuleCompile(ctx, other.Id, c.CompileId)
	expectError(t, err, model.ErrNotFound)
	got, err := s.GetModuleRevision(ctx, m.Id, a.RevisionId)
	got = must(t, got, err)
	if object.Hash([]byte(got.Content)) != a.ContentHash {
		t.Fatal("admin body does not match immutable hash")
	}

	// Each list gets enough records for several pages and an insertion after the
	// first page. No offset drift, repeated ID or new post-snapshot row is allowed.
	for _, kind := range []string{"revisions", "releases", "builds", "compiles"} {
		t.Run(kind, func(t *testing.T) {
			create := func(key string) string {
				switch kind {
				case "revisions":
					return source(t, s, m, key).RevisionId
				case "releases":
					return release(t, s, m, []string{a.RevisionId}, nil, key).ReleaseId
				case "builds":
					return build(t, s, r, key).BuildId
				default:
					return compile(t, s, m, a, key).CompileId
				}
			}
			load := func(req types.ModulePageReq) ([]string, string, error) {
				ids := []string{}
				switch kind {
				case "revisions":
					page, e := s.ListRevisions(ctx, req)
					for _, v := range page.Items {
						if v.Content != "" {
							t.Fatal("list unexpectedly loads body")
						}
						ids = append(ids, v.RevisionId)
					}
					return ids, page.NextCursor, e
				case "releases":
					page, e := s.ListReleases(ctx, req)
					for _, v := range page.Items {
						ids = append(ids, v.ReleaseId)
					}
					return ids, page.NextCursor, e
				case "builds":
					page, e := s.ListBuilds(ctx, req)
					for _, v := range page.Items {
						ids = append(ids, v.BuildId)
					}
					return ids, page.NextCursor, e
				default:
					page, e := s.ListCompiles(ctx, req)
					for _, v := range page.Items {
						ids = append(ids, v.CompileId)
					}
					return ids, page.NextCursor, e
				}
			}
			for i := 0; i < 5; i++ {
				create(fmt.Sprintf("%s-%d", kind, i))
			}
			before, _, e := load(types.ModulePageReq{ModuleId: m.Id, Limit: 100})
			if e != nil {
				t.Fatal(e)
			}
			first, next, e := load(types.ModulePageReq{ModuleId: m.Id, Limit: 2})
			if e != nil || len(first) != 2 || next == "" {
				t.Fatalf("first=%v next=%q err=%v", first, next, e)
			}
			newID := create(kind + "-after-first-page")
			_, _, e = load(types.ModulePageReq{ModuleId: other.Id, Limit: 2, Cursor: next})
			expectError(t, e, model.ErrInvalid)
			seen := append([]string{}, first...)
			for next != "" {
				var ids []string
				ids, next, e = load(types.ModulePageReq{ModuleId: m.Id, Limit: 2, Cursor: next})
				if e != nil || len(ids) > 2 {
					t.Fatalf("page=%v err=%v", ids, e)
				}
				seen = append(seen, ids...)
				if len(seen) > len(before) {
					t.Fatal("pagination did not terminate at original membership")
				}
			}
			if !reflect.DeepEqual(seen, before) {
				t.Fatalf("paged=%v snapshot=%v", seen, before)
			}
			fresh, _, e := load(types.ModulePageReq{ModuleId: m.Id, Limit: 100})
			if e != nil || fresh[0] != newID {
				t.Fatalf("refresh=%v error=%v", fresh, e)
			}
			for _, limit := range []int{-1, 0, 101} {
				_, _, e = load(types.ModulePageReq{ModuleId: m.Id, Limit: limit})
				expectError(t, e, model.ErrInvalid)
			}
			_, _, e = load(types.ModulePageReq{ModuleId: m.Id, Limit: 20, Cursor: "not-a-cursor"})
			expectError(t, e, model.ErrInvalid)
			_, _, e = load(types.ModulePageReq{ModuleId: "missing", Limit: 20})
			expectError(t, e, model.ErrNotFound)
			empty, end, e := load(types.ModulePageReq{ModuleId: other.Id, Limit: 20})
			if e != nil || len(empty) != 0 || end != "" {
				t.Fatalf("empty list=%v %q %v", empty, end, e)
			}
		})
	}
	page, err := s.ListRevisions(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ListBuilds(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 1, Cursor: page.NextCursor})
	expectError(t, err, model.ErrInvalid)
}

func TestPublicHistoryMembershipAndWithdrawal(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "History")
	other := createModule(t, s, "Other history")
	a := source(t, s, m, "Book A")
	w1 := wiki(t, s, m, a, "")
	r1 := release(t, s, m, []string{a.RevisionId}, []string{w1.RevisionId}, "r1")
	b1 := testenv.Ready(t, s, build(t, s, r1, "b1"), r1)
	_, err := s.GetHistoricalRelease(ctx, m.Id, r1.ReleaseId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetPublishedRevision(ctx, m.Id, r1.ReleaseId, a.RevisionId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.ListPublishedRevisions(ctx, types.PublishedRevisionsReq{ModuleId: m.Id, ReleaseId: r1.ReleaseId, Limit: 20})
	expectError(t, err, model.ErrNotFound)
	activate(t, s, m, r1, b1, 0)
	w2 := wiki(t, s, m, a, w1.RevisionId)
	r2 := release(t, s, m, []string{a.RevisionId}, []string{w2.RevisionId}, "r2")
	b2 := testenv.Ready(t, s, build(t, s, r2, "b2"), r2)
	activate(t, s, m, r2, b2, 1)
	got, err := s.GetPublishedRevision(ctx, m.Id, r1.ReleaseId, w1.RevisionId)
	got = must(t, got, err)
	if got.Content != "Interpretation " || got.ContentHash != w1.ContentHash || object.Hash([]byte(got.Content)) != got.ContentHash {
		t.Fatal("history was replaced by active revision", got)
	}
	_, err = s.GetPublishedRevision(ctx, m.Id, r1.ReleaseId, w2.RevisionId)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetPublishedRevision(ctx, other.Id, r1.ReleaseId, w1.RevisionId)
	expectError(t, err, model.ErrNotFound)
	p, err := s.ListPublishedRevisions(ctx, types.PublishedRevisionsReq{ModuleId: m.Id, ReleaseId: r1.ReleaseId, Limit: 1})
	p = must(t, p, err)
	if len(p.Items) != 1 || p.Items[0].RevisionId != w1.RevisionId || p.NextCursor == "" {
		t.Fatal(p)
	}
	q, err := s.ListPublishedRevisions(ctx, types.PublishedRevisionsReq{ModuleId: m.Id, ReleaseId: r1.ReleaseId, Limit: 1, Cursor: p.NextCursor})
	q = must(t, q, err)
	if len(q.Items) != 1 || q.Items[0].RevisionId != a.RevisionId || q.NextCursor != "" {
		t.Fatal(q)
	}
	_, err = s.ListPublishedRevisions(ctx, types.PublishedRevisionsReq{ModuleId: m.Id, ReleaseId: r2.ReleaseId, Limit: 1, Cursor: p.NextCursor})
	expectError(t, err, model.ErrInvalid)
	_, err = s.ListRevisions(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 1, Cursor: p.NextCursor})
	expectError(t, err, model.ErrInvalid)
	// Withdrawing the historical-only Wiki invalidates r1, while r2 remains valid.
	_, err = s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: m.Id, TargetKind: "revision", TargetId: w1.RevisionId, Reason: "withdraw old interpretation", IdempotencyKey: "old-wiki"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetPublishedRevision(ctx, m.Id, r1.ReleaseId, a.RevisionId)
	expectError(t, err, model.ErrUnavailable)
	_, err = s.GetModuleRevision(ctx, m.Id, w1.RevisionId)
	expectError(t, err, model.ErrUnavailable)
	_, err = s.GetHistoricalRelease(ctx, m.Id, r2.ReleaseId)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: m.Id, TargetKind: "module", TargetId: m.Id, Reason: "withdraw module", IdempotencyKey: "module"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetPublishedRevision(ctx, m.Id, r2.ReleaseId, w2.RevisionId)
	expectError(t, err, model.ErrUnavailable)
	admin, err := s.GetModule(ctx, m.Id, false)
	admin = must(t, admin, err)
	if admin.Lifecycle != "WITHDRAWN" {
		t.Fatal("admin cannot inspect withdrawn module state")
	}
}

// The callback occurs during external I/O; attempting a write here also detects
// accidental DB locks held over object reads (the context deadline would fail).
type interceptRead struct {
	object.Store
	key     string
	once    sync.Once
	action  func()
	corrupt bool
}

func (s *interceptRead) Get(ctx context.Context, key, hash string) ([]byte, error) {
	if key == s.key {
		if s.action != nil {
			s.once.Do(s.action)
		}
		if s.corrupt {
			return []byte("corrupt content"), nil
		}
	}
	return s.Store.Get(ctx, key, hash)
}

func TestReadersRecheckWithdrawalAfterObjectIO(t *testing.T) {
	for _, scenario := range []string{"admin-revision", "public-source-dependency", "public-module"} {
		t.Run(scenario, func(t *testing.T) {
			s := testenv.Store(t)
			m := createModule(t, s, scenario)
			a := source(t, s, m, "A")
			w := wiki(t, s, m, a, "")
			r := release(t, s, m, []string{a.RevisionId}, []string{w.RevisionId}, "release")
			b := testenv.Ready(t, s, build(t, s, r, "build"), r)
			activate(t, s, m, r, b, 0)
			store := s.Objects
			s.Objects = &interceptRead{Store: store, key: w.ObjectKey, action: func() {
				req := types.WithdrawReq{ModuleId: m.Id, TargetKind: "revision", TargetId: w.RevisionId, Reason: "during read", IdempotencyKey: "withdraw"}
				if scenario == "public-source-dependency" {
					req.TargetId = a.RevisionId
				}
				if scenario == "public-module" {
					req.TargetKind = "module"
					req.TargetId = m.Id
				}
				if _, err := s.Withdraw(ctx, "admin", req); err != nil {
					t.Fatal(err)
				}
			}}
			var err error
			if scenario == "admin-revision" {
				_, err = s.GetModuleRevision(ctx, m.Id, w.RevisionId)
			} else {
				_, err = s.GetPublishedRevision(ctx, m.Id, r.ReleaseId, w.RevisionId)
			}
			expectError(t, err, model.ErrUnavailable)
		})
	}
}

func TestReadersRejectCorruptBodyAndManifest(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Corrupt bytes")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := testenv.Ready(t, s, build(t, s, r, "b"), r)
	activate(t, s, m, r, b, 0)
	original := s.Objects
	s.Objects = &interceptRead{Store: original, key: a.ObjectKey, corrupt: true}
	_, err := s.GetModuleRevision(ctx, m.Id, a.RevisionId)
	expectError(t, err, model.ErrArtifactUnavailable)
	_, err = s.GetPublishedRevision(ctx, m.Id, r.ReleaseId, a.RevisionId)
	expectError(t, err, model.ErrArtifactUnavailable)
	s.Objects = &interceptRead{Store: original, key: r.ManifestRef, corrupt: true}
	_, err = s.GetHistoricalRelease(ctx, m.Id, r.ReleaseId)
	expectError(t, err, model.ErrArtifactUnavailable)
}

func TestPagingMigrationPreservesImmutableHistory(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Migration history")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := build(t, s, r, "b")
	c := compile(t, s, m, a, "c")
	// Model the previous schema with existing immutable and execution rows. The
	// migration must add identities without UPDATEing immutable record payloads.
	for _, kind := range []string{"revisions", "releases", "builds", "compiles"} {
		if _, err := s.DB.Exec(ctx, "ALTER TABLE knowledge_"+kind+" DROP COLUMN list_order CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("migration is not idempotent", err)
	}
	p, err := s.ListRevisions(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 20})
	p = must(t, p, err)
	if len(p.Items) != 1 || p.Items[0].RevisionId != a.RevisionId || p.Items[0].ContentHash != a.ContentHash {
		t.Fatal(p)
	}
	q, err := s.ListReleases(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 20})
	q = must(t, q, err)
	if len(q.Items) != 1 || q.Items[0].ReleaseId != r.ReleaseId || q.Items[0].ManifestHash != r.ManifestHash {
		t.Fatal(q)
	}
	builds, err := s.ListBuilds(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 20})
	builds = must(t, builds, err)
	if len(builds.Items) != 1 || builds.Items[0].BuildId != b.BuildId {
		t.Fatal(builds)
	}
	compiles, err := s.ListCompiles(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 20})
	compiles = must(t, compiles, err)
	if len(compiles.Items) != 1 || compiles.Items[0].CompileId != c.CompileId {
		t.Fatal(compiles)
	}
	newRevision := source(t, s, m, "New after migration")
	p, err = s.ListRevisions(ctx, types.ModulePageReq{ModuleId: m.Id, Limit: 20})
	p = must(t, p, err)
	if len(p.Items) != 2 || p.Items[0].RevisionId != newRevision.RevisionId {
		t.Fatal("new identity did not follow migrated rows", p)
	}
	for _, kind := range []string{"revisions", "releases"} {
		if _, err = s.DB.Exec(ctx, "UPDATE knowledge_"+kind+" SET list_order=list_order+100 WHERE module_id=$1", m.Id); err == nil {
			t.Fatal("immutable creation identity was changed", kind)
		}
	}
}
