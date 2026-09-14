package model_test

import (
	"errors"
	"reflect"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestCurrentSearchSnapshotFollowsManualPointerAndRollback(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "search snapshot pointer")
	a := source(t, s, m, "Search source")
	r1 := release(t, s, m, []string{a.RevisionId}, nil, "search-r1")
	b1 := testenv.Ready(t, s, build(t, s, r1, "search-b1"), r1)
	_, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	expectError(t, err, model.ErrNotFound) // READY is not publication.
	r2 := release(t, s, m, []string{a.RevisionId}, nil, "search-r2")
	b2 := testenv.Ready(t, s, build(t, s, r2, "search-b2"), r2)
	activate(t, s, m, r1, b1, 0)
	first, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	first = must(t, first, err)
	assertSnapshot(t, first, m.Id, r1, b1, "1", []string{a.RevisionId})
	for _, p := range testenv.Profiles() {
		if first.Indexes[p.Lane].Key == "" {
			t.Fatalf("missing %s lane", p.Lane)
		}
	}
	// A newer READY candidate is still invisible until a human publishes it.
	again, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	again = must(t, again, err)
	if !reflect.DeepEqual(again, first) {
		t.Fatal("unpublished READY candidate changed the search snapshot")
	}
	activate(t, s, m, r2, b2, 1)
	second, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	second = must(t, second, err)
	assertSnapshot(t, second, m.Id, r2, b2, "2", []string{a.RevisionId})
	activate(t, s, m, r1, b1, 2)
	rolledBack, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	rolledBack = must(t, rolledBack, err)
	assertSnapshot(t, rolledBack, m.Id, r1, b1, "3", []string{a.RevisionId})
	if !reflect.DeepEqual(rolledBack.Indexes, first.Indexes) || rolledBack.PublicationRevision == first.PublicationRevision {
		t.Fatal("rollback did not return the old index at the new publication revision")
	}
	_, err = s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: m.Id,
		TargetKind: "revision", TargetId: a.RevisionId, Reason: "withdraw", IdempotencyKey: "search-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetCurrentSearchSnapshot(ctx, m.Id)
	expectError(t, err, model.ErrUnavailable)
}

func assertSnapshot(t *testing.T, got types.SearchSnapshot, moduleID string, release types.Release,
	build types.Build, pointer string, revisions []string) {
	t.Helper()
	if got.ModuleId != moduleID || got.ReleaseId != release.ReleaseId || got.Generation != build.Generation ||
		got.PublicationRevision != pointer || !reflect.DeepEqual(got.ValidRevisionIds, revisions) || len(got.Indexes) != 3 {
		t.Fatalf("unexpected current search snapshot: %+v", got)
	}
}

func TestCurrentSearchSnapshotRejectsBrokenLanesAndSpace(t *testing.T) {
	for _, scenario := range []string{"missing lane", "wrong space"} {
		t.Run(scenario, func(t *testing.T) {
			s := testenv.Store(t)
			f := makeCitationFixture(t, s)
			broken := f.index
			broken.Lanes = append([]model.LaneManifest(nil), broken.Lanes...)
			if scenario == "missing lane" {
				broken.Lanes = broken.Lanes[:2]
			} else {
				broken.Lanes[0].Profile.Space = "wrong-space"
			}
			ref := testenv.Put(t, s, broken)
			_, err := s.DB.Exec(ctx, `UPDATE knowledge_builds SET data=jsonb_set(
 jsonb_set(data,'{index_manifest_ref}',to_jsonb($2::text)),
 '{index_manifest_hash}',to_jsonb($3::text)) WHERE id=$1`, f.build.BuildId, ref.Key, ref.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.GetCurrentSearchSnapshot(ctx, f.module.Id)
			if !errors.Is(err, model.ErrInvalid) && !errors.Is(err, model.ErrArtifactUnavailable) {
				t.Fatalf("broken %s index issued snapshot: %v", scenario, err)
			}
		})
	}
}

func TestCurrentSearchSnapshotRechecksPointerAfterObjectIO(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "search snapshot race")
	a := source(t, s, m, "Race source")
	r1 := release(t, s, m, []string{a.RevisionId}, nil, "race-r1")
	b1 := testenv.Ready(t, s, build(t, s, r1, "race-b1"), r1)
	r2 := release(t, s, m, []string{a.RevisionId}, nil, "race-r2")
	b2 := testenv.Ready(t, s, build(t, s, r2, "race-b2"), r2)
	activate(t, s, m, r1, b1, 0)
	original := s.Objects
	s.Objects = &interceptRead{Store: original, key: r1.ManifestRef, action: func() {
		activate(t, s, m, r2, b2, 1)
	}}
	_, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	expectError(t, err, model.ErrConflict)
	s.Objects = original
	current, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	current = must(t, current, err)
	assertSnapshot(t, current, m.Id, r2, b2, "2", []string{a.RevisionId})
}

func TestCurrentSearchSnapshotRechecksWithdrawalAfterObjectIO(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "search snapshot withdrawal race")
	a := source(t, s, m, "Withdrawal source")
	r := release(t, s, m, []string{a.RevisionId}, nil, "withdraw-r1")
	b := testenv.Ready(t, s, build(t, s, r, "withdraw-b1"), r)
	activate(t, s, m, r, b, 0)
	original := s.Objects
	s.Objects = &interceptRead{Store: original, key: r.ManifestRef, action: func() {
		_, err := s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: m.Id,
			TargetKind: "revision", TargetId: a.RevisionId, Reason: "withdraw during read", IdempotencyKey: "search-mid-read-withdraw"})
		if err != nil {
			t.Fatal(err)
		}
	}}
	_, err := s.GetCurrentSearchSnapshot(ctx, m.Id)
	expectError(t, err, model.ErrUnavailable)
}

func TestCurrentSearchSnapshotRejectsUnavailableObject(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	original := s.Objects
	s.Objects = &interceptRead{Store: original, key: f.release.ManifestRef, corrupt: true}
	_, err := s.GetCurrentSearchSnapshot(ctx, f.module.Id)
	expectError(t, err, model.ErrArtifactUnavailable)
}
