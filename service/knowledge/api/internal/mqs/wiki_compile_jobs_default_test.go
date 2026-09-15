package mqs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"
)

func wikiCompileSource(t *testing.T, s *model.Store) types.Compile {
	t.Helper()
	ctx := context.Background()
	m, err := s.CreateModule(ctx, "admin", types.CreateModuleReq{Title: "default H04", IdempotencyKey: "module"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateSource(ctx, "admin", types.CreateSourceReq{ModuleId: m.Id,
		Title: "r1", Content: "frozen", MediaType: "text/plain", Provenance: "synthetic",
		IdempotencyKey: "source"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCompile(ctx, "admin", types.CreateCompileReq{ModuleId: m.Id,
		PageId: "page", SourceRevisionIds: []string{r.RevisionId}, Guidance: "use only r1",
		IdempotencyKey: "compile"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// An omitted WikiCompileJobs option preserves the original H04 Eventing
// route, including the existing compile.requested Outbox body. Explicitly
// enabling the Jobs lane excludes that event from the ordinary sender.
func TestWikiCompileJobDefaultOffKeepsOriginalEventSender(t *testing.T) {
	base := testenv.Store(t)
	c := wikiCompileSource(t, base)
	var observed []string
	var observedMu sync.Mutex
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e model.Event
		if r.Header.Get("Idempotency-Key") == "" ||
			json.NewDecoder(r.Body).Decode(&e) != nil ||
			r.Header.Get("Idempotency-Key") != e.EventID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		observedMu.Lock()
		observed = append(observed, e.EventType)
		observedMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(model.TechnicalReceipt{EventID: e.EventID, TechnicalStatus: "accepted"})
	}))
	defer receiver.Close()
	sender := &mqs.HTTPSender{Endpoint: receiver.URL,
		Client: &http.Client{Timeout: time.Second}}
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		sent, err := base.DispatchOne(ctx, sender)
		if err != nil {
			t.Fatal(err)
		}
		if !sent {
			break
		}
	}
	requested := 0
	observedMu.Lock()
	for _, eventType := range observed {
		if eventType == "knowledge.wiki.compile.requested.v1" {
			requested++
		}
	}
	observedMu.Unlock()
	if requested != 1 {
		t.Fatalf("default H04 sender changed frozen Compile route: %v", observed)
	}
	var technicalJobs int64
	if err := base.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_compile_jobs").Scan(&technicalJobs); err != nil || technicalJobs != 0 {
		t.Fatalf("default H04 sender fabricated a Wiki DC Job: %d %v", technicalJobs, err)
	}
	if persisted, err := base.GetCompile(ctx, c.CompileId); err != nil || persisted.State != "BUILDING" {
		t.Fatalf("H04 receipt changed RTW Compile domain state: %+v %v", persisted, err)
	}
	separate := testenv.Store(t)
	wikiCompileSource(t, separate)
	candidate := model.New(separate.DB, separate.Objects, model.WithWikiCompileJobs())
	if err := candidate.CheckWikiCompileJobCandidate(ctx); err != nil {
		t.Fatal(err)
	}
	observedMu.Lock()
	observed = nil
	observedMu.Unlock()
	for i := 0; i < 8; i++ {
		sent, err := candidate.DispatchOne(ctx, sender)
		if err != nil {
			t.Fatal(err)
		}
		if !sent {
			break
		}
	}
	observedMu.Lock()
	for _, eventType := range observed {
		if eventType == "knowledge.wiki.compile.requested.v1" {
			observedMu.Unlock()
			t.Fatalf("enabled Jobs lane sent Compile a second time to H04: %v", observed)
		}
	}
	observedMu.Unlock()
	var pending int64
	if err := candidate.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_outbox
	 WHERE event_type='knowledge.wiki.compile.requested.v1' AND delivered_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("enabled Jobs lane lost its own pending source Outbox: %d %v", pending, err)
	}
}
