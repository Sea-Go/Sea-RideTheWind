package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/types"
)

const productFixtureScopeKey = "test-only-search-scope-key-32-bytes-minimum"

// The optional real-index handoff is private task data. The DC runtime file
// contains only disposable local credentials and is never copied into Git.
type realIndexSetup struct {
	DCRuntime       string            `json:"dc_runtime"`
	NativeRuntime   string            `json:"native_runtime,omitempty"`
	ArtifactDir     string            `json:"artifact_dir"`
	ChunkManifest   model.ArtifactRef `json:"chunk_manifest"`
	BuildID         string            `json:"build_id"`
	ReleaseID       string            `json:"release_id"`
	Generation      int64             `json:"generation"`
	ResultPath      string            `json:"result_path"`
	ExpectedQuote   string            `json:"expected_quote"`
	ExpectedChunkID string            `json:"expected_chunk_id"`
}

type realIndexResult struct {
	IndexManifest    model.ArtifactRef            `json:"index_manifest"`
	Indexes          map[string]model.ArtifactRef `json:"indexes"`
	ChunkCount       int                          `json:"chunk_count"`
	APIIndexSettings json.RawMessage              `json:"api_index_settings"`
	NativeProjection *realNativeProjection        `json:"native_projection,omitempty"`
}

// This is an isolated Hybrid receipt: Lite owns Dense/Multi, the original
// learned-IP Sparse postings keep their exact Ref. RTW's IndexManifest v1
// does not sign physical settings, so it cannot qualify production READY.
type realNativeProjection struct {
	Status              string          `json:"status"`
	Endpoint            string          `json:"endpoint"`
	RuntimeSHA256       string          `json:"runtime_sha256"`
	EnginePackageSHA256 string          `json:"engine_package_sha256"`
	Settings            json.RawMessage `json:"settings"`
	SettingsJCSSHA256   string          `json:"settings_jcs_sha256"`
	PhysicalQualified   bool            `json:"physical_qualified"`
}

// The builder child produces actual local-exact BGE-M3 artifacts and exits;
// the explicit Hybrid fixture also projects Dense/Multi before RTW READY.
// Search later runs only in the independently launched formal cmd/api binary.
func buildRealBTWIndexesOnly(t *testing.T, dir, btwRoot, rtwBase, workerToken, moduleID string,
	setup *realIndexSetup) string {
	t.Helper()
	fixturePath := filepath.Join(dir, "btw-formal-index-fixture.json")
	readyPath := filepath.Join(dir, "btw-formal-index-unused-ready")
	fixture, err := json.Marshal(map[string]any{"build_only": true, "rtw_base": rtwBase,
		"worker_token": workerToken, "scope_key": productFixtureScopeKey,
		"ready_path": readyPath, "module_id": moduleID, "real_index": setup})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "btw-formal-index-builder.test")
	compile := exec.Command("go", "test", "-c", "-mod=readonly", "-race", "-o", binary,
		"./internal/transport/http/search")
	compile.Dir = btwRoot
	compileOutput, compileErr := compile.CombinedOutput()
	if setup.NativeRuntime != "" {
		persistNativeTestBytes(t, "native-builder-compile.log", compileOutput)
	}
	if compileErr != nil {
		if setup.NativeRuntime == "" {
			t.Fatalf("compile BTW real-index builder: %v\n%s", compileErr, compileOutput)
		}
		t.Fatalf("compile BTW real-index builder: %v; private log retained", compileErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestRTWRealProductSearchServer$", "-test.v")
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(), "SEA_RTW_PRODUCT_SERVER_FIXTURE="+fixturePath)
	buildOutput, buildErr := cmd.CombinedOutput()
	if setup.NativeRuntime != "" {
		persistNativeTestBytes(t, "native-builder-child.log", buildOutput)
	}
	if buildErr != nil {
		if setup.NativeRuntime == "" {
			t.Fatalf("BTW real three-lane index builder failed: %v\n%s", buildErr,
				strings.ReplaceAll(string(buildOutput), workerToken, "[REDACTED]"))
		}
		t.Fatalf("BTW real three-lane index builder failed: %v; private log retained", buildErr)
	}
	return binary
}

func realBGEProfiles(t *testing.T, runtimePath string) []types.RetrievalProfile {
	t.Helper()
	info, err := os.Stat(runtimePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("DataCenter BGE runtime must be a private regular file: %v", err)
	}
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var runtime struct {
		Endpoint       string `json:"endpoint"`
		Configurations map[string]struct {
			Profile struct {
				Model    string `json:"model"`
				Space    string `json:"representation_space"`
				Contract struct {
					Kind        string `json:"kind"`
					Dimensions  int    `json:"dimensions"`
					TokenizerID string `json:"tokenizer_id"`
					Aggregation string `json:"aggregation"`
				} `json:"representation_contract"`
			} `json:"profile"`
		} `json:"configurations"`
	}
	if err = json.Unmarshal(raw, &runtime); err != nil || runtime.Endpoint == "" || len(runtime.Configurations) != 3 {
		t.Fatal("incomplete actual DataCenter BGE runtime")
	}
	profiles := make([]types.RetrievalProfile, 0, 3)
	for _, kind := range []string{"dense", "sparse", "token_matrix"} {
		wire, ok := runtime.Configurations[kind]
		if !ok || wire.Profile.Model == "" || wire.Profile.Space == "" ||
			wire.Profile.Contract.Kind != kind || wire.Profile.Contract.TokenizerID == "" || wire.Profile.Contract.Dimensions < 1 {
			t.Fatalf("DataCenter BGE %s profile incomplete", kind)
		}
		lane := kind
		if kind == "token_matrix" {
			lane = "multivector"
		}
		p := types.RetrievalProfile{Lane: lane, Encoder: wire.Profile.Model,
			Tokenizer: wire.Profile.Contract.TokenizerID, Space: wire.Profile.Space,
			Dimensions: wire.Profile.Contract.Dimensions}
		if kind == "token_matrix" {
			p.Mask, p.Aggregation = "valid", wire.Profile.Contract.Aggregation
			if p.Aggregation != "mean_maxsim" {
				t.Fatal("actual BGE ColBERT profile must retain mean_maxsim")
			}
		}
		profiles = append(profiles, p)
	}
	return profiles
}

type productFixtureScope struct {
	Audience               string                   `json:"aud"`
	Subject                types.AcceptedSubjectRef `json:"subject_ref"`
	SessionID              string                   `json:"session_id"`
	SearchID               string                   `json:"search_id"`
	AnswerID               string                   `json:"answer_id"`
	Snapshot               types.SearchSnapshot     `json:"snapshot"`
	AllowPartial           bool                     `json:"allow_partial"`
	AllowLowerIntelligence bool                     `json:"allow_lower_intelligence"`
	RequestHash            string                   `json:"request_hash"`
	IssuedAtUnix           int64                    `json:"issued_at_unix"`
	ExpiresAtUnix          int64                    `json:"expires_at_unix"`
}

type productFixtureScopeV2 struct {
	Audience               string                     `json:"aud"`
	Subject                types.AcceptedSubjectRefV2 `json:"subject_ref"`
	SessionID              string                     `json:"session_id"`
	SearchID               string                     `json:"search_id"`
	AnswerID               string                     `json:"answer_id"`
	Snapshot               types.SearchSnapshot       `json:"snapshot"`
	AllowPartial           bool                       `json:"allow_partial"`
	AllowLowerIntelligence bool                       `json:"allow_lower_intelligence"`
	RequestHash            string                     `json:"request_hash"`
	IssuedAtUnix           int64                      `json:"issued_at_unix"`
	ExpiresAtUnix          int64                      `json:"expires_at_unix"`
}

type productSearchFixture struct {
	server     *httptest.Server
	stage      atomic.Int64 // 0: forged 200; 1: committed but lost reply; 2: committed 200; 3: blocked failure; 4: transparent real BTW relay
	calls      atomic.Int64
	mu         sync.Mutex
	scopes     []productFixtureScope
	forwardURL string
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func newProductSearchFixture(t *testing.T, rtwBase *string, workerToken string) *productSearchFixture {
	t.Helper()
	f := &productSearchFixture{entered: make(chan struct{}), release: make(chan struct{})}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search/summary" ||
			r.Header.Get("Content-Type") != "application/json" || len(r.Header.Values("X-Sea-Search-Scope")) != 1 {
			t.Errorf("RTW sent noncanonical BTW method, path or headers")
			http.Error(w, "bad request", 400)
			return
		}
		scope, ok := decodeProductFixtureScope(t, r.Header.Get("X-Sea-Search-Scope"))
		if !ok {
			http.Error(w, "bad scope", 400)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8192))
		if err != nil {
			t.Error(err)
			http.Error(w, "bad body", 400)
			return
		}
		var search model.ProductSearchInput
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&search); err != nil {
			t.Errorf("RTW sent unexpected BTW body: %v", err)
			http.Error(w, "bad body", 400)
			return
		}
		var tail any
		if decoder.Decode(&tail) != io.EOF {
			t.Error("RTW sent trailing BTW body")
			http.Error(w, "bad body", 400)
			return
		}
		canonical, _ := json.Marshal(search)
		hash := sha256.Sum256(canonical)
		expectedAudience := "btw.search.summary.v1"
		if os.Getenv("KNOWLEDGE_V2_PRODUCER_SCOPE") == "1" {
			expectedAudience = "btw.search.summary.v2"
		}
		if !bytes.Equal(raw, canonical) || scope.RequestHash != hex.EncodeToString(hash[:]) ||
			scope.Audience != expectedAudience ||
			scope.Snapshot.ModuleId != search.ModuleID || scope.Subject.AuthorityId != "rtw.identity" ||
			scope.Subject.TenantId != "platform" || scope.Subject.SubjectId == "" ||
			scope.SessionID != "search-facade-session" || scope.AllowPartial || scope.AllowLowerIntelligence {
			t.Error("RTW signed a noncanonical request or unexpected product scope")
			http.Error(w, "bad scope", 400)
			return
		}
		f.mu.Lock()
		f.scopes = append(f.scopes, scope)
		forwardURL := f.forwardURL
		f.mu.Unlock()
		if f.stage.Load() == 4 {
			forwarded, forwardErr := http.NewRequestWithContext(r.Context(), http.MethodPost,
				forwardURL+"/v1/search/summary", bytes.NewReader(raw))
			if forwardErr != nil || forwardURL == "" {
				t.Error("real BTW forwarding endpoint unavailable")
				http.Error(w, "real BTW unavailable", http.StatusBadGateway)
				return
			}
			forwarded.Header = r.Header.Clone()
			// A live DataCenter model run includes cold model loads; keep the
			// fixed-fixture pace for local-exact runs.
			forwardTimeout := 30 * time.Second
			if os.Getenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE") != "" {
				forwardTimeout = 95 * time.Second
			}
			upstream, forwardErr := (&http.Client{Timeout: forwardTimeout}).Do(forwarded)
			if forwardErr != nil {
				t.Errorf("real BTW request failed: %v", forwardErr)
				http.Error(w, "real BTW unavailable", http.StatusBadGateway)
				return
			}
			defer upstream.Body.Close()
			w.Header().Set("Content-Type", upstream.Header.Get("Content-Type"))
			w.WriteHeader(upstream.StatusCode)
			if _, forwardErr = io.Copy(w, io.LimitReader(upstream.Body, 1<<20)); forwardErr != nil {
				t.Errorf("copy real BTW terminal result: %v", forwardErr)
			}
			return
		}
		result := types.ProductSearchResult{SearchId: scope.SearchID, AnswerId: scope.AnswerID,
			Status: "insufficient", Citations: []types.ProductSearchCitation{}}
		if f.stage.Load() == 0 {
			_ = json.NewEncoder(w).Encode(result) // Forged success without any RTW acceptance.
			return
		}
		if f.stage.Load() == 3 {
			f.once.Do(func() { close(f.entered) })
			<-f.release
			http.Error(w, "blocked call failed", 502)
			return
		}
		packRaw, _ := json.Marshal(map[string]any{"search_id": scope.SearchID, "snapshot": scope.Snapshot,
			"status": "empty", "evidence": []any{}})
		// This double stands in for BTW's boundary commit; the turn must
		// satisfy BTW's whole-turn contract because later same-session runs
		// re-read every accepted turn before their next model call.
		turnRaw, _ := json.Marshal(map[string]any{
			"Request": map[string]any{"SearchID": scope.SearchID, "AnswerID": scope.AnswerID,
				"Subject": scope.Subject, "SessionID": scope.SessionID,
				"Search": map[string]any{"Query": search.Query, "Depth": search.Depth,
					"Intelligence": search.Intelligence, "Snapshot": scope.Snapshot}},
			"result": map[string]any{"search": map[string]any{
				"retrieval": map[string]any{"status": "empty", "stop_reason": "no_evidence",
					"profile": map[string]any{"requested_depth": search.Depth, "effective_depth": search.Depth,
						"requested_intelligence": search.Intelligence, "effective_intelligence": search.Intelligence,
						"policy_version": "rtw-fixture-recovery-v1"},
					"snapshot": scope.Snapshot, "used_subqueries": 1},
				"evidence_pack": json.RawMessage(packRaw)},
				"answer_id": scope.AnswerID, "answer": "", "citations": []string{}, "summary_status": "insufficient"},
		})
		commit, _ := json.Marshal(types.CommitAcceptedAnswerReq{AnswerId: scope.AnswerID,
			SearchId: scope.SearchID, Subject: scope.Subject, SessionId: scope.SessionID, TurnJson: string(turnRaw)})
		request, err := http.NewRequest(http.MethodPost, *rtwBase+"/internal/v1/knowledge/accepted-answers", bytes.NewReader(commit))
		if err != nil {
			t.Error(err)
			http.Error(w, "commit failed", 502)
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+workerToken)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			http.Error(w, "commit failed", 502)
			return
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Errorf("fixture BTW commit status=%d", response.StatusCode)
			http.Error(w, "commit failed", 502)
			return
		}
		if f.stage.Load() == 1 {
			http.Error(w, "reply lost after durable commit", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func decodeProductFixtureScope(t *testing.T, header string) (productFixtureScope, bool) {
	t.Helper()
	var scope productFixtureScope
	parts := strings.Split(header, ".")
	if len(parts) != 2 {
		t.Error("missing scope signature")
		return scope, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		t.Error("noncanonical scope payload")
		return scope, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, []byte(productFixtureScopeKey))
	_, _ = mac.Write(payload)
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		t.Error("invalid RTW scope signature")
		return scope, false
	}
	var audience struct {
		Audience string `json:"aud"`
	}
	if err = json.Unmarshal(payload, &audience); err != nil {
		t.Error("malformed RTW scope audience")
		return scope, false
	}
	var canonical []byte
	if audience.Audience == "btw.search.summary.v2" {
		var wire productFixtureScopeV2
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&wire); err != nil || decoder.Decode(new(any)) != io.EOF {
			t.Error("malformed RTW v2 scope payload")
			return scope, false
		}
		canonical, _ = json.Marshal(wire)
		if wire.Subject.Issuer != "rtw.identity" || wire.Subject.SubjectId == "" {
			t.Error("RTW v2 scope has wrong issuer or UID")
			return scope, false
		}
		scope = productFixtureScope{Audience: wire.Audience,
			Subject:   types.AcceptedSubjectRef{AuthorityId: wire.Subject.Issuer, TenantId: "platform", SubjectId: wire.Subject.SubjectId},
			SessionID: wire.SessionID, SearchID: wire.SearchID, AnswerID: wire.AnswerID,
			Snapshot: wire.Snapshot, AllowPartial: wire.AllowPartial,
			AllowLowerIntelligence: wire.AllowLowerIntelligence, RequestHash: wire.RequestHash,
			IssuedAtUnix: wire.IssuedAtUnix, ExpiresAtUnix: wire.ExpiresAtUnix}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&scope); err != nil || decoder.Decode(new(any)) != io.EOF {
			t.Error("malformed RTW v1 scope payload")
			return scope, false
		}
		canonical, _ = json.Marshal(scope)
	}
	now := time.Now().Unix()
	if !bytes.Equal(payload, canonical) ||
		(scope.Audience != "btw.search.summary.v1" && scope.Audience != "btw.search.summary.v2") ||
		scope.IssuedAtUnix > now+30 || scope.IssuedAtUnix < now-300 ||
		scope.ExpiresAtUnix <= now || scope.ExpiresAtUnix-scope.IssuedAtUnix > 300 {
		t.Error("noncanonical, stale or wrong-audience scope")
		return scope, false
	}
	return scope, true
}

// startRealBTWProductServer starts an independently compiled BTW test process.
// The existing fixture remains a byte-preserving relay, so its test stages
// cannot fabricate an accepted turn for this path.
func startRealBTWProductServer(t *testing.T, dir, btwRoot, rtwBase, workerToken, moduleID string,
	candidate *types.CitationChunk, real *realIndexSetup) string {
	t.Helper()
	variant := "empty"
	if candidate != nil {
		variant = "cited"
	}
	readyPath := filepath.Join(dir, "btw-product-"+variant+"-server-url")
	fixturePath := filepath.Join(dir, "btw-product-"+variant+"-server-fixture.json")
	fixture, err := json.Marshal(map[string]string{"rtw_base": rtwBase, "worker_token": workerToken,
		"scope_key": productFixtureScopeKey, "ready_path": readyPath, "module_id": moduleID})
	if candidate != nil {
		values := map[string]any{"rtw_base": rtwBase, "worker_token": workerToken,
			"scope_key": productFixtureScopeKey, "ready_path": readyPath, "module_id": moduleID,
			"candidate": map[string]string{"revision_id": candidate.RevisionId,
				"chunk_id": candidate.ChunkId, "quote_hash": candidate.TextHash}}
		if real != nil {
			values["real_index"] = real
		}
		if os.Getenv("SEA_BTW_SEARCH_HISTORY_ROUND") == "1" && real == nil {
			// The explicit-budget injection gate: the child re-reads RTW's
			// accepted turns before its second same-session model call.
			values["history"] = map[string]any{
				"budget":      map[string]int{"max_turns": 4, "max_bytes": 8192},
				"result_path": filepath.Join(dir, "btw-product-cited-history-receipt.json"),
			}
		}
		fixture, err = json.Marshal(values)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "btw-product-"+variant+"-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "btw-product-"+variant+"-server.test")
	compile := exec.Command("go", "test", "-c", "-mod=readonly", "-race", "-o", binary,
		"./internal/transport/http/search")
	compile.Dir = btwRoot
	if output, err := compile.CombinedOutput(); err != nil {
		logFile.Close()
		t.Fatalf("compile real BTW product server: %v\n%s", err, output)
	}
	cmd := exec.Command(binary, "-test.run=^TestRTWRealProductSearchServer$", "-test.v")
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(), "SEA_RTW_PRODUCT_SERVER_FIXTURE="+fixturePath)
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
				t.Errorf("BTW product server exited unsuccessfully: %v", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("BTW product server did not stop after SIGTERM")
		}
		logFile.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logPath)
			t.Logf("BTW product server log: %s", log)
		}
	})
	startup := 45 * time.Second
	if real != nil {
		startup = 4 * time.Minute // actual CPU BGE encoding and all three self probes
	}
	for deadline := time.Now().Add(startup); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		contents, err := os.ReadFile(readyPath)
		if err != nil {
			continue
		}
		endpoint := strings.TrimSpace(string(contents))
		parsed, parseErr := url.Parse(endpoint)
		if parseErr == nil && parsed.Scheme == "http" && parsed.Host != "" && parsed.Path == "" {
			return endpoint
		}
		t.Fatalf("BTW ready file contained invalid endpoint: %q", endpoint)
	}
	log, _ := os.ReadFile(logPath)
	t.Fatalf("BTW product server did not become ready: %s", log)
	return ""
}
