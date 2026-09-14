package model_test

import (
	"sync"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestBuildFenceIsAllocatedAcrossIndependentDCJobs(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "global build fence")
	a := source(t, s, m, "source")
	r := release(t, s, m, []string{a.RevisionId}, nil, "release")
	b := build(t, s, r, "build")
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	claim := types.ClaimBuildReq{BuildId: b.BuildId, Generation: b.Generation,
		ManifestHash: b.ManifestHash, AttemptId: "dc-job-a.attempt-a", LeaseEpoch: 0,
		LeaseExpiresAt: expires}
	first, err := s.ClaimBuild(ctx, claim)
	first = must(t, first, err)
	if first.LeaseEpoch != 1 || first.AttemptId != claim.AttemptId {
		t.Fatalf("RTW did not allocate initial build fence: %+v", first)
	}
	replay, err := s.ClaimBuild(ctx, claim)
	replay = must(t, replay, err)
	if replay.LeaseEpoch != first.LeaseEpoch || replay.LeaseExpiresAt != expires {
		t.Fatalf("lost reply replay minted another fence: %+v", replay)
	}
	renew := claim
	renew.LeaseExpiresAt = time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	renewed, err := s.ClaimBuild(ctx, renew)
	renewed = must(t, renewed, err)
	if renewed.LeaseEpoch != 1 || renewed.LeaseExpiresAt != renew.LeaseExpiresAt {
		t.Fatalf("same attempt renewal changed global fence: %+v", renewed)
	}
	renewReplay, err := s.ClaimBuild(ctx, renew)
	renewReplay = must(t, renewReplay, err)
	if renewReplay.LeaseEpoch != 1 || renewReplay.LeaseExpiresAt != renew.LeaseExpiresAt {
		t.Fatalf("lost renewal reply minted another fence: %+v", renewReplay)
	}
	_, err = s.ClaimBuild(ctx, claim)
	expectError(t, err, model.ErrConflict) // Shortening a live lease is forbidden.
	other := claim
	other.AttemptId = "dc-job-b.attempt-b"
	second, err := s.ClaimBuild(ctx, other)
	second = must(t, second, err)
	if second.LeaseEpoch != 2 || second.AttemptId != other.AttemptId {
		t.Fatalf("new DC job epoch 1 did not receive RTW build epoch 2: %+v", second)
	}
	var current types.Build
	current, err = s.GetBuild(ctx, b.BuildId)
	current = must(t, current, err)
	if current.LeaseEpoch != 2 || current.AttemptId != second.AttemptId {
		t.Fatal("preempted claim did not atomically advance RTW fence")
	}
	jump := other
	jump.AttemptId = "dc-job-c.attempt-c"
	jump.LeaseEpoch = 9
	_, err = s.ClaimBuild(ctx, jump)
	expectError(t, err, model.ErrConflict) // Legacy positive input cannot skip ahead.
	_, err = s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: b.BuildId,
		Generation: b.Generation, ManifestHash: b.ManifestHash, AttemptId: first.AttemptId,
		LeaseEpoch: first.LeaseEpoch, State: "FAILED", ErrorCode: "late"})
	expectError(t, err, model.ErrConflict)
	_, err = s.DB.Exec(ctx, "UPDATE knowledge_builds SET data=jsonb_set(data,'{lease_expires_at}',to_jsonb($2::text)) WHERE id=$1",
		b.BuildId, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	lateRenew := other
	lateRenew.LeaseExpiresAt = time.Now().Add(3 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err = s.ClaimBuild(ctx, lateRenew)
	expectError(t, err, model.ErrConflict) // Expired same attempt cannot revive.
	third := jump
	third.LeaseEpoch = 0
	thirdBuild, err := s.ClaimBuild(ctx, third)
	thirdBuild = must(t, thirdBuild, err)
	if thirdBuild.LeaseEpoch != 3 {
		t.Fatalf("expired B did not advance to global fence three: %+v", thirdBuild)
	}
	// Concurrent new jobs receive unique, increasing grants; only the last
	// grant can submit a terminal RTW result.
	var claims [2]types.ClaimBuildReq
	claims[0], claims[1] = third, third
	claims[0].AttemptId, claims[1].AttemptId = "dc-job-d.attempt-d", "dc-job-e.attempt-e"
	var got [2]types.Build
	var errs [2]error
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) { defer wg.Done(); got[i], errs[i] = s.ClaimBuild(ctx, claims[i]) }(i)
	}
	wg.Wait()
	epochs := map[int64]bool{}
	for i := range got {
		if errs[i] != nil {
			t.Fatalf("concurrent RTW grant failed: %v", errs[i])
		}
		epochs[got[i].LeaseEpoch] = true
	}
	if !epochs[4] || !epochs[5] || len(epochs) != 2 {
		t.Fatalf("concurrent grants reused epoch: %+v", got)
	}
	current, err = s.GetBuild(ctx, b.BuildId)
	current = must(t, current, err)
	if current.LeaseEpoch != 5 {
		t.Fatalf("RTW build fence moved incorrectly: %+v", current)
	}
	for _, earlier := range got {
		if earlier.LeaseEpoch != 4 {
			continue
		}
		_, err = s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: b.BuildId,
			Generation: b.Generation, ManifestHash: b.ManifestHash, AttemptId: earlier.AttemptId,
			LeaseEpoch: earlier.LeaseEpoch, State: "FAILED", ErrorCode: "late"})
		expectError(t, err, model.ErrConflict)
	}
	b.AttemptId, b.LeaseEpoch = current.AttemptId, current.LeaseEpoch
	index := testenv.Index(t, s, b, r)
	ref := testenv.Put(t, s, index)
	ready, err := s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: b.BuildId,
		Generation: b.Generation, ManifestHash: b.ManifestHash, AttemptId: current.AttemptId,
		LeaseEpoch: current.LeaseEpoch, State: "READY", IndexManifestRef: ref.Key,
		IndexManifestHash: ref.SHA256})
	ready = must(t, ready, err)
	if ready.State != "READY" || ready.LeaseEpoch != 5 {
		t.Fatalf("latest fence did not uniquely sign READY: %+v", ready)
	}
	_, err = s.ClaimBuild(ctx, types.ClaimBuildReq{BuildId: b.BuildId,
		Generation: b.Generation, ManifestHash: b.ManifestHash, AttemptId: "post-ready",
		LeaseEpoch: 0, LeaseExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	expectError(t, err, model.ErrConflict)
}
