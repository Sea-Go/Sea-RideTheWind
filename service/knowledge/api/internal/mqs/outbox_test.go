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

func TestOutboxReceiptLossAndConcurrentSenders(t *testing.T) {
	s := testenv.Store(t)
	ctx := context.Background()
	m, err := s.CreateModule(ctx, "admin", types.CreateModuleReq{Title: "Outbox", IdempotencyKey: "module"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateSource(ctx, "admin", types.CreateSourceReq{ModuleId: m.Id, Title: "A", Content: "A", MediaType: "text/plain", Provenance: "synthetic", IdempotencyKey: "source"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	attempts := 0
	logicalEffects := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e model.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Idempotency-Key") != e.EventID || e.Producer != "ridethewind.knowledge" {
			t.Error("event envelope or idempotency mismatch")
		}
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if seen[e.EventID] == 0 {
			logicalEffects++
		}
		seen[e.EventID]++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		} // Durable receiver accepted, but its receipt was lost.
		json.NewEncoder(w).Encode(model.TechnicalReceipt{EventID: e.EventID, TechnicalStatus: "accepted"})
	}))
	defer receiver.Close()
	sender := &mqs.HTTPSender{Endpoint: receiver.URL, Client: &http.Client{Timeout: time.Second}}
	_, err = s.DispatchOne(ctx, sender)
	if err == nil {
		t.Fatal("lost receipt marked delivered")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.DispatchOne(ctx, sender); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if logicalEffects != 1 || attempts != 2 {
		t.Fatalf("effects=%d attempts=%d", logicalEffects, attempts)
	}
	var pending int
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE delivered_at IS NULL").Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
}
