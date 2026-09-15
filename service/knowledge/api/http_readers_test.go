package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

type httpRequest func(method, path, auth string, in, out any, want int)

// verifyProductReaders runs through the real go-zero process. Structural READY
// fixtures unlock publication; they do not claim actual indexing/model quality.
func verifyProductReaders(t *testing.T, s *model.Store, request httpRequest, token, workerToken, objectRoot string, m types.Module, a, w types.Revision, r types.Release, b types.Build) {
	t.Helper()
	modulePath := "/v1/knowledge/modules/" + m.Id
	var draft types.Module
	request("POST", "/v1/knowledge/modules", token, types.CreateModuleReq{Title: "Draft reader", IdempotencyKey: "http-draft-reader"}, &draft, 200)
	var loaded types.Module
	request("GET", "/v1/knowledge/workbench/modules/"+draft.Id, token, nil, &loaded, 200)
	if loaded.Id != draft.Id || loaded.ActiveReleaseId != "" {
		t.Fatal("admin draft detail mismatched", loaded)
	}
	request("GET", "/v1/knowledge/modules/"+draft.Id, "", nil, nil, 404)
	request("GET", "/v1/knowledge/workbench/modules/"+draft.Id, "", nil, nil, 401)

	var loadedRevision types.Revision
	request("GET", modulePath+"/revisions/"+a.RevisionId, token, nil, &loadedRevision, 200)
	if loadedRevision.Content != "Book A\n\nEvidence" || object.Hash([]byte(loadedRevision.Content)) != a.ContentHash {
		t.Fatal("body/hash mismatch", loadedRevision)
	}
	var loadedRelease types.Release
	request("GET", modulePath+"/releases/"+r.ReleaseId, token, nil, &loadedRelease, 200)
	if loadedRelease.ManifestHash != r.ManifestHash {
		t.Fatal("release mismatch", loadedRelease)
	}
	var loadedBuild types.Build
	request("GET", modulePath+"/builds/"+b.BuildId, token, nil, &loadedBuild, 200)
	if loadedBuild.State != "READY" || loadedBuild.BuildId != b.BuildId {
		t.Fatal("build detail mismatch", loadedBuild)
	}
	var pending types.Compile
	request("POST", modulePath+"/compiles", token, types.CreateCompileReq{PageId: "read-compile", SourceRevisionIds: []string{a.RevisionId}, Guidance: "Explain", IdempotencyKey: "read-compile"}, &pending, 200)
	var loadedCompile types.Compile
	request("GET", modulePath+"/compiles/"+pending.CompileId, token, nil, &loadedCompile, 200)
	if loadedCompile.State != "BUILDING" || loadedCompile.CompileId != pending.CompileId {
		t.Fatal("compile detail mismatch", loadedCompile)
	}
	for _, suffix := range []string{"/revisions/" + a.RevisionId, "/releases/" + r.ReleaseId, "/builds/" + b.BuildId, "/compiles/" + pending.CompileId} {
		request("GET", "/v1/knowledge/modules/"+draft.Id+suffix, token, nil, nil, 404)
		request("GET", modulePath+suffix, "", nil, nil, 401)
	}
	for _, kind := range []string{"revisions", "releases", "builds", "compiles"} {
		request("GET", modulePath+"/"+kind+"?limit=0", token, nil, nil, 400)
		request("GET", modulePath+"/"+kind+"?limit=101", token, nil, nil, 400)
		request("GET", modulePath+"/"+kind+"?limit=abc", token, nil, nil, 400)
		request("GET", modulePath+"/"+kind+"?cursor=invalid", token, nil, nil, 400)
		request("GET", modulePath+"/"+kind, "", nil, nil, 401)
	}
	var revisions types.ListRevisionsResp
	request("GET", modulePath+"/revisions?limit=1", token, nil, &revisions, 200)
	if len(revisions.Items) != 1 || revisions.NextCursor == "" || revisions.Items[0].Content != "" {
		t.Fatal("unbounded body list", revisions)
	}
	request("GET", modulePath+"/revisions?limit=1&cursor="+url.QueryEscape(revisions.NextCursor), token, nil, &revisions, 200)
	qualityHolder := os.Getenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT") != ""
	factSetHolder := os.Getenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT") != ""
	if len(revisions.Items) != 1 || (qualityHolder || factSetHolder) && revisions.NextCursor == "" ||
		!(qualityHolder || factSetHolder) && revisions.NextCursor != "" {
		t.Fatal("second page incorrect", revisions)
	}
	if qualityHolder {
		request("GET", modulePath+"/revisions?limit=1&cursor="+url.QueryEscape(revisions.NextCursor), token, nil, &revisions, 200)
		if len(revisions.Items) != 1 || revisions.NextCursor != "" || revisions.Items[0].Content != "" {
			t.Fatal("explicit quality Holder third Wiki revision page incorrect", revisions)
		}
	}
	if factSetHolder {
		request("GET", modulePath+"/revisions?limit=1&cursor="+url.QueryEscape(revisions.NextCursor), token, nil, &revisions, 200)
		if len(revisions.Items) != 1 || revisions.NextCursor == "" || revisions.Items[0].Content != "" {
			t.Fatal("explicit FactSet Holder third Source/Wiki revision page incorrect", revisions)
		}
		request("GET", modulePath+"/revisions?limit=1&cursor="+url.QueryEscape(revisions.NextCursor), token, nil, &revisions, 200)
		if len(revisions.Items) != 1 || revisions.NextCursor != "" || revisions.Items[0].Content != "" {
			t.Fatal("explicit FactSet Holder fourth Source/Wiki revision page incorrect", revisions)
		}
	}
	var releases types.ListReleasesResp
	request("GET", modulePath+"/releases", token, nil, &releases, 200)
	if len(releases.Items) != 1 || releases.Items[0].ReleaseId != r.ReleaseId {
		t.Fatal(releases)
	}
	var builds types.ListBuildsResp
	request("GET", modulePath+"/builds", token, nil, &builds, 200)
	if len(builds.Items) != 1 || builds.Items[0].BuildId != b.BuildId {
		t.Fatal(builds)
	}
	var compiles types.ListCompilesResp
	request("GET", modulePath+"/compiles", token, nil, &compiles, 200)
	pendingSeen := false
	for _, c := range compiles.Items {
		pendingSeen = pendingSeen || c.CompileId == pending.CompileId
	}
	if !pendingSeen || (qualityHolder || factSetHolder) && len(compiles.Items) != 2 ||
		!(qualityHolder || factSetHolder) && len(compiles.Items) != 1 {
		t.Fatal(compiles)
	}
	// Details support polling after a command and expose a real terminal state.
	request("POST", "/v1/knowledge/compiles/"+pending.CompileId+"/cancel", token, types.CancelCompileReq{Reason: "stop", IdempotencyKey: "read-cancel"}, nil, 200)
	request("GET", modulePath+"/compiles/"+pending.CompileId, token, nil, &loadedCompile, 200)
	if loadedCompile.State != "CANCELLED" {
		t.Fatal("polling fabricated a terminal state", loadedCompile)
	}

	var w2 types.Revision
	request("POST", modulePath+"/wiki-pages/page-a/revisions", token, types.CreateWikiReq{BaseRevisionId: w.RevisionId, Title: "Second interpretation", Content: "New published body", SourceRefs: []types.SourceRef{{RevisionId: a.RevisionId, Locator: "paragraph:2"}}, IdempotencyKey: "http-wiki-2"}, &w2, 200)
	var r2 types.Release
	request("POST", modulePath+"/releases", token, types.CreateReleaseReq{SourceRevisionIds: []string{a.RevisionId}, WikiRevisionIds: []string{w2.RevisionId}, ChunkingProfile: "paragraph-v1", RetrievalProfiles: testenv.Profiles(), IdempotencyKey: "http-release-2"}, &r2, 200)
	candidate, err := s.CreateBuild(context.Background(), "test-admin", types.CreateBuildReq{ReleaseId: r2.ReleaseId, IdempotencyKey: "http-build-2"})
	if err != nil {
		t.Fatal(err)
	}
	candidate = testenv.Ready(t, s, candidate, r2)
	// More than one row in each reader proves both continuation and terminal
	// responses against the generated schema, including omitted-limit requests.
	request("POST", modulePath+"/compiles", token, types.CreateCompileReq{PageId: "schema-compile", SourceRevisionIds: []string{a.RevisionId}, Guidance: "Schema boundary fixture", IdempotencyKey: "schema-compile"}, nil, 200)
	for _, kind := range []string{"revisions", "releases", "builds", "compiles"} {
		verifyPagedReaderSchema(t, request, token, modulePath+"/"+kind)
	}
	for _, path := range []string{modulePath + "/published-releases/" + r2.ReleaseId, modulePath + "/releases/" + r2.ReleaseId + "/revisions", modulePath + "/releases/" + r2.ReleaseId + "/revisions/" + w2.RevisionId} {
		request("GET", path, "", nil, nil, 404)
	}
	request("PUT", modulePath+"/activation", token, types.ActivateReq{ReleaseId: r2.ReleaseId, BuildId: candidate.BuildId, ExpectedPointerRevision: 1, Reason: "publish new interpretation"}, nil, 200)
	snapshotPath := "/internal/v1/knowledge/modules/" + m.Id + "/search-snapshot"
	var snapshot types.SearchSnapshot
	request("GET", snapshotPath, workerToken, nil, &snapshot, 200)
	if snapshot.ReleaseId != r2.ReleaseId || snapshot.Generation != candidate.Generation || snapshot.PublicationRevision != "2" {
		t.Fatalf("current search snapshot followed historical release: %+v", snapshot)
	}
	request("GET", modulePath+"/published-releases/"+r.ReleaseId, "", nil, &loadedRelease, 200)
	if loadedRelease.ReleaseId != r.ReleaseId {
		t.Fatal("historical release followed active pointer")
	}
	publicOld := modulePath + "/releases/" + r.ReleaseId + "/revisions"
	verifyPagedReaderSchema(t, request, "", publicOld)
	request("GET", publicOld+"?limit=1", "", nil, &revisions, 200)
	if len(revisions.Items) != 1 || revisions.Items[0].RevisionId != w.RevisionId || revisions.NextCursor == "" {
		t.Fatal(revisions)
	}
	request("GET", publicOld+"?limit=1&cursor="+url.QueryEscape(revisions.NextCursor), "", nil, &revisions, 200)
	if len(revisions.Items) != 1 || revisions.Items[0].RevisionId != a.RevisionId || revisions.NextCursor != "" {
		t.Fatal(revisions)
	}
	request("GET", publicOld+"/"+w.RevisionId, "", nil, &loadedRevision, 200)
	if loadedRevision.Content != "My interpretation" || loadedRevision.ContentHash != w.ContentHash {
		t.Fatal("historical body changed", loadedRevision)
	}
	request("GET", publicOld+"/"+w2.RevisionId, "", nil, nil, 404)
	request("GET", "/v1/knowledge/modules/"+draft.Id+"/releases/"+r.ReleaseId+"/revisions/"+w.RevisionId, "", nil, nil, 404)
	// A real local object corruption is surfaced as 503 with no body, then the
	// original immutable bytes are restored for the subsequent withdrawal check.
	path := filepath.Join(objectRoot, w.ObjectKey)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("corrupt bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(path, original, 0600)
	request("GET", publicOld+"/"+w.RevisionId, "", nil, nil, 503)
	request("GET", modulePath+"/revisions/"+w.RevisionId, token, nil, nil, 503)
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	request("POST", modulePath+"/withdrawals", token, types.WithdrawReq{TargetKind: "revision", TargetId: w.RevisionId, Reason: "withdraw historical interpretation", IdempotencyKey: "withdraw-old-http"}, nil, 200)
	request("GET", snapshotPath, workerToken, nil, &snapshot, 200)
	if snapshot.ReleaseId != r2.ReleaseId || snapshot.PublicationRevision != "2" {
		t.Fatal("withdrawal of historical member changed current snapshot", snapshot)
	}
	request("GET", publicOld+"/"+w.RevisionId, "", nil, nil, 410)
	request("GET", publicOld, "", nil, nil, 410)
	request("GET", modulePath+"/published-releases/"+r.ReleaseId, "", nil, nil, 410)
	request("GET", modulePath+"/revisions/"+w.RevisionId, token, nil, nil, 410)
	request("GET", modulePath+"/releases/"+r2.ReleaseId+"/revisions/"+w2.RevisionId, "", nil, &loadedRevision, 200)
	request("POST", modulePath+"/withdrawals", token, types.WithdrawReq{TargetKind: "module", TargetId: m.Id, Reason: "withdraw module", IdempotencyKey: "withdraw-module-http"}, nil, 200)
	request("GET", modulePath+"/releases/"+r2.ReleaseId+"/revisions/"+w2.RevisionId, "", nil, nil, 410)
	request("GET", "/v1/knowledge/workbench/modules/"+m.Id, token, nil, &loaded, 200)
	if loaded.Lifecycle != "WITHDRAWN" {
		t.Fatal("admin lost withdrawn module details", loaded)
	}
	request("GET", snapshotPath, workerToken, nil, nil, 410)
}

func verifyPagedReaderSchema(t *testing.T, request httpRequest, token, path string) {
	t.Helper()
	var page struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}
	request("GET", path, token, nil, &page, 200)
	if len(page.Items) < 2 || page.NextCursor != "" {
		t.Fatalf("default limit did not yield bounded complete fixture: %s %+v", path, page)
	}
	total := len(page.Items)
	request("GET", path+"?limit=1", token, nil, &page, 200)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first page lacks continuation: %s %+v", path, page)
	}
	seen := 1
	for page.NextCursor != "" {
		request("GET", path+"?limit=1&cursor="+url.QueryEscape(page.NextCursor), token, nil, &page, 200)
		if len(page.Items) != 1 {
			t.Fatalf("invalid bounded page: %s %+v", path, page)
		}
		seen++
		if seen > total {
			t.Fatalf("pagination exceeded fixture members: %s", path)
		}
	}
	if seen != total {
		t.Fatalf("terminal page omitted members: %s got=%d want=%d", path, seen, total)
	}
}
