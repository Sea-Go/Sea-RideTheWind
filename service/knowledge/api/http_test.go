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
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/config"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/conf"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

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
	contract := loadGeneratedHTTPContract(t)
	s := testenv.Store(t)
	dir := t.TempDir()
	objects, err := object.NewLocal(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
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
	c.Timeout = 15000
	c.Log.Mode = "console"
	c.Log.Level = "info"
	c.Observability.Version = os.Getenv("KNOWLEDGE_TEST_VERSION")
	if c.Observability.Version == "" {
		c.Observability.Version = "knowledge-http-test-binary"
	}
	c.Auth.AccessSecret = "synthetic-test-jwt-secret-not-a-real-key"
	c.Auth.AccessExpire = 3600
	c.AdministratorIDs = []string{"test-admin"}
	c.WorkerToken = "synthetic-worker-token"
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
	base := "http://" + net.JoinHostPort("127.0.0.1", fmtInt(port))
	client := &http.Client{Timeout: 5 * time.Second}
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
	var r types.Release
	request("POST", "/v1/knowledge/modules/"+m.Id+"/releases", token, types.CreateReleaseReq{SourceRevisionIds: []string{a.RevisionId}, WikiRevisionIds: []string{w.RevisionId}, ChunkingProfile: "paragraph-v1", RetrievalProfiles: testenv.Profiles(), IdempotencyKey: "http-release"}, &r, 200)
	var build types.Build
	request("POST", "/v1/knowledge/releases/"+r.ReleaseId+"/index-builds", token, types.CreateBuildReq{IdempotencyKey: "http-build"}, &build, 200)
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/claim", c.WorkerToken, types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), Generation: build.Generation, AttemptId: "http-worker", LeaseEpoch: 1, ManifestHash: build.ManifestHash}, &build, 200)
	ref := testenv.Put(t, s, testenv.Index(t, s, build, r))
	result := types.AcceptBuildReq{Generation: build.Generation, AttemptId: build.AttemptId, LeaseEpoch: build.LeaseEpoch, ManifestHash: build.ManifestHash, State: "READY", IndexManifestRef: ref.Key, IndexManifestHash: ref.SHA256}
	request("POST", "/internal/v1/knowledge/builds/"+build.BuildId+"/results", c.WorkerToken, result, &build, 200)
	request("GET", "/v1/knowledge/modules/"+m.Id+"/published", "", nil, nil, 404)
	request("GET", "/v1/knowledge/modules/"+m.Id+"/releases/current", token, nil, &state, 200)
	if state.BuildState != "READY" || state.ActiveReleaseId != "" {
		t.Fatal(state)
	}
	activation := types.ActivateReq{ReleaseId: r.ReleaseId, BuildId: build.BuildId, ExpectedPointerRevision: 0, Reason: "manual HTTP publication"}
	request("PUT", "/v1/knowledge/modules/"+m.Id+"/activation", token, activation, &state, 200)
	if state.PointerRevision != 1 || state.ActiveReleaseId != r.ReleaseId {
		t.Fatal(state)
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
	verifyProductReaders(t, s, request, token, filepath.Join(dir, "objects"), m, a, w, r, build)
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
		if event == "http.request.completed" && record["route"] == "unmatched" && record["method"] == "POST" && record["status"] == float64(404) {
			seenUnmatchedPost = true
		}
	}
	if !seenUnmatchedPost {
		t.Fatal("unmatched POST lost its bounded actual HTTP method")
	}
	for _, event := range []string{"knowledge.service.starting", "knowledge.service.started", "knowledge.module.create.succeeded", "knowledge.release.activate.rejected", "http.request.completed"} {
		if !seen[event] {
			t.Fatalf("missing runtime event %s in %d records", event, len(records))
		}
	}
}
func fmtInt(n int) string { b, _ := json.Marshal(n); return string(b) }
