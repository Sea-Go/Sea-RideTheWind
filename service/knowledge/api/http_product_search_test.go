package main

import (
	"bytes"
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
		if !bytes.Equal(raw, canonical) || scope.RequestHash != hex.EncodeToString(hash[:]) ||
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
			upstream, forwardErr := (&http.Client{Timeout: 30 * time.Second}).Do(forwarded)
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
		turnRaw, _ := json.Marshal(map[string]any{
			"Request": map[string]any{"SearchID": scope.SearchID, "AnswerID": scope.AnswerID,
				"Subject": scope.Subject, "SessionID": scope.SessionID,
				"Search": map[string]any{"Query": search.Query, "Depth": search.Depth,
					"Intelligence": search.Intelligence, "Snapshot": scope.Snapshot}},
			"result": map[string]any{"search": map[string]any{"evidence_pack": json.RawMessage(packRaw)},
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
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&scope); err != nil {
		t.Error("malformed RTW scope payload")
		return scope, false
	}
	canonical, _ := json.Marshal(scope)
	now := time.Now().Unix()
	if !bytes.Equal(payload, canonical) || scope.Audience != "btw.search.summary.v1" ||
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
func startRealBTWProductServer(t *testing.T, dir, btwRoot, rtwBase, workerToken string) string {
	t.Helper()
	readyPath := filepath.Join(dir, "btw-product-server-url")
	fixturePath := filepath.Join(dir, "btw-product-server-fixture.json")
	fixture, err := json.Marshal(map[string]string{"rtw_base": rtwBase, "worker_token": workerToken,
		"scope_key": productFixtureScopeKey, "ready_path": readyPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "btw-product-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "btw-product-server.test")
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
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
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
