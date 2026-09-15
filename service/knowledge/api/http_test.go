package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/conf"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
	rtwjwt "sea-try-go/service/user/common/jwt"
	"sea-try-go/service/user/user/rpc/pb"
)

type productUserRPC struct {
	pb.UnimplementedUserServiceServer
	deleted       atomic.Bool
	missingStatus atomic.Bool
	calls         atomic.Int64
}

func (s *productUserRPC) GetUser(_ context.Context, req *pb.GetUserReq) (*pb.GetUserResp, error) {
	s.calls.Add(1)
	if s.deleted.Load() && req.Uid == 9123 {
		return nil, status.Error(codes.NotFound, "user deleted")
	}
	if req.Uid != 9123 && req.Uid != 7777 && req.Uid != 8888 {
		return &pb.GetUserResp{Found: false}, nil
	}
	uid := req.Uid
	if uid == 8888 {
		uid = 7777 // An RPC mismatch must never produce a SubjectRef.
	}
	info := &pb.UserInfo{Uid: uid}
	if !s.missingStatus.Load() {
		active := int64(0)
		info.Status = &active
	}
	return &pb.GetUserResp{Found: true, User: info}, nil
}

func TestHTTPProcessHelper(t *testing.T) {
	path := os.Getenv("KNOWLEDGE_TEST_SERVER_CONFIG")
	if path == "" {
		t.Skip("integration subprocess only")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c config.Config
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if err = run(c); err != nil {
		t.Fatal(err)
	}
}
func TestRealHTTPKnowledgeWorkflow(t *testing.T) {
	runRealHTTPKnowledgeWorkflow(t, false)
}

func runRealHTTPKnowledgeWorkflow(t *testing.T, realUser bool) {
	publishNewRelease := os.Getenv("SEA_BGE_WORKER_PUBLISH_SEARCH") == "1"
	contract := loadGeneratedHTTPContract(t)
	s := testenv.Store(t)
	dir := t.TempDir()
	objects, err := object.NewLocal(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	const userSecret = "synthetic-test-user-secret-not-a-real-key"
	var userRPC *productUserRPC
	var userServer *grpc.Server
	var realUsers *realUserServices
	var userEndpoint string
	productUID := int64(9123)
	otherUID := int64(7777)
	var productToken, otherToken string
	if realUser {
		realUsers = startRealUserServices(t, s, userSecret)
		productUID, productToken = realUsers.registerAndLogin(t, "knowledge-history-owner")
		otherUID, otherToken = realUsers.registerAndLogin(t, "knowledge-history-other")
		userEndpoint = realUsers.conn.Target()
	} else {
		userListener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		userRPC = &productUserRPC{}
		userServer = grpc.NewServer()
		pb.RegisterUserServiceServer(userServer, userRPC)
		go func() { _ = userServer.Serve(userListener) }()
		t.Cleanup(func() { userServer.Stop(); _ = userListener.Close() })
		userEndpoint = userListener.Addr().String()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	var c config.Config
	if err = conf.FillDefault(&c); err != nil {
		t.Fatal(err)
	}
	c.Name = "knowledge-http-test"
	c.Mode = "test"
	c.Host = "127.0.0.1"
	c.Port = port
	c.Timeout = 120000
	c.Log.Mode = "console"
	c.Log.Level = "info"
	c.Observability.Version = os.Getenv("KNOWLEDGE_TEST_VERSION")
	if c.Observability.Version == "" {
		c.Observability.Version = "knowledge-http-test-binary"
	}
	c.Auth.AccessSecret = "synthetic-test-jwt-secret-not-a-real-key"
	c.Auth.AccessExpire = 3600
	c.UserAuth.AccessSecret = userSecret
	c.UserRpc.Endpoints = []string{userEndpoint}
	c.AdministratorIDs = []string{"test-admin"}
	c.WorkerToken = "synthetic-worker-token"
	var base string
	searchFixture := newProductSearchFixture(t, &base, c.WorkerToken)
	c.SearchSummary.Endpoint = searchFixture.server.URL + "/v1/search/summary"
	c.SearchSummary.ScopeKey = productFixtureScopeKey
	if os.Getenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE") != "" {
		c.SearchSummary.FastTimeoutMillis = 60000 // measured local Ollama may need more than the fixed fixture
	}
	toolFixture := newToolSearchFixture(t)
	c.SearchTools.Endpoint = toolFixture.server.URL + "/v1/search/tools/search"
	c.SearchTools.ScopeKey = toolFixtureScopeKey
	c.SearchJudgments.Enabled = true
	c.GroundingReviews.Enabled = true
	dsn, err := url.Parse(os.Getenv("KNOWLEDGE_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", s.DB.Config().ConnConfig.RuntimeParams["search_path"])
	dsn.RawQuery = query.Encode()
	c.Postgres.DSN = dsn.String()
	c.Postgres.MaxConnections = 4
	c.Objects.Backend = "local"
	c.Objects.LocalDirectory = filepath.Join(dir, "objects")
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "server.json")
	if err = os.WriteFile(cfg, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHTTPProcessHelper$")
	cmd.Env = append(os.Environ(), "KNOWLEDGE_TEST_SERVER_CONFIG="+cfg)
	logFile, err := os.Create(filepath.Join(dir, "http.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		logFile.Close()
		if evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidence != "" {
			if err := os.MkdirAll(evidence, 0700); err == nil {
				if raw, err := os.ReadFile(filepath.Join(dir, "http.log")); err == nil {
					var runtimeLines [][]byte
					for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
						if bytes.Equal(line, []byte("PASS")) {
							continue // go test's helper-process epilogue, not service output.
						}
						if !json.Valid(line) {
							t.Errorf("non-JSON shutdown output: %q", line)
							continue
						}
						runtimeLines = append(runtimeLines, line)
					}
					_ = os.WriteFile(filepath.Join(evidence, "knowledge-http.jsonl"), append(bytes.Join(runtimeLines, []byte("\n")), '\n'), 0600)
				}
			}
		}
		if t.Failed() {
			log, _ := os.ReadFile(filepath.Join(dir, "http.log"))
			t.Log(string(log))
		}
	})
	base = "http://" + net.JoinHostPort("127.0.0.1", fmtInt(port))
	clientTimeout := 5 * time.Second
	if os.Getenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE") != "" {
		clientTimeout = 95 * time.Second
	}
	client := &http.Client{Timeout: clientTimeout}
	deadline := time.Now().Add(15 * time.Second)
	for {
		res, e := client.Get(base + "/v1/knowledge/modules")
		if e == nil {
			res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		select {
		case err := <-exited:
			t.Fatal("server exited", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"userId": "test-admin", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(c.Auth.AccessSecret))
	if err != nil {
		t.Fatal(err)
	}
	nonAdminToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{"userId": "not-an-admin", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(c.Auth.AccessSecret))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, auth string, in, out any, want int) {
		t.Helper()
		var body io.Reader
		if in != nil {
			raw, e := json.Marshal(in)
			if e != nil {
				t.Fatal(e)
			}
			body = bytes.NewReader(raw)
		}
		req, e := http.NewRequest(method, base+path, body)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
		}
		if want == 200 {
			contract.validateSuccess(t, method, req.URL, raw)
		}
		if out != nil {
			var envelope struct {
				Code int             `json:"code"`
				Data json.RawMessage `json:"data"`
			}
			if e = json.Unmarshal(raw, &envelope); e != nil {
				t.Fatal(e)
			}
			if envelope.Code != want {
				t.Fatalf("bad envelope: %s", raw)
			}
			// Each HTTP response is a fresh object; omitted optional fields must not
			// accidentally retain a previous test response (for example next_cursor).
			value := reflect.ValueOf(out).Elem()
			value.Set(reflect.Zero(value.Type()))
			if e = json.Unmarshal(envelope.Data, out); e != nil {
				t.Fatal(e)
			}
		}
	}
	request("POST", "/v1/knowledge/modules", "", map[string]any{"title": "No auth", "idempotency_key": "no-auth"}, nil, 401)
	request("POST", "/v1/knowledge/no-such-route", "", nil, nil, 404)
	request("POST", "/v1/knowledge/modules", token, map[string]any{"title": "missing key"}, nil, 400)
	var m types.Module
	request("POST", "/v1/knowledge/modules", token, types.CreateModuleReq{Title: "HTTP book", IdempotencyKey: "http-module"}, &m, 200)
	var state types.ReleaseState
	request("GET", "/v1/knowledge/modules/"+m.Id+"/releases/current", token, nil, &state, 200)
	if state.BuildState != "NOT_BUILT" {
		t.Fatal(state)
	}
	var a types.Revision
	request("POST", "/v1/knowledge/modules/"+m.Id+"/sources", token, types.CreateSourceReq{Title: "Book A", Content: "Book A\n\nEvidence", MediaType: "text/markdown", Provenance: "synthetic", IdempotencyKey: "http-source"}, &a, 200)
	var w types.Revision
	request("POST", "/v1/knowledge/modules/"+m.Id+"/wiki-pages/page-a/revisions", token, types.CreateWikiReq{Title: "Interpretation", Content: "My interpretation", SourceRefs: []types.SourceRef{{RevisionId: a.RevisionId, Locator: "paragraph:2"}}, IdempotencyKey: "http-wiki"}, &w, 200)
	realBGE := os.Getenv("SEA_DC_BGE_RUNTIME")
	retrievalProfiles := testenv.Profiles()
	if realBGE != "" {
		if os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT") == "" {
			t.Fatal("actual DC BGE acceptance requires the independent BTW product consumer")
		}
		if formal := os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT"); formal != "" &&
			formal != os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT") {
			t.Fatal("formal cmd/api and real-index builder must use the same BTW worktree")
		}
		retrievalProfiles = realBGEProfiles(t, realBGE)
	}
	var r types.Release
	request("POST", "/v1/knowledge/modules/"+m.Id+"/releases", token, types.CreateReleaseReq{SourceRevisionIds: []string{a.RevisionId}, WikiRevisionIds: []string{w.RevisionId}, ChunkingProfile: "paragraph-v1", RetrievalProfiles: retrievalProfiles, IdempotencyKey: "http-release"}, &r, 200)
	var build types.Build
	request("POST", "/v1/knowledge/releases/"+r.ReleaseId+"/index-builds", token, types.CreateBuildReq{IdempotencyKey: "http-build"}, &build, 200)
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/claim", c.WorkerToken, types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), Generation: build.Generation, AttemptId: "http-worker", LeaseEpoch: 1, ManifestHash: build.ManifestHash}, &build, 200)
	// The HTTP citation path consumes a real fixed chunk/locator, not the
	// structural-only chunk marker used by other H06 test fixtures.
	quote := "Evidence"
	quoteHash := object.Hash([]byte(quote))
	chunkIdentity, err := json.Marshal([]any{a.RevisionId, r.ChunkingProfile, 2, 0, quoteHash})
	if err != nil {
		t.Fatal(err)
	}
	chunk := types.CitationChunk{ChunkId: object.Hash(chunkIdentity), RevisionId: a.RevisionId,
		ContentId: a.EntityId, SourceKind: a.Kind, Original: types.CitationObject{Key: a.ObjectKey, Sha256: a.ContentHash},
		Location: types.CitationLocation{Locator: "paragraph:2", OriginalByteStart: 8, OriginalByteEnd: 16,
			NormalizedRuneStart: 0, NormalizedRuneEnd: len([]rune(quote))},
		Text: quote, TextHash: quoteHash, EncodingKey: object.Hash([]byte(quote)), Required: true}
	if realBGE != "" {
		encoded, encodeErr := json.Marshal([]any{r.ChunkingProfile, quote})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		chunk.EncodingKey = object.Hash(encoded)
	}
	chunkManifest := map[string]any{"schema_version": 1, "module_id": m.Id, "release_id": r.ReleaseId,
		"input_manifest_hash": r.ManifestHash, "profile": r.ChunkingProfile, "parser_version": "sea.paragraph.v1",
		"chunker_version": "trpc.fixed.v1.8.1", "chunk_size": 32, "overlap": 0,
		"inputs": []any{map[string]any{"revision_id": a.RevisionId, "content_id": a.EntityId,
			"source_kind": a.Kind, "original": chunk.Original, "chunk_count": 1}}, "chunks": []types.CitationChunk{chunk}}
	if realBGE != "" {
		wikiText := "My interpretation"
		wikiHash := object.Hash([]byte(wikiText))
		wikiIdentity, identityErr := json.Marshal([]any{w.RevisionId, r.ChunkingProfile, 1, 0, wikiHash})
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		wikiEncoding, encodeErr := json.Marshal([]any{r.ChunkingProfile, wikiText})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		wikiChunk := types.CitationChunk{ChunkId: object.Hash(wikiIdentity), RevisionId: w.RevisionId,
			ContentId: w.EntityId, SourceKind: w.Kind, Original: types.CitationObject{Key: w.ObjectKey, Sha256: w.ContentHash},
			Location: types.CitationLocation{Locator: "paragraph:1", OriginalByteStart: 0,
				OriginalByteEnd: len(wikiText), NormalizedRuneStart: 0, NormalizedRuneEnd: len([]rune(wikiText))},
			Text: wikiText, TextHash: wikiHash, EncodingKey: object.Hash(wikiEncoding), Required: true}
		chunkManifest["inputs"] = []any{map[string]any{"revision_id": a.RevisionId, "content_id": a.EntityId,
			"source_kind": a.Kind, "original": chunk.Original, "chunk_count": 1},
			map[string]any{"revision_id": w.RevisionId, "content_id": w.EntityId,
				"source_kind": w.Kind, "original": wikiChunk.Original, "chunk_count": 1}}
		chunkManifest["chunks"] = []types.CitationChunk{chunk, wikiChunk}
	}
	chunkManifestRef := testenv.Put(t, s, chunkManifest)
	var index model.IndexManifest
	var ref model.ArtifactRef
	var builtIndex realIndexResult
	var realSearchEndpoint string
	var formalAPI *formalSearchAPI
	if realBGE == "" {
		index = testenv.Index(t, s, build, r)
		index.ChunkManifest = chunkManifestRef
		ref = testenv.Put(t, s, index)
	} else {
		setup := &realIndexSetup{DCRuntime: realBGE, ArtifactDir: filepath.Join(dir, "objects"),
			ChunkManifest: chunkManifestRef, BuildID: build.BuildId, ReleaseID: r.ReleaseId,
			Generation: build.Generation, ResultPath: filepath.Join(dir, "btw-real-index-result.json"),
			ExpectedQuote: quote, ExpectedChunkID: chunk.ChunkId}
		if os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT") != "" {
			buildRealBTWIndexesOnly(t, dir, os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT"),
				base, c.WorkerToken, m.Id, setup)
		} else {
			realSearchEndpoint = startRealBTWProductServer(t, dir, os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT"),
				base, c.WorkerToken, m.Id, &chunk, setup)
		}
		resultRaw, readErr := os.ReadFile(setup.ResultPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(resultRaw, &builtIndex); err != nil || builtIndex.ChunkCount != 2 || len(builtIndex.Indexes) != 3 ||
			(os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT") != "" && len(builtIndex.APIIndexSettings) == 0) {
			t.Fatal("BTW actual BGE three-lane result incomplete")
		}
		ref = builtIndex.IndexManifest
		indexRaw, getErr := s.Objects.Get(context.Background(), ref.Key, ref.SHA256)
		if getErr != nil || json.Unmarshal(indexRaw, &index) != nil || index.ChunkManifest != chunkManifestRef ||
			index.ChunkCount != 2 || len(index.Lanes) != 3 {
			t.Fatal("RTW cannot read BTW actual three-lane index manifest")
		}
		for _, lane := range index.Lanes {
			if builtIndex.Indexes[lane.Profile.Lane] != lane.Artifact {
				t.Fatal("BTW reported lane reference differs from immutable index manifest")
			}
		}
	}
	result := types.AcceptBuildReq{Generation: build.Generation, AttemptId: build.AttemptId, LeaseEpoch: build.LeaseEpoch, ManifestHash: build.ManifestHash, State: "READY", IndexManifestRef: ref.Key, IndexManifestHash: ref.SHA256}
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/results", c.WorkerToken, result, &build, 200)
	request("GET", "/v1/knowledge/modules/"+m.Id+"/published", "", nil, nil, 404)
	snapshotPath := "/internal/v1/knowledge/modules/" + m.Id + "/search-snapshot"
	request("GET", snapshotPath, "", nil, nil, 401)
	request("GET", snapshotPath, c.WorkerToken, nil, nil, 404)
	request("GET", "/v1/knowledge/modules/"+m.Id+"/releases/current", token, nil, &state, 200)
	if state.BuildState != "READY" || state.ActiveReleaseId != "" {
		t.Fatal(state)
	}
	activation := types.ActivateReq{ReleaseId: r.ReleaseId, BuildId: build.BuildId, ExpectedPointerRevision: 0, Reason: "manual HTTP publication"}
	request("PUT", "/v1/knowledge/modules/"+m.Id+"/activation", token, activation, &state, 200)
	if state.PointerRevision != 1 || state.ActiveReleaseId != r.ReleaseId {
		t.Fatal(state)
	}
	if btwRoot := os.Getenv("SEA_BTW_ITEM_CONSUMER_ROOT"); btwRoot != "" {
		runRealBTWItemPool(t, btwRoot, base, c.WorkerToken, os.Getenv("KNOWLEDGE_TEST_DSN"),
			m.Id, a.RevisionId, a.EntityId)
	}
	var searchSnapshot types.SearchSnapshot
	request("GET", snapshotPath, c.WorkerToken, nil, &searchSnapshot, 200)
	if searchSnapshot.ModuleId != m.Id || searchSnapshot.ReleaseId != r.ReleaseId ||
		searchSnapshot.Generation != build.Generation || searchSnapshot.PublicationRevision != "1" ||
		!reflect.DeepEqual(searchSnapshot.ValidRevisionIds, []string{a.RevisionId, w.RevisionId}) || len(searchSnapshot.Indexes) != 3 {
		t.Fatalf("HTTP current search snapshot differs from manual publication: %+v", searchSnapshot)
	}
	for _, lane := range index.Lanes {
		got := searchSnapshot.Indexes[lane.Profile.Lane]
		if got.Key != lane.Artifact.Key || got.Sha256 != lane.Artifact.SHA256 {
			t.Fatalf("HTTP %s index ref differs from accepted build", lane.Profile.Lane)
		}
	}
	if socketRoot := os.Getenv("SEA_BTW_SEARCH_API_SOCKET_ROOT"); socketRoot != "" {
		if realBGE == "" {
			t.Fatal("formal cmd/api real-socket gate requires actual DC BGE runtime")
		}
		formalAPI = startRealBTWSearchAPIProcess(t, dir, socketRoot, base, c.WorkerToken,
			realBGE, builtIndex, quote)
		realSearchEndpoint = formalAPI.URL
	}
	request("PUT", "/v1/knowledge/modules/"+m.Id+"/activation", token, activation, &state, 200)
	activation.Reason = "different command"
	request("PUT", "/v1/knowledge/modules/"+m.Id+"/activation", token, activation, nil, 409)
	var listed types.ListModulesResp
	request("GET", "/v1/knowledge/modules?limit=12", "", nil, &listed, 200)
	if len(listed.Items) != 1 || listed.Items[0].Id != m.Id {
		t.Fatal(listed)
	}
	var published types.Release
	request("GET", "/v1/knowledge/modules/"+m.Id+"/published", "", nil, &published, 200)
	if published.ManifestHash != r.ManifestHash {
		t.Fatal(published)
	}
	read := types.ReadSearchSourceReq{ModuleId: m.Id, ReleaseId: r.ReleaseId, Generation: build.Generation,
		PublicationRevision: "1", RevisionId: a.RevisionId, ChunkId: chunk.ChunkId}
	request("POST", "/internal/v1/knowledge/search-sources/read", "", read, nil, 401)
	var original types.CitationChunk
	request("POST", "/internal/v1/knowledge/search-sources/read", c.WorkerToken, read, &original, 200)
	if !reflect.DeepEqual(original, chunk) {
		t.Fatalf("HTTP original differs from fixed chunk: %+v", original)
	}
	if btwRoot := os.Getenv("SEA_BTW_CITATION_CONSUMER_ROOT"); btwRoot != "" {
		// This optional local cross-repository gate lets the generated BTW
		// client consume the same actual RTW HTTP process and PG publication.
		// Ordinary RTW tests remain repository-local when the variable is unset.
		fixedIndexes := map[string]any{}
		for _, lane := range index.Lanes {
			fixedIndexes[lane.Profile.Lane] = lane.Artifact
		}
		fixtureRaw, err := json.Marshal(map[string]any{
			"base_url": base, "token": c.WorkerToken, "chunk": chunk,
			"trace_id_path": filepath.Join(dir, "btw-citation-trace-id"),
			"snapshot": map[string]any{"module_id": m.Id, "release_id": r.ReleaseId,
				"generation": build.Generation, "publication_revision": "1",
				"indexes": fixedIndexes, "valid_revision_ids": []string{a.RevisionId, w.RevisionId}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fixturePath := filepath.Join(dir, "btw-citation-fixture.json")
		if err := os.WriteFile(fixturePath, fixtureRaw, 0600); err != nil {
			t.Fatal(err)
		}
		consumer := exec.Command("go", "test", "-mod=readonly", "-race", "-count=1",
			"-run", "^TestRTWRealProviderCitationAdapter$", "-v", "./internal/app")
		consumer.Dir = btwRoot
		consumer.Env = append(os.Environ(), "SEA_RTW_REAL_CITATION_FIXTURE="+fixturePath)
		output, err := consumer.CombinedOutput()
		if err != nil {
			t.Fatalf("BTW generated client rejected real RTW source/receipt: %v\n%s", err, output)
		}
		if testing.Verbose() {
			t.Logf("BTW real RTW citation consumer: %s", strings.TrimSpace(string(output)))
		}
		traceRaw, err := os.ReadFile(filepath.Join(dir, "btw-citation-trace-id"))
		if err != nil || len(traceRaw) != 32 {
			t.Fatalf("BTW consumer did not return actual Trace ID: %q %v", traceRaw, err)
		}
		foundRead, foundAccept, foundAnswer := false, false, false
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			logs, readErr := os.ReadFile(filepath.Join(dir, "http.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, line := range bytes.Split(bytes.TrimSpace(logs), []byte("\n")) {
				var record struct {
					Event   string `json:"event"`
					TraceID string `json:"trace_id"`
				}
				if json.Unmarshal(line, &record) != nil || record.TraceID != string(traceRaw) {
					continue
				}
				foundRead = foundRead || record.Event == "knowledge.search.source.read.succeeded"
				foundAccept = foundAccept || record.Event == "knowledge.search.citations.accept.succeeded"
				foundAnswer = foundAnswer || record.Event == "knowledge.answer.accept.succeeded"
			}
			if foundRead && foundAccept && foundAnswer {
				break
			}
		}
		if !foundRead || !foundAccept || !foundAnswer {
			t.Fatalf("RTW source/citation/answer stages did not share BTW Trace ID %s: read=%v citation=%v answer=%v", traceRaw, foundRead, foundAccept, foundAnswer)
		}
		var answers int
		if err := s.DB.QueryRow(context.Background(),
			"SELECT count(*) FROM knowledge_accepted_answers WHERE answer_id='answer-btw-real-provider'").Scan(&answers); err != nil || answers != 1 {
			t.Fatalf("BTW accepted answer not exactly once in actual RTW PG: count=%d error=%v", answers, err)
		}
	}
	badRead := read
	badRead.PublicationRevision = "2"
	request("POST", "/internal/v1/knowledge/search-sources/read", c.WorkerToken, badRead, nil, 404)
	searchID := "search-http-citation"
	key := struct {
		SourceKind string `json:"source_kind"`
		ContentID  string `json:"content_id"`
		RevisionID string `json:"revision_id"`
		ChunkID    string `json:"chunk_id"`
	}{chunk.SourceKind, chunk.ContentId, chunk.RevisionId, chunk.ChunkId}
	idInput, err := json.Marshal(struct {
		SearchID string
		Key      any
		Hash     string
	}{searchID, key, chunk.TextHash})
	if err != nil {
		t.Fatal(err)
	}
	indexes := map[string]any{}
	for _, lane := range index.Lanes {
		indexes[lane.Profile.Lane] = lane.Artifact
	}
	pack := map[string]any{"search_id": searchID, "snapshot": map[string]any{
		"module_id": m.Id, "release_id": r.ReleaseId, "generation": build.Generation,
		"publication_revision": "1", "indexes": indexes, "valid_revision_ids": []string{a.RevisionId}},
		"profile": map[string]any{}, "status": "complete", "stop_reason": "", "coverage_status": "covered",
		"gaps": []string{}, "evidence": []any{map[string]any{
			"evidence_id": "ev_" + object.Hash(idInput)[:24], "key": key, "locator": chunk.Location,
			"original": chunk.Original, "quote": quote, "quote_hash": quoteHash,
			"relevance": 0.03, "sources": []any{},
		}}}
	packRaw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	accept := types.AcceptSearchCitationsReq{SearchId: searchID, PackJson: string(packRaw), PackHash: object.Hash(packRaw)}
	var citation types.SearchCitationReceipt
	request("POST", "/internal/v1/knowledge/search-citations", c.WorkerToken, accept, &citation, 200)
	if citation.SearchId != searchID || citation.PackHash != accept.PackHash || citation.DurableRef == "" {
		t.Fatalf("HTTP response lacks durable receipt: %+v", citation)
	}
	var replay types.SearchCitationReceipt
	request("POST", "/internal/v1/knowledge/search-citations", c.WorkerToken, accept, &replay, 200)
	if replay != citation {
		t.Fatalf("HTTP replay changed receipt: %+v %+v", citation, replay)
	}
	var citations types.SearchCitationRecord
	request("GET", "/internal/v1/knowledge/search-citations/"+searchID, c.WorkerToken, nil, &citations, 200)
	if citations.DurableRef != citation.DurableRef || len(citations.Evidence) != 1 ||
		citations.Evidence[0].QuoteHash != quoteHash || citations.Evidence[0].State != "available" {
		t.Fatalf("HTTP committed mapping differs: %+v", citations)
	}
	acceptedSubject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: fmt.Sprintf("%d", productUID)}
	answerID := "http-answer-1"
	acceptedTurn := map[string]any{
		"Request": map[string]any{"SearchID": searchID, "AnswerID": answerID,
			"Subject": acceptedSubject, "SessionID": "http-learning-session",
			"Search": map[string]any{"Query": "What does this source say?", "Depth": "fast",
				"Intelligence": "low", "Snapshot": pack["snapshot"]}},
		"result": map[string]any{"search": map[string]any{"evidence_pack": json.RawMessage(packRaw),
			"citation_receipt": citation}, "answer_id": answerID, "answer": "Evidence is cited.",
			"citations": []string{citations.Evidence[0].EvidenceId}, "summary_status": "succeeded"},
	}
	turnJSON, err := json.Marshal(acceptedTurn)
	if err != nil {
		t.Fatal(err)
	}
	commitAnswer := types.CommitAcceptedAnswerReq{AnswerId: answerID, SearchId: searchID,
		Subject: acceptedSubject, SessionId: "http-learning-session", TurnJson: string(turnJSON)}
	var accepted types.AcceptedAnswer
	request("POST", "/internal/v1/knowledge/accepted-answers", "", commitAnswer, nil, 401)
	request("POST", "/internal/v1/knowledge/accepted-answers", c.WorkerToken, commitAnswer, &accepted, 200)
	if accepted.Status != "succeeded" || accepted.AcceptedOrdinal != 1 || accepted.TurnJson != string(turnJSON) {
		t.Fatalf("HTTP accepted answer differs: %+v", accepted)
	}
	var acceptedReplay types.AcceptedAnswer
	request("POST", "/internal/v1/knowledge/accepted-answers", c.WorkerToken, commitAnswer, &acceptedReplay, 200)
	if acceptedReplay != accepted {
		t.Fatalf("HTTP answer replay changed projection: %+v %+v", accepted, acceptedReplay)
	}
	runGroundingReviewHTTP(t, request, base, token, nonAdminToken, c.WorkerToken, dir,
		searchID, answerID, "Evidence is cited.", citations.Evidence[0].EvidenceId,
		quote, quoteHash, citation.DurableRef)
	answerQuery := url.Values{"authority_id": {acceptedSubject.AuthorityId}, "tenant_id": {acceptedSubject.TenantId},
		"subject_id": {acceptedSubject.SubjectId}, "session_id": {commitAnswer.SessionId}}
	request("GET", "/internal/v1/knowledge/accepted-answers/"+answerID+"?"+answerQuery.Encode(),
		c.WorkerToken, nil, &acceptedReplay, 200)
	if acceptedReplay != accepted {
		t.Fatalf("HTTP uncertain commit recovery differs: %+v %+v", accepted, acceptedReplay)
	}
	var history types.AcceptedAnswersPage
	request("GET", "/internal/v1/knowledge/accepted-answers?"+answerQuery.Encode(), c.WorkerToken, nil, &history, 200)
	if len(history.Items) != 1 || history.Items[0] != accepted {
		t.Fatalf("HTTP product history missing accepted answer: %+v", history)
	}
	var mismatchToken string
	if !realUser {
		productToken, err = rtwjwt.GetToken(c.UserAuth.AccessSecret, time.Now().Unix(), 3600, productUID)
		if err != nil {
			t.Fatal(err)
		}
		otherToken, err = rtwjwt.GetToken(c.UserAuth.AccessSecret, time.Now().Unix(), 3600, otherUID)
		if err != nil {
			t.Fatal(err)
		}
		mismatchToken, err = rtwjwt.GetToken(c.UserAuth.AccessSecret, time.Now().Unix(), 3600, 8888)
		if err != nil {
			t.Fatal(err)
		}
	}
	toolPath := "/v1/knowledge/answer-sessions/tool-parent-session/tool-runs"
	toolParentBody := map[string]any{"module_id": m.Id, "idempotency_key": "tool-parent-key-1"}
	request("POST", toolPath, "", toolParentBody, nil, 401)
	request("POST", toolPath, productToken, map[string]any{"module_id": m.Id,
		"idempotency_key": "tool-parent-key-1", "subject_ref": acceptedSubject}, nil, 400)
	var toolParent types.ToolParentResult
	request("POST", toolPath, productToken, toolParentBody, &toolParent, 200)
	if toolParent.OperationId == "" || toolParent.ScopeRef == "" || toolParent.SnapshotRef == "" ||
		toolParent.BudgetRef == "" || toolParent.ModuleId != m.Id ||
		toolParent.Budget.SearchCalls != 4 || toolParent.Budget.ReadCalls != 24 ||
		toolParent.Budget.QuoteRunes != 32768 || toolParent.DeadlineAtMs <= time.Now().UnixMilli() {
		t.Fatalf("authenticated Tool parent did not pin server scope/budget: %+v", toolParent)
	}
	parentPath := toolPath + "/" + toolParent.OperationId
	var replayedParent types.ToolParentResult
	request("POST", toolPath, productToken, toolParentBody, &replayedParent, 200)
	if !reflect.DeepEqual(replayedParent, toolParent) {
		t.Fatal("Tool parent idempotency changed fixed scope")
	}
	request("GET", parentPath, otherToken, nil, nil, 404)
	request("GET", parentPath, productToken, nil, &replayedParent, 200)
	if !reflect.DeepEqual(replayedParent, toolParent) {
		t.Fatal("Tool parent GET changed fixed scope")
	}
	toolSearchPath := parentPath + "/searches"
	toolSearchBody := map[string]any{"query": "Find the current evidence", "depth": "fast",
		"intelligence": "low", "read_calls": 8, "quote_runes": 8192,
		"idempotency_key": "tool-search-key-1"}
	request("POST", toolSearchPath, productToken, map[string]any{"query": "Find the current evidence",
		"depth": "fast", "intelligence": "low", "read_calls": 8, "quote_runes": 8192,
		"idempotency_key": "tool-search-key-1", "snapshot_ref": "forged"}, nil, 400)
	toolFixture.stage.Store(1)
	var toolSearch types.ToolSearchResult
	request("POST", toolSearchPath, productToken, toolSearchBody, &toolSearch, 503)
	if toolSearch.SearchId == "" || toolSearch.Status != "retryable_failure" || len(toolSearch.Evidence) != 0 ||
		toolFixture.calls.Load() != 1 {
		t.Fatalf("forged Tool quote leaked: %+v calls=%d", toolSearch, toolFixture.calls.Load())
	}
	toolFixture.stage.Store(0)
	request("POST", toolSearchPath, productToken, toolSearchBody, &toolSearch, 200)
	if toolSearch.Status != "empty" || toolSearch.Evidence == nil || len(toolSearch.Evidence) != 0 ||
		toolSearch.CitationReceipt != nil || toolSearch.PackHash != "" || toolFixture.calls.Load() != 2 {
		t.Fatalf("empty Tool search acquired fake evidence or receipt: %+v calls=%d", toolSearch, toolFixture.calls.Load())
	}
	var toolSearchReplay types.ToolSearchResult
	request("POST", toolSearchPath, productToken, toolSearchBody, &toolSearchReplay, 200)
	if !reflect.DeepEqual(toolSearchReplay, toolSearch) || toolFixture.calls.Load() != 2 {
		t.Fatal("completed Tool replay dispatched BTW again")
	}
	request("GET", toolSearchPath+"/"+toolSearch.SearchId, productToken, nil, &toolSearchReplay, 200)
	if !reflect.DeepEqual(toolSearchReplay, toolSearch) {
		t.Fatal("Tool GET changed completed result")
	}
	request("GET", toolSearchPath+"/"+toolSearch.SearchId, otherToken, nil, nil, 404)
	request("POST", toolSearchPath, productToken, map[string]any{"query": "changed",
		"depth": "fast", "intelligence": "low", "read_calls": 8, "quote_runes": 8192,
		"idempotency_key": "tool-search-key-1"}, nil, 409)
	request("POST", parentPath+"/evidence-reads", productToken,
		map[string]any{"search_id": toolSearch.SearchId, "evidence_id": "ev_missing",
			"idempotency_key": "tool-read-key-1"}, nil, 404)
	request("GET", parentPath, productToken, nil, &replayedParent, 200)
	if replayedParent.Budget.SearchCalls != 3 || replayedParent.Budget.ReadCalls != 24 ||
		replayedParent.Budget.QuoteRunes != 32768 {
		t.Fatalf("Tool budget did not reserve once/refund verified empty result: %+v", replayedParent.Budget)
	}
	if btwRoot := os.Getenv("SEA_BTW_TOOLS_CONSUMER_ROOT"); btwRoot != "" {
		endpoint := startRealBTWToolsServer(t, dir, btwRoot, base, c.WorkerToken, m.Id, nil)
		toolFixture.mu.Lock()
		toolFixture.forwardURL = endpoint
		toolFixture.mu.Unlock()
		toolFixture.stage.Store(2)
		realBody := map[string]any{"query": "Find the current evidence", "depth": "fast",
			"intelligence": "low", "read_calls": 8, "quote_runes": 8192,
			"idempotency_key": "tool-search-real-btw-1"}
		callsBefore := toolFixture.calls.Load()
		var realResult types.ToolSearchResult
		request("POST", toolSearchPath, productToken, realBody, &realResult, 200)
		if realResult.SearchId == "" || realResult.Status != "empty" ||
			realResult.SnapshotRef != toolParent.SnapshotRef || len(realResult.Evidence) != 0 ||
			realResult.CitationReceipt != nil || realResult.PackHash != "" ||
			toolFixture.calls.Load() != callsBefore+1 {
			t.Fatalf("real BTW Tool search escaped fixed empty scope: %+v calls=%d",
				realResult, toolFixture.calls.Load())
		}
		var citationCount, childCount int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_search_citations
 WHERE search_id=$1`, realResult.SearchId).Scan(&citationCount); err != nil || citationCount != 0 {
			t.Fatalf("real BTW empty Tool invented RTW citation: count=%d err=%v", citationCount, err)
		}
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_tool_searches
 WHERE operation_id=$1 AND search_id=$2 AND status='complete'`, toolParent.OperationId,
			realResult.SearchId).Scan(&childCount); err != nil || childCount != 1 {
			t.Fatalf("real BTW Tool child not durably completed: count=%d err=%v", childCount, err)
		}
		var realReplay types.ToolSearchResult
		request("POST", toolSearchPath, productToken, realBody, &realReplay, 200)
		if !reflect.DeepEqual(realReplay, realResult) || toolFixture.calls.Load() != callsBefore+1 {
			t.Fatal("real BTW Tool replay reran search or changed evidence")
		}
		request("GET", toolSearchPath+"/"+realResult.SearchId, productToken, nil, &realReplay, 200)
		if !reflect.DeepEqual(realReplay, realResult) {
			t.Fatal("real BTW Tool fixed GET disagrees with POST")
		}
		request("GET", parentPath, productToken, nil, &replayedParent, 200)
		if replayedParent.Budget.SearchCalls != 2 || replayedParent.Budget.ReadCalls != 24 ||
			replayedParent.Budget.QuoteRunes != 32768 {
			t.Fatalf("real BTW Tool budget did not commit once: %+v", replayedParent.Budget)
		}
		beforeCited := replayedParent.Budget
		citedEndpoint := startRealBTWToolsServer(t, t.TempDir(), btwRoot, base, c.WorkerToken, m.Id, &chunk)
		toolFixture.mu.Lock()
		toolFixture.forwardURL = citedEndpoint
		toolFixture.mu.Unlock()
		citedBody := map[string]any{"query": "Find the current evidence", "depth": "fast",
			"intelligence": "low", "read_calls": 8, "quote_runes": 8192,
			"idempotency_key": "tool-search-real-btw-cited-1"}
		callsBefore = toolFixture.calls.Load()
		var citedResult types.ToolSearchResult
		request("POST", toolSearchPath, productToken, citedBody, &citedResult, 200)
		if citedResult.SearchId == "" || citedResult.Status != "complete" ||
			citedResult.SnapshotRef != toolParent.SnapshotRef || len(citedResult.Evidence) != 1 ||
			citedResult.Evidence[0].RevisionId != chunk.RevisionId ||
			citedResult.Evidence[0].SourceKind != chunk.SourceKind ||
			citedResult.Evidence[0].Quote != quote || citedResult.Evidence[0].QuoteHash != quoteHash ||
			citedResult.PackHash == "" || citedResult.CitationReceipt == nil ||
			citedResult.CitationReceipt.SearchId != citedResult.SearchId ||
			citedResult.CitationReceipt.PackHash != citedResult.PackHash ||
			citedResult.CitationReceipt.DurableRef == "" ||
			citedResult.Usage.ReadCalls != 1 || citedResult.Usage.QuoteRunes != utf8.RuneCountInString(quote) ||
			toolFixture.calls.Load() != callsBefore+1 {
			t.Fatalf("real BTW Tool cited evidence or usage invalid: %+v calls=%d", citedResult,
				toolFixture.calls.Load())
		}
		durable, err := s.GetSearchCitations(context.Background(), citedResult.SearchId)
		if err != nil || durable.PackHash != citedResult.PackHash ||
			durable.DurableRef != citedResult.CitationReceipt.DurableRef || len(durable.Evidence) != 1 ||
			durable.Evidence[0].EvidenceId != citedResult.Evidence[0].EvidenceId ||
			durable.Evidence[0].QuoteHash != quoteHash {
			t.Fatalf("real BTW Tool citation was not durably accepted: %+v err=%v", durable, err)
		}
		var citedReplay types.ToolSearchResult
		request("POST", toolSearchPath, productToken, citedBody, &citedReplay, 200)
		if !reflect.DeepEqual(citedReplay, citedResult) || toolFixture.calls.Load() != callsBefore+1 {
			t.Fatal("real BTW cited Tool replay reran search or changed evidence")
		}
		request("GET", toolSearchPath+"/"+citedResult.SearchId, productToken, nil, &citedReplay, 200)
		if !reflect.DeepEqual(citedReplay, citedResult) {
			t.Fatal("real BTW cited Tool GET disagrees with POST")
		}
		request("GET", toolSearchPath+"/"+citedResult.SearchId, otherToken, nil, nil, 404)
		request("GET", parentPath, productToken, nil, &replayedParent, 200)
		if replayedParent.Budget.SearchCalls != beforeCited.SearchCalls-1 ||
			replayedParent.Budget.ReadCalls != beforeCited.ReadCalls-1 ||
			replayedParent.Budget.QuoteRunes != beforeCited.QuoteRunes-utf8.RuneCountInString(quote) {
			t.Fatalf("real BTW cited Tool did not refund unused reservation once: before=%+v after=%+v",
				beforeCited, replayedParent.Budget)
		}
		readBody := map[string]any{"search_id": citedResult.SearchId,
			"evidence_id": citedResult.Evidence[0].EvidenceId, "idempotency_key": "tool-read-real-btw-cited-1"}
		var reread types.ToolReadResult
		request("POST", parentPath+"/evidence-reads", productToken, readBody, &reread, 200)
		if reread.Evidence.Quote != quote || reread.Evidence.QuoteHash != quoteHash ||
			reread.CitationReceipt.DurableRef != citedResult.CitationReceipt.DurableRef ||
			reread.SnapshotRef != toolParent.SnapshotRef {
			t.Fatalf("RTW Tool product reread differs from accepted evidence: %+v", reread)
		}
		var rereadReplay types.ToolReadResult
		request("POST", parentPath+"/evidence-reads", productToken, readBody, &rereadReplay, 200)
		if !reflect.DeepEqual(rereadReplay, reread) {
			t.Fatal("RTW Tool product reread changed on same idempotency key")
		}
		request("GET", parentPath, productToken, nil, &replayedParent, 200)
		if replayedParent.Budget.ReadCalls != beforeCited.ReadCalls-2 ||
			replayedParent.Budget.QuoteRunes != beforeCited.QuoteRunes-2*utf8.RuneCountInString(quote) {
			t.Fatalf("RTW Tool reread did not charge once: before=%+v after=%+v",
				beforeCited, replayedParent.Budget)
		}
	}
	searchPath := "/v1/knowledge/answer-sessions/search-facade-session/searches"
	searchBody := map[string]any{"module_id": m.Id, "query": "What does this book say?",
		"depth": "fast", "intelligence": "low", "idempotency_key": "product-search-key-1"}
	request("POST", searchPath, "", searchBody, nil, 401)
	request("POST", searchPath, productToken, map[string]any{"module_id": m.Id,
		"query": "hello", "depth": "fast", "intelligence": "low", "idempotency_key": "bad\nkey"}, nil, 400)
	request("POST", searchPath, productToken, map[string]any{"module_id": m.Id,
		"query": "hello", "depth": "fast", "intelligence": "low", "idempotency_key": "short"}, nil, 400)
	request("POST", searchPath, productToken, map[string]any{"module_id": m.Id,
		"query": "hello", "depth": "fast", "intelligence": "low", "idempotency_key": "product-unexpected", "subject_ref": acceptedSubject}, nil, 400)
	request("GET", searchPath+"/search_missing", productToken, nil, nil, 404)
	var productSearch types.ProductSearchResult
	request("POST", searchPath, productToken, searchBody, &productSearch, 503)
	if productSearch.Status != "retryable_failure" || productSearch.SearchId == "" || productSearch.AnswerId == "" ||
		searchFixture.calls.Load() != 1 {
		t.Fatalf("BTW forged 200 leaked an unaccepted product answer: %+v calls=%d", productSearch, searchFixture.calls.Load())
	}
	productSearchID, answerIDForSearch := productSearch.SearchId, productSearch.AnswerId
	var productStatus types.ProductSearchResult
	request("GET", searchPath+"/"+productSearchID, productToken, nil, &productStatus, 200)
	if productStatus.Status != "retryable_failure" || productStatus.AnswerId != answerIDForSearch {
		t.Fatalf("failed product search lost its stable operation identity: %+v", productStatus)
	}
	var emptyProductHistory types.AcceptedAnswersPage
	request("GET", "/v1/knowledge/answer-sessions/search-facade-session/accepted-answers",
		productToken, nil, &emptyProductHistory, 200)
	if len(emptyProductHistory.Items) != 0 {
		t.Fatalf("forged BTW 200 entered accepted answer history: %+v", emptyProductHistory)
	}
	searchFixture.stage.Store(1) // BTW commits via real RTW private HTTP, then returns 502.
	request("POST", searchPath, productToken, searchBody, &productSearch, 200)
	if productSearch.Status != "insufficient" || productSearch.SearchId != productSearchID ||
		productSearch.AnswerId != answerIDForSearch || len(productSearch.Citations) != 0 ||
		searchFixture.calls.Load() != 2 {
		t.Fatalf("committed answer was not recovered after lost BTW reply: %+v calls=%d", productSearch, searchFixture.calls.Load())
	}
	request("GET", searchPath+"/"+productSearchID, productToken, nil, &productStatus, 200)
	if !reflect.DeepEqual(productStatus, productSearch) {
		t.Fatalf("operation GET disagrees with recovered accepted answer: %+v %+v", productStatus, productSearch)
	}
	judgmentPath := "/v1/knowledge/modules/" + m.Id + "/search-judgments"
	judgmentInput := types.RecordSearchJudgmentReq{SearchId: productSearchID,
		ContentRevisionId: a.RevisionId, ChunkId: chunk.ChunkId, Grade: "2",
		RubricVersion: "sea.search.relevance.v1", Reason: "synthetic HTTP test judge",
		IdempotencyKey: "http-qrel-judgment-key"}
	request("POST", judgmentPath, "", judgmentInput, nil, 401)
	request("POST", judgmentPath, nonAdminToken, judgmentInput, nil, 403)
	var judgment types.SearchJudgmentReceipt
	request("POST", judgmentPath, token, judgmentInput, &judgment, 200)
	if judgment.SearchId != productSearchID || judgment.ChunkId != chunk.ChunkId || judgment.EventSha256 == "" {
		t.Fatalf("admin judgment did not capture fixed search and chunk: %+v", judgment)
	}
	var judgmentEvent types.SearchJudgmentEventReceipt
	eventPath := "/internal/v1/knowledge/search-judgments/events/" + judgment.EventId
	request("GET", eventPath, "", nil, nil, 401)
	request("GET", eventPath, c.WorkerToken, nil, &judgmentEvent, 200)
	if judgmentEvent.EventId != judgment.EventId || object.Hash([]byte(judgmentEvent.EventJson)) != judgment.EventSha256 {
		t.Fatalf("worker event lookup changed emitted bytes: %+v", judgmentEvent)
	}
	withdrawJudgment := types.WithdrawSearchJudgmentReq{SearchId: productSearchID,
		ChunkId: chunk.ChunkId, BaseRevisionId: judgment.RevisionId,
		Reason: "synthetic HTTP test retraction", IdempotencyKey: "http-qrel-withdraw-key"}
	var withdrawnJudgment types.SearchJudgmentReceipt
	request("POST", judgmentPath+"/withdrawals", token, withdrawJudgment, &withdrawnJudgment, 200)
	if withdrawnJudgment.State != "withdrawn" || withdrawnJudgment.JudgmentId != judgment.JudgmentId {
		t.Fatalf("admin withdrawal did not append revision: %+v", withdrawnJudgment)
	}
	runRealSearchSourceHandoff(t, dir, s, base, c.WorkerToken, judgment, withdrawnJudgment)
	request("POST", searchPath, productToken, searchBody, &productStatus, 200)
	if !reflect.DeepEqual(productStatus, productSearch) || searchFixture.calls.Load() != 2 {
		t.Fatal("same-key replay reran BTW or changed accepted answer")
	}
	request("GET", searchPath+"/"+productSearchID, otherToken, nil, nil, 404)
	changedSearchBody := map[string]any{"module_id": m.Id, "query": "a different question",
		"depth": "fast", "intelligence": "low", "idempotency_key": "product-search-key-1"}
	request("POST", searchPath, productToken, changedSearchBody, nil, 409)
	searchFixture.stage.Store(2) // Normal BTW success only after its real RTW commit.
	newSearchBody := map[string]any{"module_id": m.Id, "query": "A second question",
		"depth": "detailed", "intelligence": "medium", "idempotency_key": "product-search-key-2"}
	request("POST", searchPath, productToken, newSearchBody, &productStatus, 200)
	if productStatus.Status != "insufficient" || productStatus.SearchId == productSearchID || searchFixture.calls.Load() != 3 {
		t.Fatalf("normal committed BTW response did not verify: %+v calls=%d", productStatus, searchFixture.calls.Load())
	}
	searchFixture.stage.Store(3)
	inFlightBody := map[string]any{"module_id": m.Id, "query": "A held search",
		"depth": "fast", "intelligence": "high", "idempotency_key": "product-search-key-3"}
	inFlightRaw, err := json.Marshal(inFlightBody)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		req, reqErr := http.NewRequest(http.MethodPost, base+searchPath, bytes.NewReader(inFlightRaw))
		if reqErr != nil {
			firstDone <- reqErr
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+productToken)
		response, reqErr := client.Do(req)
		if reqErr != nil {
			firstDone <- reqErr
			return
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 503 {
			firstDone <- fmt.Errorf("held POST status=%d, want 503", response.StatusCode)
			return
		}
		firstDone <- nil
	}()
	select {
	case <-searchFixture.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first product operation did not reach BTW")
	}
	searchFixture.mu.Lock()
	heldScope := searchFixture.scopes[len(searchFixture.scopes)-1]
	searchFixture.mu.Unlock()
	request("GET", searchPath+"/"+heldScope.SearchID, productToken, nil, &productStatus, 202)
	if productStatus.Status != "in_flight" || productStatus.AnswerId != heldScope.AnswerID {
		t.Fatalf("running operation GET lost stable identity: %+v", productStatus)
	}
	request("POST", searchPath, productToken, inFlightBody, &productStatus, 202)
	if searchFixture.calls.Load() != 4 {
		t.Fatal("duplicate operation bypassed active lease and called BTW")
	}
	close(searchFixture.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	request("GET", searchPath+"/"+heldScope.SearchID, productToken, nil, &productStatus, 200)
	if productStatus.Status != "retryable_failure" {
		t.Fatalf("failed held operation is not queryable: %+v", productStatus)
	}
	if btwRoot := os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT"); btwRoot != "" {
		// Stage four is only a byte-preserving relay. The independent BTW test
		// process verifies RTW's scope and commits through its real RootSessionBoundary.
		endpoint := startRealBTWProductServer(t, dir, btwRoot, base, c.WorkerToken, m.Id, nil, nil)
		searchFixture.mu.Lock()
		searchFixture.forwardURL = endpoint
		searchFixture.mu.Unlock()
		searchFixture.stage.Store(4)
		realBody := map[string]any{"module_id": m.Id, "query": "What if this release has no matching evidence?",
			"depth": "fast", "intelligence": "low", "idempotency_key": "product-search-real-btw-1"}
		var realResult types.ProductSearchResult
		request("POST", searchPath, productToken, realBody, &realResult, 200)
		if realResult.Status != "insufficient" || realResult.SearchId == "" || realResult.AnswerId == "" ||
			len(realResult.Citations) != 0 || searchFixture.calls.Load() != 5 {
			t.Fatalf("real BTW signed root did not commit the product operation: %+v calls=%d",
				realResult, searchFixture.calls.Load())
		}
		if testing.Verbose() {
			t.Logf("RTW signed scope reached real BTW RootSessionBoundary: search=%s answer=%s status=%s",
				realResult.SearchId, realResult.AnswerId, realResult.Status)
		}
		var committedCount, citationCount int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_accepted_answers
 WHERE answer_id=$1 AND search_id=$2 AND authority_id='rtw.identity' AND tenant_id='platform'
 AND subject_id=$3 AND session_id='search-facade-session'`,
			realResult.AnswerId, realResult.SearchId, fmt.Sprintf("%d", productUID)).Scan(&committedCount); err != nil || committedCount != 1 {
			t.Fatalf("real BTW did not commit one RTW answer in PG: count=%d err=%v", committedCount, err)
		}
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_search_citations WHERE search_id=$1`,
			realResult.SearchId).Scan(&citationCount); err != nil || citationCount != 0 {
			t.Fatalf("empty evidence product search fabricated citation: count=%d err=%v", citationCount, err)
		}
		var replayedReal types.ProductSearchResult
		request("POST", searchPath, productToken, realBody, &replayedReal, 200)
		if !reflect.DeepEqual(replayedReal, realResult) || searchFixture.calls.Load() != 5 {
			t.Fatalf("real BTW committed operation replay changed answer: %+v calls=%d",
				replayedReal, searchFixture.calls.Load())
		}
		request("GET", searchPath+"/"+realResult.SearchId, productToken, nil, &replayedReal, 200)
		if !reflect.DeepEqual(replayedReal, realResult) {
			t.Fatalf("real BTW answer and RTW operation GET differ: %+v", replayedReal)
		}
		// The structural path starts a second process with a published chunk
		// identity. The real-index path reuses the process that built the three
		// immutable lanes before RTW accepted and published their refs.
		if realSearchEndpoint != "" {
			endpoint = realSearchEndpoint
		} else {
			endpoint = startRealBTWProductServer(t, dir, btwRoot, base, c.WorkerToken, m.Id, &chunk, nil)
		}
		searchFixture.mu.Lock()
		searchFixture.forwardURL = endpoint
		searchFixture.mu.Unlock()
		citedBody := map[string]any{"module_id": m.Id, "query": "What does Book A say?",
			"depth": "fast", "intelligence": "low", "idempotency_key": "product-search-real-btw-cited-1"}
		if realSearchEndpoint != "" {
			citedBody["query"] = quote // exact self-query requires all three real BGE lanes to hit.
		}
		callsBeforeCited := searchFixture.calls.Load()
		var cited types.ProductSearchResult
		citedStarted := time.Now()
		webSearch := false
		if realUser {
			cited, webSearch = waitWebProductSearch(t, realUsers, base, m.Id,
				"search-facade-session", citedBody["query"].(string),
				citedBody["idempotency_key"].(string))
		}
		if !webSearch {
			request("POST", searchPath, productToken, citedBody, &cited, 200)
		}
		citedLatency := time.Since(citedStarted)
		answerValid := cited.Answer == "The published source states: "+quote
		if formalAPI != nil && formalAPI.liveGateway {
			answerValid = strings.TrimSpace(cited.Answer) != "" && strings.Contains(strings.ToLower(cited.Answer), "evidence")
		}
		if cited.Status != "succeeded" || cited.SearchId == "" || cited.AnswerId == "" ||
			!answerValid || len(cited.Citations) != 1 ||
			cited.Citations[0].Quote != quote || cited.Citations[0].QuoteHash != quoteHash ||
			cited.Citations[0].RevisionId != a.RevisionId || cited.Citations[0].ContentId != a.EntityId ||
			cited.CitationReceiptRef == "" || searchFixture.calls.Load() != callsBeforeCited+1 {
			t.Fatalf("real BTW cited product result is not RTW accepted evidence: %+v calls=%d",
				cited, searchFixture.calls.Load())
		}
		if formalAPI != nil {
			formalAPI.AssertServed(t)
		}
		if testing.Verbose() {
			t.Logf("RTW signed cited search accepted by real BTW root: search=%s answer=%s receipt=%s evidence=%s",
				cited.SearchId, cited.AnswerId, cited.CitationReceiptRef, cited.Citations[0].EvidenceId)
		}
		var acceptedCount, acceptedCitations, citationRecords int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_accepted_answers
 WHERE answer_id=$1 AND search_id=$2 AND authority_id='rtw.identity' AND tenant_id='platform'
 AND subject_id=$3 AND session_id='search-facade-session' AND status='succeeded'`,
			cited.AnswerId, cited.SearchId, fmt.Sprintf("%d", productUID)).Scan(&acceptedCount); err != nil || acceptedCount != 1 {
			t.Fatalf("real cited BTW answer missing from RTW PG: count=%d err=%v", acceptedCount, err)
		}
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_answer_citations WHERE answer_id=$1`,
			cited.AnswerId).Scan(&acceptedCitations); err != nil || acceptedCitations != 1 {
			t.Fatalf("real cited BTW answer has no accepted citation in RTW PG: count=%d err=%v", acceptedCitations, err)
		}
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_search_citations WHERE search_id=$1`,
			cited.SearchId).Scan(&citationRecords); err != nil || citationRecords != 1 {
			t.Fatalf("real cited BTW search has no durable RTW receipt: count=%d err=%v", citationRecords, err)
		}
		var citedReplay types.ProductSearchResult
		if webSearch {
			var browserIdempotencyKey string
			if err := s.DB.QueryRow(context.Background(), `SELECT operation_key FROM knowledge_product_search_operations
 WHERE search_id=$1 AND authority_id='rtw.identity' AND tenant_id='platform'
 AND subject_id=$2 AND session_id='search-facade-session'`, cited.SearchId,
				fmt.Sprintf("%d", productUID)).Scan(&browserIdempotencyKey); err != nil || browserIdempotencyKey == "" {
				t.Fatalf("browser-created search did not retain its RTW idempotency authority: key=%q err=%v",
					browserIdempotencyKey, err)
			}
			citedBody["idempotency_key"] = browserIdempotencyKey
		}
		request("POST", searchPath, productToken, citedBody, &citedReplay, 200)
		if !reflect.DeepEqual(citedReplay, cited) || searchFixture.calls.Load() != callsBeforeCited+1 {
			t.Fatalf("real cited BTW idempotent replay changed answer: %+v calls=%d", citedReplay, searchFixture.calls.Load())
		}
		request("GET", searchPath+"/"+cited.SearchId, productToken, nil, &citedReplay, 200)
		if !reflect.DeepEqual(citedReplay, cited) {
			t.Fatalf("real cited BTW answer and RTW operation GET differ: %+v", citedReplay)
		}
		request("GET", searchPath+"/"+cited.SearchId, otherToken, nil, nil, 404)
		var citedState types.ProductAnswerCitationStates
		request("GET", "/v1/knowledge/answer-sessions/search-facade-session/accepted-answers/"+
			cited.AnswerId+"/citations", productToken, nil, &citedState, 200)
		if citedState.Status != "succeeded" || len(citedState.Citations) != 1 ||
			citedState.Citations[0].State != "available" ||
			citedState.Citations[0].EvidenceId != cited.Citations[0].EvidenceId {
			t.Fatalf("real cited BTW answer is not available in RTW product citation state: %+v", citedState)
		}
		if formalAPI != nil && formalAPI.liveGateway {
			var history types.AcceptedAnswersPage
			request("GET", "/v1/knowledge/answer-sessions/search-facade-session/accepted-answers?limit=50",
				productToken, nil, &history, 200)
			found := false
			for _, item := range history.Items {
				if item.AnswerId == cited.AnswerId && item.SearchId == cited.SearchId && item.Status == "succeeded" {
					found = true
				}
			}
			if !found {
				t.Fatal("live DataCenter summary absent from RTW user product history")
			}
			if outputDir := os.Getenv("SEA_BTW_SUMMARY_RESULT_DIR"); outputDir != "" {
				info, err := os.Stat(outputDir)
				if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
					t.Fatal("live summary result directory must be private")
				}
				report := map[string]any{"schema_version": "sea.search.live-summary-rtw.v1",
					"delivery_execution": "observed", "depth": "fast", "intelligence": "low",
					"delivery": "summary", "search_id": cited.SearchId, "answer_id": cited.AnswerId,
					"subject_authority_id": "rtw.identity", "subject_tenant_id": "platform",
					"subject_id": fmt.Sprintf("%d", productUID), "session_id": "search-facade-session",
					"answer": cited.Answer, "citation_receipt_ref": cited.CitationReceiptRef,
					"evidence_id": cited.Citations[0].EvidenceId,
					"quote":       cited.Citations[0].Quote, "quote_hash": cited.Citations[0].QuoteHash,
					"rtw_answer_rows": acceptedCount, "rtw_answer_citation_rows": acceptedCitations,
					"rtw_search_citation_rows":    citationRecords,
					"rtw_product_history_present": true, "real_user_center": realUser,
					"end_to_end_latency_ms": citedLatency.Milliseconds(),
					"model_gateway":         "datacenter-local-ollama", "model_usage": nil,
					"qrel_complete": false}
				body, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(filepath.Join(outputDir, "summary-"+cited.SearchId+".json"),
					os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write(append(body, '\n')); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	productPath := "/v1/knowledge/answer-sessions/" + commitAnswer.SessionId + "/accepted-answers"
	citationStatePath := productPath + "/" + answerID + "/citations"
	var beforeUnauthorized int64
	if userRPC != nil {
		beforeUnauthorized = userRPC.calls.Load()
	}
	request("GET", productPath, "", nil, nil, 401)
	request("GET", productPath, c.WorkerToken, nil, nil, 401)
	request("GET", productPath, token, nil, nil, 401) // Administrator JWT has a different issuer secret.
	request("GET", citationStatePath, "", nil, nil, 401)
	if userRPC != nil && userRPC.calls.Load() != beforeUnauthorized {
		t.Fatal("unauthorized product request reached User RPC")
	}
	var ownHistory types.AcceptedAnswersPage
	request("GET", productPath+"?authority_id=forged&tenant_id=forged&subject_id="+fmt.Sprintf("%d", otherUID), productToken, nil, &ownHistory, 200)
	if len(ownHistory.Items) != 1 || ownHistory.Items[0] != accepted {
		t.Fatalf("client subject fields changed own history: %+v", ownHistory)
	}
	if !realUser {
		userRPC.missingStatus.Store(true)
		request("GET", productPath, productToken, nil, nil, 503)
		request("GET", parentPath, productToken, nil, nil, 503)
		userRPC.missingStatus.Store(false)
	}
	var ownAnswer types.AcceptedAnswer
	request("GET", productPath+"/"+answerID, productToken, nil, &ownAnswer, 200)
	if ownAnswer != accepted {
		t.Fatalf("product AnswerID lookup differs: %+v", ownAnswer)
	}
	readProductBytes := func(path, bearer string) []byte {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("read %s status=%d err=%v body=%s", path, res.StatusCode, err, raw)
		}
		return raw
	}
	v1AnswerWireBefore := readProductBytes(productPath+"/"+answerID, productToken)
	v2ProductPath := "/v2/knowledge/answer-sessions/" + commitAnswer.SessionId + "/accepted-answers"
	v2CitationPath := v2ProductPath + "/" + answerID + "/citations"
	request("GET", v2ProductPath, "", nil, nil, 401)
	request("GET", v2ProductPath, otherToken, nil, &types.AcceptedAnswersPageV2{}, 200)
	var v2History types.AcceptedAnswersPageV2
	request("GET", v2ProductPath+"?authority_id=forged&tenant_id=forged&subject_id="+
		fmt.Sprintf("%d", otherUID), productToken, nil, &v2History, 200)
	if len(v2History.Items) != 1 || v2History.Items[0].Subject != (types.AcceptedSubjectRefV2{
		Issuer: "rtw.identity", SubjectId: fmt.Sprintf("%d", productUID)}) ||
		v2History.Items[0].TurnJson != accepted.TurnJson || v2History.Items[0].AcceptedOrdinal != accepted.AcceptedOrdinal {
		t.Fatalf("v2 historical projection differs from immutable v1 answer: %+v", v2History)
	}
	var v2Answer types.AcceptedAnswerV2
	request("GET", v2ProductPath+"/"+answerID, productToken, nil, &v2Answer, 200)
	if v2Answer != v2History.Items[0] || object.Hash([]byte(v2Answer.TurnJson)) != object.Hash([]byte(ownAnswer.TurnJson)) {
		t.Fatalf("v2 detail changed old turn bytes or identity: %+v %+v", v2Answer, ownAnswer)
	}
	var v2DetailWire struct {
		Data struct {
			Subject map[string]json.RawMessage `json:"subject"`
		} `json:"data"`
	}
	if err := json.Unmarshal(readProductBytes(v2ProductPath+"/"+answerID, productToken), &v2DetailWire); err != nil ||
		len(v2DetailWire.Data.Subject) != 2 || v2DetailWire.Data.Subject["issuer"] == nil ||
		v2DetailWire.Data.Subject["subject_id"] == nil {
		t.Fatalf("v2 product wire exposed legacy tenant/realm/authority: %+v err=%v", v2DetailWire, err)
	}
	if !bytes.Equal(v1AnswerWireBefore, readProductBytes(productPath+"/"+answerID, productToken)) {
		t.Fatal("v2 read changed the old v1 GET response bytes")
	}
	var originalTurnHash, originalTurn string
	if err := s.DB.QueryRow(context.Background(), `SELECT turn_hash,turn_json FROM knowledge_accepted_answers WHERE answer_id=$1`,
		answerID).Scan(&originalTurnHash, &originalTurn); err != nil || originalTurnHash != object.Hash([]byte(originalTurn)) ||
		originalTurn != v2Answer.TurnJson {
		t.Fatalf("PG turn hash/bytes differ from v1 and v2: hash=%q err=%v", originalTurnHash, err)
	}
	request("GET", v2ProductPath+"/"+answerID, otherToken, nil, nil, 404)
	request("GET", v2CitationPath, otherToken, nil, nil, 404)
	var v2States types.ProductAnswerCitationStates
	request("GET", v2CitationPath, productToken, nil, &v2States, 200)
	if v2States.AnswerId != answerID || len(v2States.Citations) != 1 ||
		v2States.Citations[0].QuoteHash != quoteHash || v2States.Citations[0].State != "available" {
		t.Fatalf("v2 citation did not preserve fixed quote and current state: %+v", v2States)
	}
	var citedStates types.ProductAnswerCitationStates
	request("GET", citationStatePath, productToken, nil, &citedStates, 200)
	if citedStates.AnswerId != answerID || citedStates.SearchId != searchID ||
		len(citedStates.Citations) != 1 || citedStates.Citations[0].State != "available" ||
		citedStates.Citations[0].EvidenceId != citations.Evidence[0].EvidenceId {
		t.Fatalf("product citation state differs from accepted answer: %+v", citedStates)
	}
	request("GET", productPath+"/"+answerID, otherToken, nil, nil, 404)
	request("GET", citationStatePath, otherToken, nil, nil, 404)
	if !realUser {
		request("GET", productPath+"/"+answerID, mismatchToken, nil, nil, 403)
	}
	var otherHistory types.AcceptedAnswersPage
	request("GET", productPath, otherToken, nil, &otherHistory, 200)
	if len(otherHistory.Items) != 0 {
		t.Fatalf("cross-subject history exposed: %+v", otherHistory)
	}
	// Both real User Center accounts now own a frozen v1 answer in the same
	// logical session; the v2 projection must keep their UID scopes separate.
	otherAnswerID := "http-other-answer"
	otherSubject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform",
		SubjectId: fmt.Sprintf("%d", otherUID)}
	oldSubjectJSON, err := json.Marshal(acceptedSubject)
	if err != nil {
		t.Fatal(err)
	}
	otherSubjectJSON, err := json.Marshal(otherSubject)
	if err != nil {
		t.Fatal(err)
	}
	otherTurn := strings.Replace(commitAnswer.TurnJson, string(oldSubjectJSON), string(otherSubjectJSON), 1)
	otherTurn = strings.Replace(otherTurn, `"AnswerID":"`+answerID+`"`, `"AnswerID":"`+otherAnswerID+`"`, 1)
	otherTurn = strings.Replace(otherTurn, `"answer_id":"`+answerID+`"`, `"answer_id":"`+otherAnswerID+`"`, 1)
	if otherTurn == commitAnswer.TurnJson {
		t.Fatal("other UID turn did not update frozen Subject and AnswerID")
	}
	otherCommit := types.CommitAcceptedAnswerReq{AnswerId: otherAnswerID, SearchId: searchID,
		Subject: otherSubject, SessionId: commitAnswer.SessionId, TurnJson: otherTurn}
	var otherAccepted types.AcceptedAnswer
	request("POST", "/internal/v1/knowledge/accepted-answers", c.WorkerToken, otherCommit, &otherAccepted, 200)
	var otherV2History types.AcceptedAnswersPageV2
	request("GET", v2ProductPath, otherToken, nil, &otherV2History, 200)
	if len(otherV2History.Items) != 1 || otherV2History.Items[0].AnswerId != otherAnswerID ||
		otherV2History.Items[0].Subject.SubjectId != fmt.Sprintf("%d", otherUID) ||
		otherV2History.Items[0].TurnJson != otherAccepted.TurnJson {
		t.Fatalf("other UID lost its own v1→v2 answer: %+v", otherV2History)
	}
	request("GET", v2ProductPath+"/"+otherAnswerID, productToken, nil, nil, 404)
	request("GET", v2ProductPath+"/"+answerID, otherToken, nil, nil, 404)
	request("GET", productPath+"?limit=101", productToken, nil, nil, 400)
	request("GET", productPath+"?after_ordinal=-1", productToken, nil, nil, 400)
	request("GET", "/v1/knowledge/answer-sessions/other-session/accepted-answers/"+answerID, productToken, nil, nil, 404)
	second := commitAnswer
	second.AnswerId = "http-answer-2"
	second.TurnJson = strings.ReplaceAll(commitAnswer.TurnJson, answerID, second.AnswerId)
	var secondAccepted types.AcceptedAnswer
	request("POST", "/internal/v1/knowledge/accepted-answers", c.WorkerToken, second, &secondAccepted, 200)
	if secondAccepted.AcceptedOrdinal != 2 {
		t.Fatalf("second accepted answer ordinal=%d", secondAccepted.AcceptedOrdinal)
	}
	request("GET", productPath+"?limit=1", productToken, nil, &ownHistory, 200)
	if len(ownHistory.Items) != 1 || ownHistory.Items[0] != accepted || ownHistory.NextOrdinal != 1 {
		t.Fatalf("first product page differs: %+v", ownHistory)
	}
	request("GET", productPath+"?limit=1&after_ordinal=1", productToken, nil, &ownHistory, 200)
	if len(ownHistory.Items) != 1 || ownHistory.Items[0] != secondAccepted || ownHistory.NextOrdinal != 0 {
		t.Fatalf("second product page differs: %+v", ownHistory)
	}
	if realUser {
		var secondCitationState types.ProductAnswerCitationStates
		request("GET", productPath+"/"+second.AnswerId+"/citations", productToken, nil, &secondCitationState, 200)
		if len(secondCitationState.Citations) != 1 || secondCitationState.Citations[0].State != "available" ||
			secondCitationState.Citations[0].EvidenceId != citations.Evidence[0].EvidenceId {
			t.Fatalf("second accepted answer lacks current citation: %+v", secondCitationState)
		}
		waitSharedProductHistory(t, "available", sharedProductHistoryReady(base, productToken, otherToken,
			commitAnswer.SessionId, answerID, second.AnswerId, accepted.Status, secondAccepted.Status,
			quote, citations.Evidence[0].EvidenceId, a.RevisionId, quoteHash, "available"))
	}
	if !realUser {
		userRPC.deleted.Store(true)
		request("GET", productPath, productToken, nil, nil, 403)
		request("GET", productPath+"/"+answerID, productToken, nil, nil, 403)
		request("GET", citationStatePath, productToken, nil, nil, 403)
		userRPC.deleted.Store(false)
	}
	answerQuery.Set("subject_id", "other-uid")
	request("GET", "/internal/v1/knowledge/accepted-answers/"+answerID+"?"+answerQuery.Encode(), c.WorkerToken, nil, nil, 404)
	answerQuery.Set("subject_id", acceptedSubject.SubjectId)
	conflictingAnswer := commitAnswer
	conflictingAnswer.TurnJson = strings.Replace(commitAnswer.TurnJson, "Evidence is cited.", "Different answer.", 1)
	request("POST", "/internal/v1/knowledge/accepted-answers", c.WorkerToken, conflictingAnswer, nil, 409)
	var stored types.Revision
	request("GET", "/internal/v1/knowledge/revisions/"+a.RevisionId, c.WorkerToken, nil, &stored, 200)
	if stored.Content != "Book A\n\nEvidence" {
		t.Fatal(stored)
	}
	var outbox int
	if err = s.DB.QueryRow(context.Background(), "SELECT count(*) FROM knowledge_outbox WHERE event_type='knowledge.release.activated.v1'").Scan(&outbox); err != nil || outbox != 1 {
		t.Fatalf("publish events=%d err=%v", outbox, err)
	}
	var traceparent, originRequestID string
	if err = s.DB.QueryRow(context.Background(), "SELECT correlation->>'traceparent',correlation->>'request_id' FROM knowledge_outbox WHERE event_type='knowledge.release.activated.v1'").Scan(&traceparent, &originRequestID); err != nil || !strings.HasPrefix(traceparent, "00-") || originRequestID == "" {
		t.Fatalf("durable outbox correlation missing: traceparent=%q request_id=%q err=%v", traceparent, originRequestID, err)
	}
	verifyProductReaders(t, s, request, token, c.WorkerToken, filepath.Join(dir, "objects"), m, a, w, r, build)
	request("GET", "/internal/v1/knowledge/search-citations/"+searchID, c.WorkerToken, nil, &citations, 200)
	if len(citations.Evidence) != 1 || citations.Evidence[0].State != "unavailable" {
		t.Fatalf("withdrawal did not invalidate historical citation: %+v", citations)
	}
	request("GET", citationStatePath, productToken, nil, &citedStates, 200)
	if len(citedStates.Citations) != 1 || citedStates.Citations[0].State != "unavailable" {
		t.Fatalf("product citation state ignored withdrawal: %+v", citedStates)
	}
	request("GET", v2CitationPath, productToken, nil, &v2States, 200)
	if len(v2States.Citations) != 1 || v2States.Citations[0].State != "unavailable" ||
		v2States.Citations[0].QuoteHash != quoteHash {
		t.Fatalf("v2 citation replaced old quote/hash after withdrawal: %+v", v2States)
	}
	request("GET", v2ProductPath+"/"+answerID, productToken, nil, &v2Answer, 200)
	if v2Answer.TurnJson != originalTurn || object.Hash([]byte(v2Answer.TurnJson)) != originalTurnHash {
		t.Fatalf("v2 history changed immutable turn after withdrawal: %+v", v2Answer)
	}
	if !bytes.Equal(v1AnswerWireBefore, readProductBytes(productPath+"/"+answerID, productToken)) {
		t.Fatal("withdrawal or v2 projection changed old v1 GET response bytes")
	}
	if realUser {
		var secondCitationState types.ProductAnswerCitationStates
		request("GET", productPath+"/"+second.AnswerId+"/citations", productToken, nil, &secondCitationState, 200)
		if len(secondCitationState.Citations) != 1 || secondCitationState.Citations[0].State != "unavailable" {
			t.Fatalf("second answer retained withdrawn citation: %+v", secondCitationState)
		}
		waitSharedProductHistory(t, "withdrawn", sharedProductHistoryReady(base, productToken, otherToken,
			commitAnswer.SessionId, answerID, second.AnswerId, accepted.Status, secondAccepted.Status,
			quote, citations.Evidence[0].EvidenceId, a.RevisionId, quoteHash, "unavailable"))
	}
	disableRealOwner := func() {
		// A disabled account is still returned by today's GetUser handler. This
		// observed counterexample keeps the H02 disable gate open.
		realUsers.markDisabled(t, productUID)
		request("POST", toolPath, productToken, toolParentBody, nil, 403)
		request("GET", parentPath, productToken, nil, nil, 403)
		request("GET", toolSearchPath+"/"+toolSearch.SearchId, productToken, nil, nil, 403)
		request("POST", parentPath+"/evidence-reads", productToken,
			map[string]any{"search_id": toolSearch.SearchId, "evidence_id": "ev_missing",
				"idempotency_key": "tool-read-key-disabled"}, nil, 403)
		request("POST", searchPath, productToken, searchBody, nil, 403)
		request("GET", searchPath+"/"+productSearchID, productToken, nil, nil, 403)
		request("GET", productPath, productToken, nil, nil, 403)
		request("GET", productPath+"/"+answerID, productToken, nil, nil, 403)
		request("GET", citationStatePath, productToken, nil, nil, 403)
		request("GET", v2ProductPath, productToken, nil, nil, 403)
		request("GET", v2ProductPath+"/"+answerID, productToken, nil, nil, 403)
		request("GET", v2CitationPath, productToken, nil, nil, 403)
		request("GET", productPath, otherToken, nil, &otherHistory, 200)
		if len(otherHistory.Items) != 1 || otherHistory.Items[0] != otherAccepted {
			t.Fatalf("active other subject changed after disabling owner: %+v", otherHistory)
		}
		request("GET", v2ProductPath, otherToken, nil, &otherV2History, 200)
		if len(otherV2History.Items) != 1 || otherV2History.Items[0].AnswerId != otherAnswerID {
			t.Fatalf("active other v2 subject changed after disabling owner: %+v", otherV2History)
		}
		realUsers.delete(t, productUID)
		request("GET", parentPath, productToken, nil, nil, 403)
		request("GET", productPath, productToken, nil, nil, 403)
		request("GET", productPath+"/"+answerID, productToken, nil, nil, 403)
		request("GET", citationStatePath, productToken, nil, nil, 403)
		realUsers.stopRPC(t)
	}
	if realUser && !publishNewRelease {
		disableRealOwner()
	} else if !publishNewRelease {
		userServer.Stop()
	}
	if !publishNewRelease {
		request("GET", productPath, productToken, nil, nil, 503)
		request("GET", parentPath, productToken, nil, nil, 503)
		request("POST", searchPath, productToken, searchBody, nil, 503)
		request("GET", searchPath+"/"+productSearchID, productToken, nil, nil, 503)
		request("GET", citationStatePath, productToken, nil, nil, 503)
	}
	if btwRoot := os.Getenv("SEA_BTW_INDEX_CONSUMER_ROOT"); btwRoot != "" {
		// The ordinary fixture uses an unpublished module. Cancellation keeps
		// the already published module's active pointer while new candidates build.
		cancelNewRelease := os.Getenv("SEA_BGE_WORKER_CANCEL_RELEASE") == "1"
		if publishNewRelease && !cancelNewRelease {
			t.Fatal("publication search requires the cancelled candidate and new Build generation")
		}
		var indexModule types.Module
		var publishedBefore types.ReleaseState
		var publicationCountBefore int
		var h06Baseline h06PublishedBaseline
		var newReleaseForPublish types.Release
		bgeRuntimeFile := os.Getenv("SEA_BGE_RUNTIME_FILE")
		if cancelNewRelease {
			// The main workflow later withdraws its original source. A separate
			// task-owned module keeps a valid published baseline for CAS checks.
			request("POST", "/v1/knowledge/modules", token,
				types.CreateModuleReq{Title: "Cancellation publication baseline", IdempotencyKey: "http-cancel-module"}, &indexModule, 200)
			var baselineSource types.Revision
			request("POST", "/v1/knowledge/modules/"+indexModule.Id+"/sources", token,
				types.CreateSourceReq{Title: "Published baseline", Content: "baseline\n\npublic",
					MediaType: "text/markdown", Provenance: "synthetic", IdempotencyKey: "http-cancel-baseline-source"},
				&baselineSource, 200)
			var baselineRelease types.Release
			baselineProfiles := testenv.Profiles()
			if publishNewRelease {
				if bgeRuntimeFile == "" {
					t.Fatal("formal publication search requires actual locked BGE runtime")
				}
				baselineProfiles = liveBGEIndexProfiles(t, bgeRuntimeFile)
			}
			request("POST", "/v1/knowledge/modules/"+indexModule.Id+"/releases", token,
				types.CreateReleaseReq{SourceRevisionIds: []string{baselineSource.RevisionId},
					WikiRevisionIds: []string{}, ChunkingProfile: "index-paragraph-v1",
					RetrievalProfiles: baselineProfiles, IdempotencyKey: "http-cancel-baseline-release"},
				&baselineRelease, 200)
			var baselineBuild types.Build
			request("POST", "/v1/knowledge/releases/"+baselineRelease.ReleaseId+"/index-builds", token,
				types.CreateBuildReq{IdempotencyKey: "http-cancel-baseline-build"}, &baselineBuild, 200)
			if publishNewRelease {
				h06Baseline = buildH06PublishedBaseline(t, s, request, dir, btwRoot, base,
					c.WorkerToken, token, bgeRuntimeFile, indexModule.Id,
					baselineSource, baselineRelease, baselineBuild)
				request("GET", "/v1/knowledge/modules/"+indexModule.Id+"/releases/current", token,
					nil, &publishedBefore, 200)
			} else {
				baselineBuild = testenv.Ready(t, s, baselineBuild, baselineRelease)
				request("PUT", "/v1/knowledge/modules/"+indexModule.Id+"/activation", token,
					types.ActivateReq{ReleaseId: baselineRelease.ReleaseId, BuildId: baselineBuild.BuildId,
						ExpectedPointerRevision: 0, Reason: "isolated cancellation fixture baseline"}, &publishedBefore, 200)
			}
			if publishedBefore.ActiveReleaseId == "" || publishedBefore.ActiveBuildId == "" || publishedBefore.PointerRevision < 1 {
				t.Fatalf("cancel/new-release fixture requires an already published module: %+v", publishedBefore)
			}
			if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM knowledge_publications WHERE module_id=$1", indexModule.Id).
				Scan(&publicationCountBefore); err != nil || publicationCountBefore < 1 {
				t.Fatalf("published baseline receipt absent: count=%d err=%v", publicationCountBefore, err)
			}
		} else {
			request("POST", "/v1/knowledge/modules", token,
				types.CreateModuleReq{Title: "Cross-repository index handoff", IdempotencyKey: "http-index-module"}, &indexModule, 200)
		}
		var indexSource types.Revision
		request("POST", "/v1/knowledge/modules/"+indexModule.Id+"/sources", token,
			types.CreateSourceReq{Title: "Index source", Content: "east\n\nnorth", MediaType: "text/markdown",
				Provenance: "synthetic", IdempotencyKey: "http-index-source"}, &indexSource, 200)
		profiles := []types.RetrievalProfile{
			{Lane: "dense", Encoder: "fixture_model", Tokenizer: "tokens_v1", Space: "dense_space", Dimensions: 2},
			{Lane: "sparse", Encoder: "fixture_model", Tokenizer: "tokens_v1", Space: "sparse_space", Dimensions: 100},
			{Lane: "multivector", Encoder: "fixture_model", Tokenizer: "tokens_v1", Space: "multi_space",
				Dimensions: 2, Mask: "valid", Aggregation: "sum_maxsim"},
		}
		if bgeRuntimeFile != "" {
			profiles = liveBGEIndexProfiles(t, bgeRuntimeFile)
		}
		var indexRelease types.Release
		request("POST", "/v1/knowledge/modules/"+indexModule.Id+"/releases", token,
			types.CreateReleaseReq{SourceRevisionIds: []string{indexSource.RevisionId}, WikiRevisionIds: []string{},
				ChunkingProfile: "index-paragraph-v1", RetrievalProfiles: profiles,
				IdempotencyKey: "http-index-release"}, &indexRelease, 200)
		var indexBuild types.Build
		request("POST", "/v1/knowledge/releases/"+indexRelease.ReleaseId+"/index-builds", token,
			types.CreateBuildReq{IdempotencyKey: "http-index-build"}, &indexBuild, 200)
		if indexBuild.State != "BUILDING" || indexBuild.AttemptId != "" || indexBuild.LeaseEpoch != 0 {
			t.Fatalf("cross-repository input was not an unclaimed build: %+v", indexBuild)
		}
		resultPath := filepath.Join(dir, "btw-real-index-result.json")
		fixtureData := map[string]any{
			"base_url": base, "worker_token": c.WorkerToken, "objects_dir": filepath.Join(dir, "objects"),
			"build_id": indexBuild.BuildId, "release_id": indexRelease.ReleaseId, "module_id": indexModule.Id,
			"source_revision_ids": []string{indexSource.RevisionId}, "wiki_revision_ids": []string{},
			"chunk_profile": indexRelease.ChunkingProfile, "chunk_size": 64, "chunk_overlap": 0,
			"result_path": resultPath,
		}
		if bgeRuntimeFile != "" {
			fixtureData["bge_runtime_file"] = bgeRuntimeFile
		}
		if cancelNewRelease {
			fixtureData["admin_token"] = token
			fixtureData["published_release_id"] = publishedBefore.ActiveReleaseId
			fixtureData["published_build_id"] = publishedBefore.ActiveBuildId
			fixtureData["published_pointer_revision"] = publishedBefore.PointerRevision
		}
		var actualDC *dcJobPlatform
		if dcRoot := os.Getenv("SEA_DC_JOB_PLATFORM_ROOT"); dcRoot != "" {
			actualDC = startRealDCJobPlatform(t, dir, dcRoot)
			fixtureData["dc_job_url"], fixtureData["dc_job_token"] = actualDC.BaseURL, actualDC.Token
			if os.Getenv("SEA_BGE_WORKER_EXPIRY") == "1" || cancelNewRelease {
				fixtureData["dc_job_dsn"] = actualDC.Pool.Config().ConnString()
			}
		}
		fixtureRaw, marshalErr := json.Marshal(fixtureData)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		fixturePath := filepath.Join(dir, "btw-real-index-fixture.json")
		if err := os.WriteFile(fixturePath, fixtureRaw, 0600); err != nil {
			t.Fatal(err)
		}
		consumer := exec.Command("bash", "cmd/worker/acceptance.sh")
		consumer.Dir = btwRoot
		consumerTest := "TestRTWRealProviderIndexDispatch"
		if os.Getenv("SEA_BGE_WORKER_PROCESSES") == "1" {
			if bgeRuntimeFile == "" || actualDC == nil {
				t.Fatal("real BGE worker processes require the locked provider and actual DC jobs")
			}
			consumerTest = "TestRTWRealBGEWorkerProcesses"
			if os.Getenv("SEA_BGE_WORKER_EXPIRY") == "1" {
				consumerTest = "TestRTWRealBGEWorkerLeaseExpiry"
			}
			if cancelNewRelease {
				consumerTest = "TestRTWRealBGECancelNewRelease"
			}
		}
		consumer.Env = append(os.Environ(), "SEA_RTW_REAL_INDEX_FIXTURE="+fixturePath,
			"GOFLAGS=-run=^"+consumerTest+"$")
		output, runErr := consumer.CombinedOutput()
		if runErr != nil {
			t.Fatalf("BTW index consumer did not hand off real RTW READY: %v\n%s", runErr, output)
		}
		if testing.Verbose() {
			t.Logf("BTW real RTW index consumer: %s", strings.TrimSpace(string(output)))
		}
		resultRaw, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			t.Fatalf("BTW did not emit its independently verified result: %v", readErr)
		}
		var handoff struct {
			BuildID           string `json:"build_id"`
			OldBuildID        string `json:"old_build_id"`
			OldReleaseID      string `json:"old_release_id"`
			FirstNewBuildID   string `json:"first_new_build_id"`
			NewReleaseID      string `json:"new_release_id"`
			NewReleaseOrdinal int64  `json:"new_release_ordinal"`
			OldDCJobID        string `json:"old_dc_job_id"`
			IndexManifestRef  string `json:"index_manifest_ref"`
			IndexManifestHash string `json:"index_manifest_hash"`
			DCAckRef          string `json:"dc_ack_ref"`
			DCAckHash         string `json:"dc_ack_hash"`
			DCJobID           string `json:"dc_job_id"`
			DCLeaseEpoch      int64  `json:"dc_lease_epoch"`
			RTWLeaseEpoch     int64  `json:"rtw_lease_epoch"`
			RTWState          string `json:"rtw_state"`
			RTWGeneration     int64  `json:"rtw_generation"`
		}
		if err := json.Unmarshal(resultRaw, &handoff); err != nil {
			t.Fatalf("decode BTW accepted handoff: %v", err)
		}
		expectedBuildID, expectedGeneration := indexBuild.BuildId, indexBuild.Generation
		if cancelNewRelease {
			expectedBuildID, expectedGeneration = handoff.BuildID, 2
		}
		if handoff.BuildID != expectedBuildID ||
			handoff.RTWGeneration != expectedGeneration || handoff.RTWState != "READY" ||
			handoff.IndexManifestRef != "sha256/"+handoff.IndexManifestHash ||
			handoff.DCAckRef != "sha256:"+handoff.IndexManifestHash ||
			handoff.DCAckHash != handoff.IndexManifestHash {
			t.Fatalf("BTW result does not tie READY to DC ACK: %+v", handoff)
		}
		if cancelNewRelease {
			if handoff.BuildID == indexBuild.BuildId || handoff.OldBuildID != indexBuild.BuildId ||
				handoff.OldReleaseID != indexRelease.ReleaseId || handoff.FirstNewBuildID == "" ||
				handoff.NewReleaseID == "" || handoff.NewReleaseID == indexRelease.ReleaseId ||
				handoff.NewReleaseOrdinal != indexRelease.Ordinal+1 || handoff.OldDCJobID == "" {
				t.Fatalf("replacement Release/Build identity was not distinct: %+v", handoff)
			}
			var old, firstNew types.Build
			request("GET", "/internal/v1/knowledge/builds/"+indexBuild.BuildId, c.WorkerToken, nil, &old, 200)
			request("GET", "/internal/v1/knowledge/builds/"+handoff.FirstNewBuildID, c.WorkerToken, nil, &firstNew, 200)
			if old.State != "CANCELLED" || old.IndexManifestRef != "" || firstNew.State != "SUPERSEDED" ||
				firstNew.Generation != 1 || firstNew.ReleaseId != handoff.NewReleaseID {
				t.Fatalf("old cancelled/new generation predecessor mismatch: old=%+v first=%+v", old, firstNew)
			}
			var newRelease types.Release
			request("GET", "/internal/v1/knowledge/releases/"+handoff.NewReleaseID, c.WorkerToken, nil, &newRelease, 200)
			if newRelease.Ordinal != handoff.NewReleaseOrdinal || newRelease.ManifestHash == indexRelease.ManifestHash {
				t.Fatalf("new Release was not frozen independently: %+v", newRelease)
			}
			newReleaseForPublish = newRelease
		}
		var acceptedBuild types.Build
		request("GET", "/internal/v1/knowledge/builds/"+expectedBuildID, c.WorkerToken, nil, &acceptedBuild, 200)
		if acceptedBuild.State != "READY" || acceptedBuild.IndexManifestRef != handoff.IndexManifestRef ||
			acceptedBuild.IndexManifestHash != handoff.IndexManifestHash ||
			acceptedBuild.Generation != expectedGeneration {
			t.Fatalf("RTW HTTP returned a different accepted build: %+v", acceptedBuild)
		}
		if actualDC != nil {
			expectedDC, expectedRTW := int64(1), int64(2)
			if os.Getenv("SEA_BGE_WORKER_EXPIRY") == "1" {
				expectedDC, expectedRTW = 2, 3
			}
			if handoff.DCJobID == "" || handoff.DCLeaseEpoch != expectedDC || handoff.RTWLeaseEpoch != expectedRTW ||
				acceptedBuild.LeaseEpoch != handoff.RTWLeaseEpoch {
				t.Fatalf("actual DC job and RTW build fence were conflated: handoff=%+v RTW=%+v", handoff, acceptedBuild)
			}
			actualDC.assertAcceptedIndexJob(t, handoff.DCJobID, handoff.IndexManifestHash, handoff.DCLeaseEpoch)
			if cancelNewRelease {
				var oldState string
				var cancelVersion int64
				var oldResults int
				err := actualDC.Pool.QueryRow(context.Background(), `SELECT state,cancel_version,
 (SELECT count(*) FROM jobs.attempt WHERE job_id=$1 AND result_hash IS NOT NULL)
 FROM jobs.job WHERE id=$1`, handoff.OldDCJobID).Scan(&oldState, &cancelVersion, &oldResults)
				if err != nil || oldState != "cancelled" || cancelVersion != 1 || oldResults != 0 {
					t.Fatalf("old DC job survived explicit cancellation: state=%s cancel=%d results=%d err=%v",
						oldState, cancelVersion, oldResults, err)
				}
			}
		}
		var storedBuildRaw []byte
		if err := s.DB.QueryRow(context.Background(), "SELECT data FROM knowledge_builds WHERE id=$1", expectedBuildID).
			Scan(&storedBuildRaw); err != nil {
			t.Fatal(err)
		}
		var storedBuild types.Build
		if err := json.Unmarshal(storedBuildRaw, &storedBuild); err != nil || !reflect.DeepEqual(storedBuild, acceptedBuild) {
			t.Fatalf("RTW PostgreSQL differs from HTTP accepted READY: %+v err=%v", storedBuild, err)
		}
		var acceptedEvents, indexPublications int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_outbox
	WHERE event_type='knowledge.index.build.accepted.v1' AND aggregate_id=$1
	AND payload->'payload'->>'build_id'=$2`, indexModule.Id, expectedBuildID).Scan(&acceptedEvents); err != nil || acceptedEvents != 1 {
			t.Fatalf("RTW acceptance outbox was not exactly once: count=%d err=%v", acceptedEvents, err)
		}
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_publications WHERE module_id=$1`,
			indexModule.Id).Scan(&indexPublications); err != nil || indexPublications != publicationCountBefore {
			t.Fatalf("index READY moved the manual publication pointer: count=%d err=%v", indexPublications, err)
		}
		if cancelNewRelease {
			var publishedAfter types.ReleaseState
			request("GET", "/v1/knowledge/modules/"+indexModule.Id+"/releases/current", token, nil, &publishedAfter, 200)
			if publishedAfter.ActiveReleaseId != publishedBefore.ActiveReleaseId ||
				publishedAfter.ActiveBuildId != publishedBefore.ActiveBuildId ||
				publishedAfter.PointerRevision != publishedBefore.PointerRevision {
				t.Fatalf("candidate build changed active publication: before=%+v after=%+v", publishedBefore, publishedAfter)
			}
		} else {
			request("GET", "/internal/v1/knowledge/modules/"+indexModule.Id+"/search-snapshot", c.WorkerToken, nil, nil, 404)
		}
		if publishNewRelease {
			var builtNew realIndexResult
			var identity struct {
				NewSourceRevisionID string `json:"new_source_revision_id"`
			}
			if err := json.Unmarshal(resultRaw, &builtNew); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(resultRaw, &identity); err != nil || identity.NewSourceRevisionID == "" {
				t.Fatalf("replacement source identity absent: %+v err=%v", identity, err)
			}
			var newSource types.Revision
			request("GET", "/internal/v1/knowledge/revisions/"+identity.NewSourceRevisionID,
				c.WorkerToken, nil, &newSource, 200)
			if newSource.Content != "west\n\nsouth" || newSource.ModuleId != indexModule.Id {
				t.Fatalf("new published candidate source changed: %+v", newSource)
			}
			runH06PublishedSearchCycle(t, s, request, searchFixture, dir, btwRoot, base,
				c.WorkerToken, token, productToken, otherToken, searchPath,
				"/v1/knowledge/answer-sessions/search-facade-session/accepted-answers",
				bgeRuntimeFile, indexModule.Id, h06Baseline, newReleaseForPublish,
				acceptedBuild, newSource, builtNew, realUser, productUID)
			if realUser {
				verified, err := realUsers.rpc.GetUser(context.Background(), &pb.GetUserReq{Uid: productUID})
				if err != nil || !verified.Found || verified.User == nil || verified.User.Uid != productUID {
					t.Fatalf("real User Center RPC did not verify signed search owner: %+v err=%v", verified, err)
				}
			}
		}
	}
	if publishNewRelease {
		if realUser {
			disableRealOwner()
		} else {
			userServer.Stop()
		}
		request("GET", productPath, productToken, nil, nil, 503)
		request("GET", parentPath, productToken, nil, nil, 503)
		request("GET", citationStatePath, productToken, nil, nil, 503)
	}
	metrics, err := client.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metricBody, err := io.ReadAll(metrics.Body)
	metrics.Body.Close()
	if err != nil || metrics.StatusCode != 200 {
		t.Fatalf("metrics status=%d err=%v", metrics.StatusCode, err)
	}
	for _, name := range []string{"sea_knowledge_operations_total", "sea_knowledge_commits_total", "sea_knowledge_http_requests_total", "sea_knowledge_outbox_pending"} {
		if !bytes.Contains(metricBody, []byte(name)) {
			t.Fatalf("missing real metric %s", name)
		}
	}
	if evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidence, "knowledge-metrics.prom"), metricBody, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var committedPublications int
	if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM knowledge_publications").Scan(&committedPublications); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metricBody), `sea_knowledge_commits_total{operation="knowledge.release.activate"} `+fmtInt(committedPublications)) {
		var activationMetrics []string
		for _, line := range strings.Split(string(metricBody), "\n") {
			if strings.Contains(line, "sea_knowledge_commits_total") && strings.Contains(line, "knowledge.release.activate") {
				activationMetrics = append(activationMetrics, line)
			}
		}
		t.Fatalf("activation replay or conflict counted as another committed publication: %v", activationMetrics)
	}
	expectedCitationCommits := 1
	if os.Getenv("SEA_BTW_CITATION_CONSUMER_ROOT") != "" {
		expectedCitationCommits += 3 // BTW delivery, direct typed Tool and native Agent Tool commit distinct search IDs
	}
	if os.Getenv("SEA_BTW_PRODUCT_SEARCH_ROOT") != "" {
		expectedCitationCommits++ // The signed product search accepts one real RTW citation.
	}
	if os.Getenv("SEA_BTW_TOOLS_CONSUMER_ROOT") != "" {
		expectedCitationCommits++ // The signed Tools child accepts one real RTW citation.
	}
	if publishNewRelease {
		expectedCitationCommits += 3 // old, new and rolled-back versions each accept one real citation.
	}
	if !strings.Contains(string(metricBody), fmt.Sprintf(`sea_knowledge_commits_total{operation="knowledge.search.citations.accept"} %d`, expectedCitationCommits)) {
		t.Fatal("citation replay was counted as another durable commit")
	}
	var records []map[string]any
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		raw, e := os.ReadFile(filepath.Join(dir, "http.log"))
		if e != nil {
			t.Fatal(e)
		}
		records = records[:0]
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var record map[string]any
			if e = json.Unmarshal(line, &record); e != nil {
				t.Fatalf("non-JSON runtime log: %q: %v", line, e)
			}
			records = append(records, record)
		}
		found := map[string]bool{}
		for _, record := range records {
			if event, ok := record["event"].(string); ok {
				found[event] = true
			}
		}
		if found["knowledge.service.started"] && found["knowledge.module.create.succeeded"] && found["knowledge.release.activate.rejected"] && found["http.request.completed"] {
			break
		}
	}
	seen := map[string]bool{}
	seenUnmatchedPost := false
	for _, record := range records {
		event, _ := record["event"].(string)
		seen[event] = true
		for _, field := range []string{"timestamp", "level", "service", "environment", "service_version", "instance_id", "component", "log_source", "event", "message"} {
			if record[field] == nil || record[field] == "" {
				t.Fatalf("event %s missing %s: %#v", event, field, record)
			}
		}
		if record["service_version"] != c.Observability.Version {
			t.Fatalf("unexpected service version: %#v", record)
		}
		if record["log_source"] == "framework" && strings.Contains(record["message"].(string), `"title":"No auth"`) {
			t.Fatalf("framework request dump entered application log: %#v", record)
		}
		if strings.HasPrefix(event, "knowledge.module.create.") || strings.HasPrefix(event, "knowledge.release.activate.") || event == "http.request.completed" {
			if record["trace_id"] == nil || record["span_id"] == nil || record["request_id"] == nil {
				t.Fatalf("request correlation missing: %#v", record)
			}
		}
		if strings.HasPrefix(event, "knowledge.search.") {
			if record["trace_id"] == nil || record["span_id"] == nil || record["request_id"] == nil || record["operation_id"] == nil {
				t.Fatalf("citation stage lacks request and trace correlation: %#v", record)
			}
			if strings.Contains(fmt.Sprint(record), quote) {
				t.Fatalf("citation stage logged source quote: %#v", record)
			}
		}
		if event == "http.request.completed" && record["route"] == "unmatched" && record["method"] == "POST" && record["status"] == float64(404) {
			seenUnmatchedPost = true
		}
	}
	if !seenUnmatchedPost {
		t.Fatal("unmatched POST lost its bounded actual HTTP method")
	}
	for _, event := range []string{"knowledge.service.starting", "knowledge.service.started", "knowledge.module.create.succeeded", "knowledge.release.activate.rejected", "knowledge.search.snapshot.current.succeeded", "knowledge.search.source.read.succeeded", "knowledge.search.citations.accept.succeeded", "knowledge.search.citations.accept.replayed", "knowledge.search.citations.get.succeeded", "http.request.completed"} {
		if !seen[event] {
			t.Fatalf("missing runtime event %s in %d records", event, len(records))
		}
	}
}
func fmtInt(n int) string { b, _ := json.Marshal(n); return string(b) }
