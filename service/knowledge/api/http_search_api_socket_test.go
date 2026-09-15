package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	otlptracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

type formalSearchAPI struct {
	URL              string
	MetricsURL       string
	LogEvidencePath  string
	modelCalls       *atomic.Int64
	liveGateway      bool
	served           atomic.Bool
	expectedSearchID string
	expectedAnswerID string
}

func (p *formalSearchAPI) AssertSignedSearch(t *testing.T, searchID, answerID string) {
	t.Helper()
	if searchID == "" || answerID == "" {
		t.Fatal("formal cmd/api signed search identity is incomplete")
	}
	p.expectedSearchID, p.expectedAnswerID = searchID, answerID
	p.AssertServed(t)
}

// AssertServed runs only after RTW has received a cited answer through this
// socket. Startup metrics cannot prove a signed product search occurred.
func (p *formalSearchAPI) AssertServed(t *testing.T) {
	t.Helper()
	p.served.Store(true)
	response, err := (&http.Client{Timeout: 2 * time.Second}).Get(p.MetricsURL + "/metrics")
	if err != nil {
		t.Fatalf("formal cmd/api post-search metrics unavailable: %v", err)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK ||
		!bytes.Contains(raw, []byte("sea_btw_operations_total")) {
		t.Fatalf("formal cmd/api post-search metric missing: status=%d err=%v", response.StatusCode, err)
	}
	if !p.liveGateway && p.modelCalls.Load() != 1 {
		t.Fatalf("fixed local OpenAI fixture calls=%d want=1", p.modelCalls.Load())
	}
	if p.liveGateway && p.modelCalls.Load() != 0 {
		t.Fatal("fixed model fixture unexpectedly served the live DataCenter run")
	}
}

// startRealBTWSearchAPIProcess owns an independently compiled cmd/api process.
// It consumes the previously built BGE-M3 three-Ref file only after RTW has
// accepted READY and manually published. Hybrid mode loads the same Dense/
// Multi refs through Lite and learned Sparse through frozen IP postings.
func startRealBTWSearchAPIProcess(t *testing.T, dir, btwRoot, rtwBase, workerToken,
	dcRuntimePath string, built realIndexResult, expectedQuote string,
	nativeRuntimePaths ...string) *formalSearchAPI {
	t.Helper()
	if len(nativeRuntimePaths) > 1 {
		t.Fatal("formal search API accepts at most one explicit native runtime")
	}
	nativeRuntimePath := ""
	if len(nativeRuntimePaths) == 1 {
		nativeRuntimePath = nativeRuntimePaths[0]
	}
	if len(built.APIIndexSettings) == 0 || len(built.Indexes) != 3 || expectedQuote == "" {
		t.Fatal("formal cmd/api requires actual three-lane index settings and expected quote")
	}
	mode, nativeAddress, nativeSettingsPath := "local-exact", "", ""
	if nativeRuntimePath != "" {
		runtime, nativeErr := readRealNativeLiteRuntime(nativeRuntimePath)
		if nativeErr != nil || built.NativeProjection == nil ||
			built.NativeProjection.Status != "test_projection_before_RTW_READY" ||
			built.NativeProjection.PhysicalQualified ||
			built.NativeProjection.Endpoint != runtime.Endpoint ||
			built.NativeProjection.EnginePackageSHA256 != runtime.EnginePackageSHA256 ||
			built.NativeProjection.RuntimeSHA256 != runtime.RawSHA256 ||
			!validRealNativeSettings(built.NativeProjection.Settings) {
			t.Fatal("formal native cmd/api lacks the same task-owned Lite projection receipt")
		}
		mode, nativeAddress = "native-hybrid", runtime.Endpoint
	}
	// Each independently running API owns its executable, configuration and
	// JSONL file. A shared O_TRUNC path lets live file descriptors overwrite
	// each other's offsets and splice two otherwise valid log records together.
	instanceDir := t.TempDir()
	runtimeRaw, err := os.ReadFile(dcRuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	var dc struct {
		Endpoint string `json:"endpoint"`
		Token    string `json:"access_token"`
	}
	if err = json.Unmarshal(runtimeRaw, &dc); err != nil || dc.Endpoint == "" || dc.Token == "" {
		t.Fatal("disposable DataCenter BGE runtime lacks endpoint or token")
	}
	indexFile := filepath.Join(instanceDir, "btw-search-api-index.json")
	if err = os.WriteFile(indexFile, built.APIIndexSettings, 0600); err != nil {
		t.Fatal(err)
	}
	if mode == "native-hybrid" {
		nativeSettingsPath = filepath.Join(instanceDir, "btw-search-api-native.json")
		if err = os.WriteFile(nativeSettingsPath, built.NativeProjection.Settings, 0600); err != nil {
			t.Fatal(err)
		}
	}
	policyFile := filepath.Join(instanceDir, "btw-search-api-policy.json")
	policy := `{"version":"real-bge-three-lane-fast-low-v1","fast_low":{"max_batches":1,"max_subqueries":1,"top_k_per_lane":2,"max_evidence":1,"wall_time":"25s"}}`
	if err = os.WriteFile(policyFile, []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	var modelCalls, traceCalls atomic.Int64
	var nativeRoot atomic.Bool
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" ||
			r.Header.Get("Authorization") != "Bearer test-only-fixed-model-key" {
			t.Errorf("unexpected fixed OpenAI-compatible request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "bad model request", http.StatusBadRequest)
			return
		}
		modelCalls.Add(1)
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		if decoder.Decode(&request) != nil || request.Model != "fixed-local-citation" {
			t.Error("formal cmd/api used unexpected model request")
			http.Error(w, "bad model body", http.StatusBadRequest)
			return
		}
		var evidenceID string
		for i := len(request.Messages) - 1; i >= 0; i-- {
			if request.Messages[i].Role != "user" {
				continue
			}
			var prompt struct {
				Pack struct {
					Evidence []struct {
						ID    string `json:"evidence_id"`
						Quote string `json:"quote"`
					} `json:"evidence"`
				} `json:"fixed_evidence_pack"`
			}
			if json.Unmarshal([]byte(request.Messages[i].Content), &prompt) == nil &&
				len(prompt.Pack.Evidence) == 1 && prompt.Pack.Evidence[0].Quote == expectedQuote {
				evidenceID = prompt.Pack.Evidence[0].ID
			}
			break
		}
		if evidenceID == "" {
			t.Error("model fixture did not receive an actual RTW-accepted EvidencePack")
			http.Error(w, "missing accepted evidence", http.StatusBadRequest)
			return
		}
		content, _ := json.Marshal(map[string]any{"answer": "The published source states: " + expectedQuote,
			"citations": []string{evidenceID}})
		response := map[string]any{"id": "fixed-citation", "object": "chat.completion",
			"created": time.Now().Unix(), "model": "fixed-local-citation",
			"choices": []any{map[string]any{"index": 0,
				"message":       map[string]any{"role": "assistant", "content": string(content)},
				"finish_reason": "stop"}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(modelServer.Close)
	modelURL, modelKey, modelName := modelServer.URL+"/v1", "test-only-fixed-model-key", "fixed-local-citation"
	liveGateway := false
	if runtimePath := os.Getenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE"); runtimePath != "" {
		info, err := os.Stat(runtimePath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("DataCenter local gateway runtime must be a private regular file")
		}
		var live struct {
			SchemaVersion        string `json:"schema_version"`
			Endpoint             string `json:"endpoint"`
			AccessToken          string `json:"access_token"`
			LogicalModel         string `json:"logical_model"`
			PhysicalModel        string `json:"physical_model"`
			ModelConfigurationID string `json:"model_configuration_id"`
		}
		raw, err := os.ReadFile(runtimePath)
		if err != nil || json.Unmarshal(raw, &live) != nil ||
			live.SchemaVersion != "sea.dc.local-chat-consumer.v1" || live.AccessToken == "" ||
			live.LogicalModel != "agent" || live.PhysicalModel == "" || live.ModelConfigurationID == "" ||
			!strings.HasPrefix(live.Endpoint, "http://127.0.0.1:") {
			t.Fatal("DataCenter local gateway runtime contract differs")
		}
		modelURL, modelKey, modelName, liveGateway = live.Endpoint, live.AccessToken, live.LogicalModel, true
	}
	otlp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" {
			t.Errorf("unexpected OTLP export route: %s %s", r.Method, r.URL.Path)
			http.Error(w, "bad export", http.StatusBadRequest)
			return
		}
		traceCalls.Add(1)
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			t.Errorf("read OTLP trace request: %v", err)
			http.Error(w, "bad trace", 400)
			return
		}
		var batch otlptracepb.ExportTraceServiceRequest
		if err = proto.Unmarshal(raw, &batch); err != nil {
			t.Errorf("decode OTLP trace request: %v", err)
			http.Error(w, "bad trace", 400)
			return
		}
		for _, resource := range batch.ResourceSpans {
			for _, scoped := range resource.ScopeSpans {
				if scoped.Scope.GetName() != "trpc.agent.go" {
					continue
				}
				for _, span := range scoped.Spans {
					if span.Name == "invoke_agent search_summary_root" {
						nativeRoot.Store(true)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(otlp.Close)
	binary := filepath.Join(instanceDir, "btw-formal-search-api")
	compile := exec.Command("go", "build", "-race", "-o", binary, "./cmd/api")
	compile.Dir = btwRoot
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile formal BTW cmd/api: %v\n%s", err, output)
	}
	version, err := exec.Command("git", "-C", btwRoot, "rev-parse", "HEAD").Output()
	if err != nil || len(strings.TrimSpace(string(version))) != 40 {
		t.Fatal("BTW source SHA unavailable")
	}
	apiAddr := freeSearchSocket(t)
	metricsAddr := freeSearchSocket(t)
	process := &formalSearchAPI{URL: "http://" + apiAddr, MetricsURL: "http://" + metricsAddr,
		modelCalls: &modelCalls, liveGateway: liveGateway}
	if evidence := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidence != "" {
		process.LogEvidencePath = filepath.Join(evidence,
			"formal-search-api-"+strings.ReplaceAll(apiAddr, ":", "-")+".jsonl")
	}
	logPath := filepath.Join(instanceDir, "btw-formal-search-api.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary)
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(),
		"BTW_SEARCH_MODE="+mode, "BTW_SEARCH_API_ADDR="+apiAddr,
		"BTW_SEARCH_METRICS_ADDR="+metricsAddr, "BTW_SEARCH_SCOPE_KEY="+productFixtureScopeKey,
		"BTW_SEARCH_TOOLS_SCOPE_KEY="+toolFixtureScopeKey,
		"BTW_RTW_URL="+rtwBase, "BTW_RTW_TOKEN="+workerToken,
		"BTW_DC_URL="+dc.Endpoint, "BTW_DC_TOKEN="+dc.Token,
		"BTW_SEARCH_MODEL_URL="+modelURL,
		"BTW_SEARCH_MODEL_KEY="+modelKey, "BTW_SEARCH_MODEL_NAME="+modelName,
		"BTW_ARTIFACT_DIR="+filepath.Join(dir, "objects"), "BTW_SEARCH_INDEX_FILE="+indexFile,
		"BTW_SEARCH_POLICY_FILE="+policyFile, "BTW_SEARCH_MAX_QUOTE_RUNES=1024",
		"BTW_SEARCH_HTTP_TIMEOUT=90s", "BTW_SEARCH_REPRESENTATION_MAX_IN_FLIGHT=1",
		"BTW_OTLP_TRACES_URL="+otlp.URL+"/v1/traces",
		"BTW_SERVICE_VERSION="+strings.TrimSpace(string(version)),
		"BTW_ENVIRONMENT=test", "BTW_INSTANCE_ID=formal-cmd-api-cross")
	if mode == "native-hybrid" {
		cmd.Env = append(cmd.Env, "BTW_SEARCH_MILVUS_ADDRESS="+nativeAddress,
			"BTW_SEARCH_MILVUS_API_KEY=", "BTW_SEARCH_NATIVE_FILE="+nativeSettingsPath)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case err := <-exited:
			if err != nil {
				t.Errorf("formal cmd/api exited unsuccessfully: %v", err)
			}
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("formal cmd/api did not stop after SIGTERM")
		}
		logFile.Close()
		logs, _ := os.ReadFile(logPath)
		validateFormalSearchJSONL(t, logs, apiAddr, process.expectedSearchID, process.expectedAnswerID)
		for _, secret := range []string{workerToken, dc.Token, modelKey} {
			if secret != "" && bytes.Contains(logs, []byte(secret)) {
				t.Error("formal cmd/api log contains a configured credential")
			}
		}
		if process.served.Load() && process.LogEvidencePath != "" {
			if err := os.MkdirAll(filepath.Dir(process.LogEvidencePath), 0700); err != nil {
				t.Error(err)
			} else if err := os.WriteFile(process.LogEvidencePath, logs, 0600); err != nil {
				t.Error(err)
			}
		}
		if !bytes.Contains(logs, []byte(`"event":"search.api.started"`)) ||
			!bytes.Contains(logs, []byte(`"event":"search.api.stopped"`)) {
			t.Error("formal cmd/api omitted structured startup or shutdown events")
		}
		if process.served.Load() && traceCalls.Load() < 1 {
			t.Error("formal cmd/api did not export OTLP traces after signed search")
		}
		if process.served.Load() && !nativeRoot.Load() {
			t.Error("formal cmd/api OTLP export lacks framework-native search_summary_root span")
		}
		if process.served.Load() && !liveGateway && modelCalls.Load() != 1 {
			t.Errorf("fixed local model calls=%d want=1", modelCalls.Load())
		}
		if process.served.Load() && liveGateway && modelCalls.Load() != 0 {
			t.Error("fixed model server was used instead of DataCenter gateway")
		}
		if t.Failed() {
			t.Logf("formal cmd/api log: %s", logs)
		}
	})
	endpoint := process.URL
	client := &http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		response, err := client.Get(endpoint + "/livez")
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode == http.StatusNoContent {
			break
		}
	}
	live, err := client.Get(endpoint + "/livez")
	if err != nil {
		t.Fatalf("formal cmd/api socket unavailable: %v", err)
	}
	live.Body.Close()
	if live.StatusCode != http.StatusNoContent {
		t.Fatalf("formal cmd/api livez=%d", live.StatusCode)
	}
	unsigned, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		endpoint+"/v1/search/summary", strings.NewReader(`{"module_id":"forged","query":"x","depth":"fast","intelligence":"low"}`))
	if err != nil {
		t.Fatal(err)
	}
	unsigned.Header.Set("Content-Type", "application/json")
	denied, err := client.Do(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	deniedRaw, _ := io.ReadAll(io.LimitReader(denied.Body, 1<<20))
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden || !bytes.Contains(deniedRaw, []byte("SEARCH_SCOPE_DENIED")) || modelCalls.Load() != 0 {
		t.Fatalf("formal cmd/api unsigned request reached dependencies: status=%d model=%d", denied.StatusCode, modelCalls.Load())
	}
	metrics, err := client.Get(process.MetricsURL + "/metrics")
	if err != nil {
		t.Fatalf("formal cmd/api metrics unavailable: %v", err)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(metrics.Body, 1<<20))
	metrics.Body.Close()
	if err != nil || metrics.StatusCode != http.StatusOK {
		t.Fatalf("formal cmd/api metrics socket invalid: status=%d err=%v", metrics.StatusCode, err)
	}
	return process
}

func validateFormalSearchJSONL(t *testing.T, raw []byte, apiAddr, searchID, answerID string) {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
	if len(lines) == 0 || len(lines[0]) == 0 {
		t.Fatal("formal cmd/api wrote no structured logs")
	}
	started, stopped, summaries, runs := 0, 0, 0, 0
	var summaryTrace, runtimeTrace string
	for number, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Errorf("formal cmd/api JSONL line %d is invalid: %v", number+1, err)
			continue
		}
		for _, key := range []string{"timestamp", "level", "message", "event", "service",
			"environment", "service_version", "instance_id", "component", "log_source"} {
			if value, ok := record[key].(string); !ok || value == "" {
				t.Errorf("formal cmd/api JSONL line %d lacks %s", number+1, key)
			}
		}
		if record["service"] != "sea-btw-search-api" || record["environment"] != "test" {
			t.Errorf("formal cmd/api JSONL line %d belongs to another service/environment", number+1)
		}
		event, _ := record["event"].(string)
		if strings.HasSuffix(event, ".finished") || strings.HasSuffix(event, ".stopped") {
			if outcome, ok := record["outcome"].(string); !ok || outcome == "" {
				t.Errorf("formal cmd/api terminal JSONL line %d lacks outcome", number+1)
			}
		}
		switch event {
		case "search.api.started":
			started++
			if record["api_addr"] != apiAddr {
				t.Errorf("formal cmd/api JSONL belongs to wrong socket: got=%v want=%s", record["api_addr"], apiAddr)
			}
		case "search.api.stopped":
			stopped++
			if record["outcome"] != "succeeded" {
				t.Error("formal cmd/api did not stop cleanly")
			}
		case "search.http.summary.finished":
			if searchID != "" && record["outcome"] == "succeeded" {
				summaries++
				if record["search_id"] != searchID || record["operation_id"] != answerID ||
					record["http_status"] != float64(http.StatusOK) {
					t.Errorf("formal cmd/api terminal scope mismatch: search=%v answer=%v status=%v",
						record["search_id"], record["operation_id"], record["http_status"])
				}
				summaryTrace, _ = record["trace_id"].(string)
			}
		case "runtime.run.finished":
			if answerID != "" && record["outcome"] == "succeeded" && record["request_id"] == answerID {
				runs++
				runtimeTrace, _ = record["trace_id"].(string)
			}
		}
	}
	if started != 1 || stopped != 1 {
		t.Errorf("formal cmd/api lifecycle JSONL counts started=%d stopped=%d", started, stopped)
	}
	if searchID != "" && (summaries != 1 || runs != 1 || len(summaryTrace) != 32 ||
		summaryTrace != runtimeTrace) {
		t.Errorf("formal cmd/api signed search trace mismatch: summaries=%d runs=%d summary_trace=%s runtime_trace=%s",
			summaries, runs, summaryTrace, runtimeTrace)
	}
}

func freeSearchSocket(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
