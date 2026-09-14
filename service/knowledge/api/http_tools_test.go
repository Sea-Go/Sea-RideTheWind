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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"
)

const toolFixtureScopeKey = "test-only-tools-scope-key-32-bytes-minimum"

type toolSearchFixture struct {
	server *httptest.Server
	stage  atomic.Int64 // 1: forged evidence without RTW citation; 0: genuine empty result.
	calls  atomic.Int64
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
		var scope struct {
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
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope) != nil {
			t.Error("malformed Tool scope")
			http.Error(w, "bad scope", 400)
			return
		}
		canonical, _ := json.Marshal(scope)
		now := time.Now().Unix()
		if !bytes.Equal(canonical, payload) || scope.Audience != "btw.search.tools.v1" ||
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
		decoder = json.NewDecoder(bytes.NewReader(raw))
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
