package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"google.golang.org/grpc"
)

// The independent parent starts a disposable PG16; the actual RTW REST
// process and a separate client must recover the same Wiki object bytes.
// This is the worker HTTP boundary, not a DC Job or model-quality test.
func TestWikiCompilePrivateHTTPAcceptReadsSharedOriginalAndMovesEditHead(t *testing.T) {
	if os.Getenv("KNOWLEDGE_TEST_DSN") == "" {
		t.Skip("run service/knowledge/scripts/acceptance.sh with isolated PG16")
	}
	ctx := context.Background()
	source := testenv.Store(t)
	directory := t.TempDir()
	root := filepath.Join(directory, "shared-objects")
	shared, err := object.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	source.Objects = shared
	module, err := source.CreateModule(ctx, "fixture-admin", types.CreateModuleReq{
		Title: "Worker HTTP Wiki", IdempotencyKey: "wiki-http-module",
	})
	if err != nil {
		t.Fatal(err)
	}
	original, err := source.CreateSource(ctx, "fixture-admin", types.CreateSourceReq{
		ModuleId: module.Id, Title: "fixed external source", Content: "one fixed fact", MediaType: "text/plain",
		Provenance: "synthetic", IdempotencyKey: "wiki-http-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	compile, err := source.CreateCompile(ctx, "fixture-admin", types.CreateCompileReq{
		ModuleId: module.Id, PageId: "shared-wiki-page", Guidance: "use only fixed fact",
		SourceRevisionIds: []string{original.RevisionId}, IdempotencyKey: "wiki-http-compile",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Startup needs a valid UserCenter endpoint even though the private worker
	// token routes below never consult product user identity.
	userListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	userServer := grpc.NewServer()
	pb.RegisterUserServiceServer(userServer, &productUserRPC{})
	go func() { _ = userServer.Serve(userListener) }()
	t.Cleanup(func() { userServer.Stop(); _ = userListener.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	var c config.Config
	if err := conf.FillDefault(&c); err != nil {
		t.Fatal(err)
	}
	c.Name, c.Mode, c.Host, c.Port, c.Timeout = "wiki-worker-http-test", "test", "127.0.0.1", port, 120000
	c.Log.Mode, c.Log.Level = "console", "info"
	c.Auth.AccessSecret, c.UserAuth.AccessSecret = "local-http-fixture-jwt", "local-http-fixture-user"
	c.Auth.AccessExpire = 3600
	c.UserRpc.Endpoints = []string{userListener.Addr().String()}
	c.AdministratorIDs = []string{"fixture-admin"}
	c.WorkerToken = "local-http-fixture-worker"
	dsn, err := url.Parse(os.Getenv("KNOWLEDGE_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", source.DB.Config().ConnConfig.RuntimeParams["search_path"])
	dsn.RawQuery = query.Encode()
	c.Postgres.DSN, c.Postgres.MaxConnections = dsn.String(), 4
	c.Objects.Backend, c.Objects.LocalDirectory = "local", root
	c.Observability.Version = "wiki-worker-http-fixture"
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "server.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(directory, "http.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestHTTPProcessHelper$")
	command.Env = append(os.Environ(), "KNOWLEDGE_TEST_SERVER_CONFIG="+configPath)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = command.Process.Kill()
			<-exited
		}
		_ = logFile.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(logFile.Name())
			t.Logf("RTW local worker server failed: %s", raw)
		}
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Timeout: 5 * time.Second}
	for readyBy := time.Now().Add(15 * time.Second); ; time.Sleep(30 * time.Millisecond) {
		response, err := client.Get(base + "/v1/knowledge/modules")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(readyBy) {
			t.Fatal("private RTW worker server did not start")
		}
		select {
		case err := <-exited:
			t.Fatalf("private RTW worker server exited: %v", err)
		default:
		}
	}
	request := func(method, path string, input any, want int, output any) {
		t.Helper()
		var body io.Reader
		if input != nil {
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, base+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+c.WorkerToken)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("worker %s %s status %d want %d body %s", method, path, response.StatusCode, want, raw)
		}
		if output != nil {
			var envelope struct {
				Code int             `json:"code"`
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(raw, &envelope) != nil || envelope.Code != 200 || json.Unmarshal(envelope.Data, output) != nil {
				t.Fatal("RTW private worker response did not carry a valid domain object")
			}
		}
	}
	var fetchedSource types.Revision
	request(http.MethodGet, "/internal/v1/knowledge/revisions/"+original.RevisionId,
		nil, http.StatusOK, &fetchedSource)
	if fetchedSource.RevisionId != original.RevisionId || fetchedSource.Content != "one fixed fact" ||
		fetchedSource.ContentHash != object.Hash([]byte(fetchedSource.Content)) {
		t.Fatal("private RTW source hydration changed frozen original bytes")
	}
	path := "/internal/v1/knowledge/compiles/" + compile.CompileId
	var fetched types.Compile
	request(http.MethodGet, path, nil, http.StatusOK, &fetched)
	if fetched.CompileId != compile.CompileId || fetched.State != "BUILDING" || fetched.InputHash != compile.InputHash {
		t.Fatal("private RTW compile was not the committed fixed ticket")
	}
	expiry := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	claim := types.ClaimCompileReq{CompileId: compile.CompileId, Generation: compile.Generation,
		InputHash: compile.InputHash, AttemptId: "wiki-http-attempt", LeaseEpoch: 1,
		CancelVersion: compile.CancelVersion, LeaseExpiresAt: expiry}
	var claimed types.Compile
	request(http.MethodPost, path+"/claim", claim, http.StatusOK, &claimed)
	if claimed.State != "BUILDING" || claimed.AttemptId != claim.AttemptId || claimed.LeaseEpoch != 1 {
		t.Fatal("private RTW claim did not keep the requested execution fence")
	}
	markdown := []byte("# Maintained external fact\n\n    four-space Markdown code\n")
	otherWriter, err := object.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	key, hash, err := otherWriter.Put(ctx, markdown)
	if err != nil {
		t.Fatal(err)
	}
	acceptedRequest := types.AcceptCompileReq{CompileId: compile.CompileId, State: "READY",
		Generation: compile.Generation, InputHash: compile.InputHash, AttemptId: claim.AttemptId,
		LeaseEpoch: 1, CancelVersion: compile.CancelVersion, ObjectKey: key, ContentHash: hash,
		Title: "Maintained external fact", SourceRefs: []types.SourceRef{{RevisionId: original.RevisionId, Locator: "paragraph:1"}}}
	var accepted types.Compile
	request(http.MethodPost, path+"/results", acceptedRequest, http.StatusOK, &accepted)
	if accepted.State != "ACCEPTED" || accepted.RevisionId == "" || accepted.ResultHash == "" {
		t.Fatal("private RTW accept did not commit an immutable Wiki revision")
	}
	var replay types.Compile
	request(http.MethodPost, path+"/results", acceptedRequest, http.StatusOK, &replay)
	if replay.RevisionId != accepted.RevisionId || replay.ResultHash != accepted.ResultHash {
		t.Fatal("private RTW result retry created a second revision")
	}
	different := acceptedRequest
	different.Title = "a different unaccepted result"
	request(http.MethodPost, path+"/results", different, http.StatusConflict, nil)
	var wiki types.Revision
	request(http.MethodGet, "/internal/v1/knowledge/revisions/"+accepted.RevisionId,
		nil, http.StatusOK, &wiki)
	if wiki.Kind != "wiki" || wiki.ModuleId != module.Id || wiki.EntityId != compile.PageId ||
		wiki.BaseRevisionId != compile.BaseRevisionId || wiki.Content != string(markdown) ||
		wiki.ObjectKey != key || wiki.ContentHash != hash || wiki.CreatedBy != "btw.compile/"+compile.CompileId ||
		len(wiki.SourceRefs) != 1 || wiki.SourceRefs[0].RevisionId != original.RevisionId {
		t.Fatal("private RTW Wiki revision could not read shared original bytes and source")
	}
	var editorHead string
	if err := source.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, module.Id, compile.PageId).Scan(&editorHead); err != nil || editorHead != wiki.RevisionId {
		t.Fatal("AcceptCompile did not advance the Wiki edit head")
	}
	var published int
	if err := source.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_publications WHERE module_id=$1`, module.Id).Scan(&published); err != nil || published != 0 {
		t.Fatal("Wiki accept changed the published Release pointer")
	}
	if _, err := source.CreateCompile(ctx, "fixture-admin", types.CreateCompileReq{
		ModuleId: module.Id, PageId: compile.PageId, BaseRevisionId: compile.BaseRevisionId,
		SourceRevisionIds: []string{original.RevisionId}, Guidance: "stale base", IdempotencyKey: "wiki-http-stale-base",
	}); err == nil {
		t.Fatal("old edit head base remained writable after AI Wiki acceptance")
	}
}
