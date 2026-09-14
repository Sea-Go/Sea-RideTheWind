package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/types"
)

// The parent owns an actual RTW admin HTTP/PG judgment, its original Outbox,
// and a real DC cmd/platform database. BTW receives only a disposable receipt
// and must independently read the frozen event over RTW's private HTTP API.
func runRealSearchSourceHandoff(t *testing.T, dir string, store *model.Store, rtwURL, workerToken string,
	first, withdrawn types.SearchJudgmentReceipt) {
	t.Helper()
	btwRoot, dcRoot := os.Getenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT"), os.Getenv("SEA_DC_EVENT_PLATFORM_ROOT")
	if btwRoot == "" && dcRoot == "" {
		return
	}
	if btwRoot == "" || dcRoot == "" {
		t.Fatal("real search judgment handoff requires both BTW and DC source roots")
	}
	platform := startRealDCJobPlatform(t, dir, dcRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sender := &mqs.HTTPSender{Endpoint: platform.BaseURL + "/v1/events", Token: platform.Token,
		Client: &http.Client{Timeout: 10 * time.Second}}
	delivered := 0
	for ; delivered < 128; delivered++ {
		sent, err := store.DispatchOne(ctx, sender)
		if err != nil {
			t.Fatalf("RTW Outbox to actual DC event platform: %v", err)
		}
		if !sent {
			break
		}
	}
	if delivered < 3 || delivered == 128 {
		t.Fatalf("expected bounded shared RTW producer with judgments and non-qrel events: %d", delivered)
	}
	var accepted, judgments, nonQrel int
	err := platform.Pool.QueryRow(ctx, `SELECT count(*),
 count(*) FILTER (WHERE envelope->>'event_type' IN
 ('knowledge.search.judgment.revised.v1','knowledge.search.judgment.withdrawn.v1')),
 count(*) FILTER (WHERE envelope->>'event_type' NOT IN
 ('knowledge.search.judgment.revised.v1','knowledge.search.judgment.withdrawn.v1'))
 FROM eventing.event WHERE producer='ridethewind.knowledge'`).Scan(&accepted, &judgments, &nonQrel)
	if err != nil || accepted != delivered || judgments != 2 || nonQrel < 1 {
		t.Fatalf("actual DC shared producer mismatch: accepted=%d judgments=%d other=%d delivered=%d err=%v",
			accepted, judgments, nonQrel, delivered, err)
	}
	var eventIDs int
	if err := platform.Pool.QueryRow(ctx, `SELECT count(*) FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=ANY($1)`, []string{first.EventId, withdrawn.EventId}).Scan(&eventIDs); err != nil || eventIDs != 2 {
		t.Fatalf("actual DC missing RTW judgment event IDs: count=%d err=%v", eventIDs, err)
	}
	resultPath := filepath.Join(dir, "btw-real-searchsource-result.json")
	fixtureRaw, err := json.Marshal(map[string]any{"rtw_url": rtwURL, "rtw_token": workerToken,
		"dc_url": platform.BaseURL, "dc_token": platform.Token, "expected_events": delivered,
		"judgment_event_ids": []string{first.EventId, withdrawn.EventId}, "result_path": resultPath})
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(dir, "btw-real-searchsource-fixture.json")
	if err := os.WriteFile(fixturePath, fixtureRaw, 0600); err != nil {
		t.Fatal(err)
	}
	consumer := exec.Command("bash", "internal/warehouse/searchsource/acceptance.sh")
	consumer.Dir = btwRoot
	consumer.Env = append(os.Environ(), "SEA_RTW_REAL_QREL_FIXTURE="+fixturePath)
	output, runErr := consumer.CombinedOutput()
	if runErr != nil {
		t.Fatalf("BTW real search source consumer failed: %v\n%s", runErr, output)
	}
	if testing.Verbose() {
		t.Logf("BTW real search source consumer: %s", strings.TrimSpace(string(output)))
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Committed   int64 `json:"committed_offset"`
		Acked       int64 `json:"acknowledged_offset"`
		Judgments   int   `json:"judgments"`
		OtherEvents int   `json:"technical_skips"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Committed != int64(delivered) ||
		result.Acked != int64(delivered) || result.Judgments != 2 || result.OtherEvents != nonQrel {
		t.Fatalf("BTW ODS/ACK result differs from actual DC stream: %+v err=%v", result, err)
	}
	var cursor int64
	if err := platform.Pool.QueryRow(ctx, `SELECT acknowledged_offset FROM eventing.consumer
 WHERE consumer='btw-warehouse-search-qrel' AND producer='ridethewind.knowledge'`).Scan(&cursor); err != nil || cursor != int64(delivered) {
		t.Fatalf("actual DC qrel consumer ACK missing: offset=%d err=%v", cursor, err)
	}
	t.Logf("real RTW/DC/BTW search source accepted=%d judgment_revisions=%d non_qrel=%d ods_ack=%d",
		accepted, judgments, nonQrel, cursor)
}
