package model_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

var ctx = context.Background()

func must[T any](t *testing.T, v T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func createModule(t *testing.T, s *model.Store, title string) types.Module {
	m, e := s.CreateModule(ctx, "admin", types.CreateModuleReq{Title: title, IdempotencyKey: title})
	return must(t, m, e)
}
func source(t *testing.T, s *model.Store, m types.Module, title string) types.Revision {
	v, e := s.CreateSource(ctx, "admin", types.CreateSourceReq{ModuleId: m.Id, Title: title, Content: "# " + title + "\n\nFirst paragraph.\n\nSecond paragraph.", MediaType: "text/markdown", Provenance: "synthetic test book", IdempotencyKey: title})
	return must(t, v, e)
}
func wiki(t *testing.T, s *model.Store, m types.Module, r types.Revision, base string) types.Revision {
	v, e := s.CreateWiki(ctx, "admin", types.CreateWikiReq{ModuleId: m.Id, PageId: "interpretation", BaseRevisionId: base, Title: "Interpretation", Content: "Interpretation " + base, SourceRefs: []types.SourceRef{{RevisionId: r.RevisionId, Locator: "paragraph:2"}}, IdempotencyKey: "wiki-" + base})
	return must(t, v, e)
}
func release(t *testing.T, s *model.Store, m types.Module, sources, wikis []string, key string) types.Release {
	v, e := s.CreateRelease(ctx, "admin", types.CreateReleaseReq{ModuleId: m.Id, SourceRevisionIds: sources, WikiRevisionIds: wikis, ChunkingProfile: "paragraph-v1", RetrievalProfiles: testenv.Profiles(), IdempotencyKey: key})
	return must(t, v, e)
}
func build(t *testing.T, s *model.Store, r types.Release, key string) types.Build {
	v, e := s.CreateBuild(ctx, "admin", types.CreateBuildReq{ReleaseId: r.ReleaseId, IdempotencyKey: key})
	return must(t, v, e)
}
func activate(t *testing.T, s *model.Store, m types.Module, r types.Release, b types.Build, pointer int64) types.ReleaseState {
	v, e := s.Activate(ctx, "admin", types.ActivateReq{ModuleId: m.Id, ReleaseId: r.ReleaseId, BuildId: b.BuildId, ExpectedPointerRevision: pointer, Reason: "manual test publish"})
	return must(t, v, e)
}
func expectError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
}

func TestRevisionReleaseAndRollback(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Book module")
	a := source(t, s, m, "Book A")
	w1 := wiki(t, s, m, a, "")
	r1 := release(t, s, m, []string{a.RevisionId}, []string{w1.RevisionId}, "v1")
	b1 := testenv.Ready(t, s, build(t, s, r1, "b1"), r1)
	state, e := s.Current(ctx, m.Id)
	state = must(t, state, e)
	if state.PointerRevision != 0 || state.ActiveReleaseId != "" {
		t.Fatal("READY automatically activated")
	}
	p1 := activate(t, s, m, r1, b1, 0)
	if p1.PointerRevision != 1 {
		t.Fatal(p1)
	}
	w2 := wiki(t, s, m, a, w1.RevisionId)
	r2 := release(t, s, m, []string{a.RevisionId}, []string{w2.RevisionId}, "v2")
	b2 := testenv.Ready(t, s, build(t, s, r2, "b2"), r2)
	current, e := s.Published(ctx, m.Id)
	current = must(t, current, e)
	if current.ReleaseId != r1.ReleaseId {
		t.Fatal("draft became visible")
	}
	p2 := activate(t, s, m, r2, b2, 1)
	if p2.PointerRevision != 2 {
		t.Fatal(p2)
	}
	aRead, e := s.GetRevision(ctx, a.RevisionId)
	aRead = must(t, aRead, e)
	if aRead.ContentHash != a.ContentHash {
		t.Fatal("source overwritten")
	}
	wRead, e := s.GetRevision(ctx, w1.RevisionId)
	wRead = must(t, wRead, e)
	if wRead.Content != "Interpretation " {
		t.Fatal("historical wiki overwritten")
	}
	bookB := source(t, s, m, "Book B")
	r3 := release(t, s, m, []string{a.RevisionId, bookB.RevisionId}, []string{w2.RevisionId}, "v3")
	b3 := testenv.Ready(t, s, build(t, s, r3, "b3"), r3)
	activate(t, s, m, r3, b3, 2)
	rollback := activate(t, s, m, r1, b1, 3)
	if rollback.PointerRevision != 4 || rollback.ActiveReleaseId != r1.ReleaseId {
		t.Fatal(rollback)
	}
	other := createModule(t, s, "Other module")
	o, e := s.Current(ctx, other.Id)
	o = must(t, o, e)
	if o.PointerRevision != 0 {
		t.Fatal("cross-module pointer modified")
	}
	var n int
	if e = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_publications WHERE module_id=$1", m.Id).Scan(&n); e != nil || n != 4 {
		t.Fatalf("audit=%d err=%v", n, e)
	}
	if e = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE event_type='knowledge.release.activated.v1' AND aggregate_id=$1", m.Id).Scan(&n); e != nil || n != 4 {
		t.Fatalf("outbox=%d err=%v", n, e)
	}
	_, e = s.DB.Exec(ctx, "UPDATE knowledge_revisions SET data=jsonb_set(data,'{title}','\"changed\"'::jsonb) WHERE id=$1", a.RevisionId)
	if e == nil {
		t.Fatal("DB allowed immutable revision mutation")
	}
	_, e = s.DB.Exec(ctx, "UPDATE knowledge_releases SET data=jsonb_set(data,'{manifest_hash}','\"changed\"'::jsonb) WHERE id=$1", r1.ReleaseId)
	if e == nil {
		t.Fatal("DB allowed immutable release mutation")
	}
	_, e = s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: m.Id, TargetKind: "revision", TargetId: a.RevisionId, Reason: "withdraw book", IdempotencyKey: "withdraw"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Published(ctx, m.Id)
	expectError(t, e, model.ErrUnavailable)
	_, e = s.Activate(ctx, "admin", types.ActivateReq{ModuleId: m.Id, ReleaseId: r2.ReleaseId, BuildId: b2.BuildId, ExpectedPointerRevision: 4, Reason: "rollback"})
	expectError(t, e, model.ErrUnavailable)
}

func TestBuildFencingAndManifestValidation(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Fencing")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := build(t, s, r, "b1")
	claim := types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), BuildId: b.BuildId, Generation: b.Generation, AttemptId: "a1", LeaseEpoch: 1, ManifestHash: b.ManifestHash}
	_, e := s.ClaimBuild(ctx, claim)
	if e != nil {
		t.Fatal(e)
	}
	claim.AttemptId = "a2"
	claim.LeaseEpoch = 2
	_, e = s.ClaimBuild(ctx, claim)
	if e != nil {
		t.Fatal(e)
	}
	b.AttemptId = "a2"
	b.LeaseEpoch = 2
	manifest := testenv.Index(t, s, b, r)
	ref := testenv.Put(t, s, manifest)
	good := types.AcceptBuildReq{BuildId: b.BuildId, Generation: b.Generation, AttemptId: "a2", LeaseEpoch: 2, ManifestHash: b.ManifestHash, State: "READY", IndexManifestRef: ref.Key, IndexManifestHash: ref.SHA256}
	for _, name := range []string{"attempt", "generation", "cancel", "input", "object hash", "missing lane", "space", "count", "probe"} {
		t.Run(name, func(t *testing.T) {
			req := good
			bad := testenv.Index(t, s, b, r)
			switch name {
			case "attempt":
				req.AttemptId = "a1"
				req.LeaseEpoch = 1
			case "generation":
				req.Generation++
			case "cancel":
				req.CancelVersion++
			case "input":
				req.ManifestHash = "wrong"
			case "object hash":
				req.IndexManifestHash = "wrong"
			case "missing lane":
				bad.Lanes = bad.Lanes[:2]
			case "space":
				bad.Lanes[0].Profile.Space = "wrong"
			case "count":
				bad.Lanes[0].ChunkCount = 2
			case "probe":
				bad.Lanes[0].ProbePassed = false
			}
			if name == "missing lane" || name == "space" || name == "count" || name == "probe" {
				ref := testenv.Put(t, s, bad)
				req.IndexManifestRef = ref.Key
				req.IndexManifestHash = ref.SHA256
			}
			if _, err := s.AcceptBuild(ctx, req); err == nil {
				t.Fatal("invalid result accepted")
			}
			got, e := s.GetBuild(ctx, b.BuildId)
			got = must(t, got, e)
			if got.State != "BUILDING" {
				t.Fatal(got)
			}
		})
	}
	ready, e := s.AcceptBuild(ctx, good)
	ready = must(t, ready, e)
	if ready.State != "READY" {
		t.Fatal(ready)
	}
	_, e = s.AcceptBuild(ctx, good)
	if e != nil {
		t.Fatal("duplicate READY failed", e)
	}
	activate(t, s, m, r, ready, 0)
	cancelled := build(t, s, r, "b2")
	_, e = s.ClaimBuild(ctx, types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), BuildId: cancelled.BuildId, Generation: cancelled.Generation, AttemptId: "late", LeaseEpoch: 1, ManifestHash: cancelled.ManifestHash})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.CancelBuild(ctx, "admin", types.CancelBuildReq{BuildId: cancelled.BuildId, Reason: "stop", IdempotencyKey: "cancel"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: cancelled.BuildId, Generation: cancelled.Generation, AttemptId: "late", LeaseEpoch: 1, ManifestHash: cancelled.ManifestHash, State: "FAILED", ErrorCode: "timeout"})
	expectError(t, e, model.ErrConflict)
	stale := build(t, s, r, "b3")
	build(t, s, r, "b4")
	_, e = s.ClaimBuild(ctx, types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), BuildId: stale.BuildId, Generation: stale.Generation, AttemptId: "stale", LeaseEpoch: 1, ManifestHash: stale.ManifestHash})
	expectError(t, e, model.ErrConflict)
	published, e := s.Published(ctx, m.Id)
	published = must(t, published, e)
	if published.ReleaseId != r.ReleaseId {
		t.Fatal("candidate failure modified active release")
	}
}

func TestIdempotencyAndConcurrentActivation(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Concurrent")
	m2 := createModule(t, s, "Concurrent")
	if m.Id != m2.Id {
		t.Fatal("module command duplicated")
	}
	_, err := s.CreateModule(ctx, "admin", types.CreateModuleReq{Title: "different", IdempotencyKey: "Concurrent"})
	expectError(t, err, model.ErrConflict)
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := testenv.Ready(t, s, build(t, s, r, "b"), r)
	var wg sync.WaitGroup
	errorsCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Activate(ctx, fmt.Sprintf("admin-%d", i), types.ActivateReq{ModuleId: m.Id, ReleaseId: r.ReleaseId, BuildId: b.BuildId, ExpectedPointerRevision: 0, Reason: "publish"})
			errorsCh <- err
		}(i)
	}
	wg.Wait()
	close(errorsCh)
	success := 0
	conflicts := 0
	for err := range errorsCh {
		if err == nil {
			success++
		} else if errors.Is(err, model.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 9 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestCompileAcceptanceAndManualEditConflict(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Compile")
	a := source(t, s, m, "A")
	req := types.CreateCompileReq{ModuleId: m.Id, PageId: "interpretation", SourceRevisionIds: []string{a.RevisionId}, Guidance: "interpret source", IdempotencyKey: "compile"}
	c, e := s.CreateCompile(ctx, "admin", req)
	c = must(t, c, e)
	_, e = s.ClaimCompile(ctx, types.ClaimCompileReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), CompileId: c.CompileId, Generation: c.Generation, AttemptId: "attempt", LeaseEpoch: 1, InputHash: c.InputHash})
	if e != nil {
		t.Fatal(e)
	}
	key, hash, e := s.Objects.Put(ctx, []byte("Compiled interpretation"))
	if e != nil {
		t.Fatal(e)
	}
	result := types.AcceptCompileReq{CompileId: c.CompileId, Generation: c.Generation, AttemptId: "attempt", LeaseEpoch: 1, InputHash: c.InputHash, ObjectKey: key, ContentHash: hash, Title: "Compiled", SourceRefs: []types.SourceRef{{RevisionId: a.RevisionId, Locator: "paragraph:2"}}}
	bad := result
	bad.ContentHash = "bad"
	_, e = s.AcceptCompile(ctx, bad)
	expectError(t, e, model.ErrInvalid)
	accepted, e := s.AcceptCompile(ctx, result)
	accepted = must(t, accepted, e)
	if accepted.State != "ACCEPTED" || accepted.RevisionId == "" {
		t.Fatal(accepted)
	}
	replay, e := s.AcceptCompile(ctx, result)
	replay = must(t, replay, e)
	if replay.RevisionId != accepted.RevisionId {
		t.Fatal("duplicate compile created revision")
	}
	req.BaseRevisionId = accepted.RevisionId
	req.IdempotencyKey = "compile2"
	c2, e := s.CreateCompile(ctx, "admin", req)
	c2 = must(t, c2, e)
	_, e = s.ClaimCompile(ctx, types.ClaimCompileReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), CompileId: c2.CompileId, Generation: c2.Generation, AttemptId: "attempt", LeaseEpoch: 1, InputHash: c2.InputHash})
	if e != nil {
		t.Fatal(e)
	}
	human := wiki(t, s, m, a, accepted.RevisionId)
	result.CompileId = c2.CompileId
	result.Generation = c2.Generation
	result.InputHash = c2.InputHash
	_, e = s.AcceptCompile(ctx, result)
	expectError(t, e, model.ErrConflict)
	read, e := s.GetRevision(ctx, human.RevisionId)
	read = must(t, read, e)
	if read.CreatedBy != "admin" {
		t.Fatal("human edit replaced")
	}
	state, e := s.Current(ctx, m.Id)
	state = must(t, state, e)
	if state.PointerRevision != 0 {
		t.Fatal("compile published")
	}
}

func TestCompileCancellationFailureAndExpiry(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Compile lifecycle")
	a := source(t, s, m, "A")
	for _, scenario := range []string{"cancelled", "superseded", "expired", "old attempt", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			req := types.CreateCompileReq{ModuleId: m.Id, PageId: scenario, SourceRevisionIds: []string{a.RevisionId}, Guidance: "compile", IdempotencyKey: scenario}
			c, e := s.CreateCompile(ctx, "admin", req)
			c = must(t, c, e)
			claim := types.ClaimCompileReq{CompileId: c.CompileId, Generation: c.Generation, AttemptId: "attempt1", LeaseEpoch: 1, LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), InputHash: c.InputHash}
			_, e = s.ClaimCompile(ctx, claim)
			if e != nil {
				t.Fatal(e)
			}
			result := types.AcceptCompileReq{CompileId: c.CompileId, Generation: c.Generation, AttemptId: "attempt1", LeaseEpoch: 1, InputHash: c.InputHash, State: "FAILED", ErrorCode: "fixture_worker_timeout"}
			switch scenario {
			case "cancelled":
				_, e = s.CancelCompile(ctx, "admin", types.CancelCompileReq{CompileId: c.CompileId, Reason: "stop", IdempotencyKey: "cancel"})
			case "superseded":
				req.IdempotencyKey += "-new"
				_, e = s.CreateCompile(ctx, "admin", req)
			case "expired":
				_, e = s.DB.Exec(ctx, "UPDATE knowledge_compiles SET data=jsonb_set(data,'{lease_expires_at}',to_jsonb($2::text)) WHERE id=$1", c.CompileId, time.Now().Add(-time.Minute).Format(time.RFC3339Nano))
			case "old attempt":
				claim.AttemptId = "attempt2"
				claim.LeaseEpoch = 2
				_, e = s.ClaimCompile(ctx, claim)
			}
			if e != nil {
				t.Fatal(e)
			}
			got, e := s.AcceptCompile(ctx, result)
			if scenario != "failed" {
				expectError(t, e, model.ErrConflict)
				return
			}
			got = must(t, got, e)
			if got.State != "FAILED" || got.RevisionId != "" {
				t.Fatal(got)
			}
			_, e = s.AcceptCompile(ctx, result)
			if e != nil {
				t.Fatal("failed replay not idempotent", e)
			}
		})
	}
}

func TestExpiredBuildAndOutboxVersions(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Expired build")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := build(t, s, r, "b")
	_, err := s.ClaimBuild(ctx, types.ClaimBuildReq{BuildId: b.BuildId, Generation: b.Generation, AttemptId: "one", LeaseEpoch: 1, LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), ManifestHash: b.ManifestHash})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(ctx, "UPDATE knowledge_builds SET data=jsonb_set(data,'{lease_expires_at}',to_jsonb($2::text)) WHERE id=$1", b.BuildId, time.Now().Add(-time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: b.BuildId, Generation: b.Generation, AttemptId: "one", LeaseEpoch: 1, ManifestHash: b.ManifestHash, State: "FAILED", ErrorCode: "timeout"})
	expectError(t, err, model.ErrConflict)
	rows, err := s.DB.Query(ctx, "SELECT (payload->>'aggregate_version')::bigint,payload->>'operation_id' FROM knowledge_outbox WHERE aggregate_id=$1 ORDER BY (payload->>'aggregate_version')::bigint", m.Id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var previous int64
	for rows.Next() {
		var sequence int64
		var operation string
		if err = rows.Scan(&sequence, &operation); err != nil {
			t.Fatal(err)
		}
		if sequence != previous+1 || operation == "" {
			t.Fatalf("sequence=%d previous=%d operation=%s", sequence, previous, operation)
		}
		previous = sequence
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if previous != 3 {
		t.Fatalf("want source/release/build events, got %d", previous)
	}
}

func TestPublicationRollsBackWhenOutboxCommitFails(t *testing.T) {
	s := testenv.Store(t)
	m := createModule(t, s, "Atomic publication")
	a := source(t, s, m, "A")
	r := release(t, s, m, []string{a.RevisionId}, nil, "r")
	b := testenv.Ready(t, s, build(t, s, r, "b"), r)
	_, err := s.DB.Exec(ctx, `CREATE FUNCTION reject_publication_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='knowledge.release.activated.v1' THEN RAISE EXCEPTION 'fixture outbox failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_publication BEFORE INSERT ON knowledge_outbox FOR EACH ROW EXECUTE FUNCTION reject_publication_event()`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Activate(ctx, "admin", types.ActivateReq{ModuleId: m.Id, ReleaseId: r.ReleaseId, BuildId: b.BuildId, ExpectedPointerRevision: 0, Reason: "publish"})
	if err == nil {
		t.Fatal("outbox failure swallowed")
	}
	state, e := s.Current(ctx, m.Id)
	state = must(t, state, e)
	if state.PointerRevision != 0 || state.ActiveReleaseId != "" {
		t.Fatal("pointer committed without event", state)
	}
	var count int
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_publications WHERE module_id=$1", m.Id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audit=%d err=%v", count, err)
	}
	if _, err = s.DB.Exec(ctx, "DROP TRIGGER reject_publication ON knowledge_outbox"); err != nil {
		t.Fatal(err)
	}
	activate(t, s, m, r, b, 0)
}
