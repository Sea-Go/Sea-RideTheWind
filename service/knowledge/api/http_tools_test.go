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

	"sea-try-go/service/knowledge/api/internal/types"
)

const toolFixtureScopeKey = "test-only-tools-scope-key-32-bytes-minimum"

type toolFixtureScope struct {
	Audience               string                   `json:"aud"`
	Subject                types.AcceptedSubjectRef `json:"subject_ref"`
	SessionID              string                   `json:"session_id"`
	OperationID            string                   `json:"operation_id"`
	BudgetRef              string                   `json:"budget_ref"`
	SearchID               string                   `json:"search_id"`
	SnapshotRef            string                   `json:"snapshot_ref"`
	Snapshot               types.SearchSnapshot     `json:"snapshot"`
	AllowPartial           bool                     `json:"allow_partial"`
	AllowLowerIntelligence bool                     `json:"allow_lower_intelligence"`
	RequestHash            string                   `json:"request_hash"`
	IssuedAtUnix           int64                    `json:"issued_at_unix"`
	ExpiresAtUnix          int64                    `json:"expires_at_unix"`
}

type toolFixtureScopeV2 struct {
	Audience               string                     `json:"aud"`
	Subject                types.AcceptedSubjectRefV2 `json:"subject_ref"`
	SessionID              string                     `json:"session_id"`
	OperationID            string                     `json:"operation_id"`
	BudgetRef              string                     `json:"budget_ref"`
	SearchID               string                     `json:"search_id"`
	SnapshotRef            string                     `json:"snapshot_ref"`
	Snapshot               types.SearchSnapshot       `json:"snapshot"`
	AllowPartial           bool                       `json:"allow_partial"`
	AllowLowerIntelligence bool                       `json:"allow_lower_intelligence"`
	RequestHash            string                     `json:"request_hash"`
	IssuedAtUnix           int64                      `json:"issued_at_unix"`
	ExpiresAtUnix          int64                      `json:"expires_at_unix"`
}

type toolSearchFixture struct {
	server     *httptest.Server
	stage      atomic.Int64 // 1: forged evidence; 0: empty fixture; 2: transparent real BTW relay.
	calls      atomic.Int64
	mu         sync.Mutex
	forwardURL string
}

func newToolSearchFixture(t *testing.T) *toolSearchFixture {
	t.Helper()
	f := &toolSearchFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search/tools/search" ||
			r.Header.Get("Content-Type") != "application/json" ||
			len(r.Header.Values("X-Sea-Search-Tools-Scope")) != 1 {
			t.Error("RTW sent invalid Tool method, path or header")
			http.Error(w, "bad transport", 400)
			return
		}
		signed := strings.Split(r.Header.Get("X-Sea-Search-Tools-Scope"), ".")
		if len(signed) != 2 {
			t.Error("missing Tool signature")
			http.Error(w, "bad signature", 400)
			return
		}
		payload, err := base64.RawURLEncoding.DecodeString(signed[0])
		if err != nil || base64.RawURLEncoding.EncodeToString(payload) != signed[0] {
			t.Error("noncanonical Tool payload")
			http.Error(w, "bad payload", 400)
			return
		}
		mac := hmac.New(sha256.New, []byte(toolFixtureScopeKey))
		_, _ = mac.Write(payload)
		signature, err := base64.RawURLEncoding.DecodeString(signed[1])
		if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
			t.Error("wrong Tool signature")
			http.Error(w, "bad signature", 400)
			return
		}
		var scope toolFixtureScope
		var aud struct {
			Audience string `json:"aud"`
		}
		if json.Unmarshal(payload, &aud) != nil {
			t.Error("malformed Tool audience")
			http.Error(w, "bad scope", 400)
			return
		}
		var canonical []byte
		if aud.Audience == "btw.search.tools.v2" {
			var wire toolFixtureScopeV2
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&wire) != nil || decoder.Decode(new(any)) != io.EOF {
				t.Error("malformed v2 Tool scope")
				http.Error(w, "bad scope", 400)
				return
			}
			canonical, _ = json.Marshal(wire)
			if wire.Subject.Issuer != "rtw.identity" || wire.Subject.SubjectId == "" {
				t.Error("wrong v2 Tool issuer or UID")
				http.Error(w, "bad scope", 400)
				return
			}
			scope = toolFixtureScope{Audience: wire.Audience,
				Subject:   types.AcceptedSubjectRef{AuthorityId: wire.Subject.Issuer, TenantId: "platform", SubjectId: wire.Subject.SubjectId},
				SessionID: wire.SessionID, OperationID: wire.OperationID, BudgetRef: wire.BudgetRef,
				SearchID: wire.SearchID, SnapshotRef: wire.SnapshotRef, Snapshot: wire.Snapshot,
				AllowPartial: wire.AllowPartial, AllowLowerIntelligence: wire.AllowLowerIntelligence,
				RequestHash: wire.RequestHash, IssuedAtUnix: wire.IssuedAtUnix, ExpiresAtUnix: wire.ExpiresAtUnix}
		} else {
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF {
				t.Error("malformed v1 Tool scope")
				http.Error(w, "bad scope", 400)
				return
			}
			canonical, _ = json.Marshal(scope)
		}
		now := time.Now().Unix()
		expectedAudience := "btw.search.tools.v1"
		if os.Getenv("KNOWLEDGE_V2_PRODUCER_SCOPE") == "1" {
			expectedAudience = "btw.search.tools.v2"
		}
		if !bytes.Equal(canonical, payload) || scope.Audience != expectedAudience ||
			scope.Subject.AuthorityId != "rtw.identity" || scope.Subject.TenantId != "platform" ||
			scope.Subject.SubjectId == "" || scope.SessionID != "tool-parent-session" ||
			scope.OperationID == "" || scope.BudgetRef == "" || scope.SearchID == "" ||
			scope.SnapshotRef == "" || scope.Snapshot.ModuleId == "" ||
			scope.IssuedAtUnix > now+30 || scope.ExpiresAtUnix <= now ||
			scope.ExpiresAtUnix-scope.IssuedAtUnix > 120 || scope.AllowPartial || scope.AllowLowerIntelligence {
			t.Error("Tool scope escaped trusted parent or publication")
			http.Error(w, "bad scope", 400)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8192))
		if err != nil {
			t.Error(err)
			http.Error(w, "bad body", 400)
			return
		}
		var body struct {
			ModuleID     string `json:"module_id"`
			Query        string `json:"query"`
			Depth        string `json:"depth"`
			Intelligence string `json:"intelligence"`
			SearchID     string `json:"search_id"`
			Limits       struct {
				ReadCalls  int `json:"read_calls"`
				QuoteRunes int `json:"quote_runes"`
			} `json:"limits"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil {
			t.Error("malformed Tool body")
			http.Error(w, "bad body", 400)
			return
		}
		canonical, _ = json.Marshal(body)
		hash := sha256.Sum256(raw)
		if !bytes.Equal(canonical, raw) || scope.RequestHash != hex.EncodeToString(hash[:]) ||
			body.ModuleID != scope.Snapshot.ModuleId || body.SearchID != scope.SearchID ||
			body.Query != "Find the current evidence" || body.Depth != "fast" || body.Intelligence != "low" ||
			body.Limits.ReadCalls != 8 || body.Limits.QuoteRunes != 8192 {
			t.Error("Tool request mismatches signed parent and limits")
			http.Error(w, "bad body", 400)
			return
		}
		if f.stage.Load() == 2 {
			f.mu.Lock()
			endpoint := f.forwardURL
			f.mu.Unlock()
			if endpoint == "" {
				t.Error("real BTW Tools endpoint unavailable")
				http.Error(w, "unavailable", 502)
				return
			}
			forwarded, e := http.NewRequestWithContext(r.Context(), http.MethodPost,
				endpoint+"/v1/search/tools/search", bytes.NewReader(raw))
			if e != nil {
				t.Error(e)
				http.Error(w, "unavailable", 502)
				return
			}
			forwarded.Header = r.Header.Clone()
			upstream, e := (&http.Client{Timeout: 30 * time.Second}).Do(forwarded)
			if e != nil {
				t.Errorf("real BTW Tools call failed: %v", e)
				http.Error(w, "unavailable", 502)
				return
			}
			defer upstream.Body.Close()
			w.Header().Set("Content-Type", upstream.Header.Get("Content-Type"))
			w.WriteHeader(upstream.StatusCode)
			if _, e = io.Copy(w, io.LimitReader(upstream.Body, 1<<20)); e != nil {
				t.Errorf("real BTW Tools reply failed: %v", e)
			}
			return
		}
		result := types.ToolSearchResult{SearchId: body.SearchID, Status: "empty",
			StopReason: "no_evidence", SnapshotRef: scope.SnapshotRef,
			RequestedIntelligence: body.Intelligence, EffectiveIntelligence: body.Intelligence,
			Evidence: []types.ToolEvidence{}, Gaps: []string{}, Conflicts: []string{},
			Usage: types.ToolUsage{}}
		if f.stage.Load() == 1 {
			result.Status = "complete"
			result.Evidence = []types.ToolEvidence{{EvidenceId: "ev_forged", RevisionId: "rev_forged",
				Locator: "paragraph:1", Quote: "forged quote", QuoteHash: strings.Repeat("0", 64), SourceKind: "source"}}
			result.PackHash = strings.Repeat("0", 64)
			result.CitationReceipt = &types.ToolReceipt{SearchId: body.SearchID,
				PackHash: result.PackHash, DurableRef: "forged_receipt"}
			result.Usage = types.ToolUsage{ReadCalls: 1, QuoteRunes: 12}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// The optional cross-repository gate starts BTW's independently compiled
// handler process. This process, not the local relay, owns Tool/Graph output.
func startRealBTWToolsServer(t *testing.T, dir, btwRoot, rtwBase, workerToken, moduleID string, candidate *types.CitationChunk, lostReply bool) string {
	t.Helper()
	readyPath := filepath.Join(dir, "btw-tools-server-url")
	fixturePath := filepath.Join(dir, "btw-tools-server-fixture.json")
	fields := map[string]any{"rtw_base": rtwBase, "worker_token": workerToken,
		"scope_key": toolFixtureScopeKey, "ready_path": readyPath, "module_id": moduleID}
	if candidate != nil {
		fields["candidate"] = map[string]string{"revision_id": candidate.RevisionId,
			"chunk_id": candidate.ChunkId, "quote_hash": candidate.TextHash}
	}
	if lostReply {
		fields["lost_reply"] = true
	}
	fixture, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "btw-tools-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "btw-tools-server.test")
	compile := exec.Command("go", "test", "-c", "-mod=readonly", "-race", "-o", binary,
		"./internal/transport/http/search")
	compile.Dir = btwRoot
	if output, err := compile.CombinedOutput(); err != nil {
		logFile.Close()
		t.Fatalf("compile real BTW Tools server: %v\n%s", err, output)
	}
	cmd := exec.Command(binary, "-test.run=^TestRTWRealToolsSearchServer$", "-test.v")
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(), "SEA_RTW_TOOLS_SERVER_FIXTURE="+fixturePath)
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
				t.Errorf("BTW Tools server exited unsuccessfully: %v", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("BTW Tools server did not stop after SIGTERM")
		}
		logFile.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logPath)
			t.Logf("BTW Tools server log: %s", log)
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
		t.Fatalf("BTW Tools ready file contained invalid endpoint: %q", endpoint)
	}
	log, _ := os.ReadFile(logPath)
	t.Fatalf("BTW Tools server did not become ready: %s", log)
	return ""
}
