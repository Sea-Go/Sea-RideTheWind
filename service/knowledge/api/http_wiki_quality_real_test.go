package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/types"
)

// This explicitly enabled parent keeps the actual RTW admin HTTP, Wiki/Source
// PG, Outbox and real DC cmd/platform alive while one outside BTW consumer
// verifies original quality Event bytes, shared producer offsets and ODS ACK.
// It does not certify FactSet completeness or a product D07 quality rate.
func runRealWikiQualityHandoff(t *testing.T, dir string, store *model.Store,
	rtwURL, workerToken, moduleID, manualWikiID, publishedReleaseID string,
	manual, ai types.WikiFactJudgmentRecord) {
	t.Helper()
	btwRoot, dcRoot := os.Getenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT"), os.Getenv("SEA_DC_EVENT_PLATFORM_ROOT")
	// Select only this Wiki source Holder; DC's root is shared by other modes.
	if btwRoot == "" {
		return
	}
	if dcRoot == "" {
		t.Fatal("real Wiki quality source handoff needs both fixed BTW and DC checkouts")
	}
	if manual.WikiOriginKind != "manual_revision" || ai.WikiOriginKind != "ai_accepted" ||
		manual.WikiRevisionId == ai.WikiRevisionId || manual.FactId != ai.FactId ||
		manual.EventId == "" || ai.EventId == "" || manual.EventId == ai.EventId {
		t.Fatal("two human judgments did not pin one Source Fact across AI/manual Wiki revisions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	quality := model.New(store.DB, store.Objects, model.WithWikiQualityJudgments())
	originalEvents := map[string]string{}
	for _, judgment := range []types.WikiFactJudgmentRecord{manual, ai} {
		wire, err := quality.GetWikiQualityEvent(ctx, judgment.EventId)
		if err != nil || wire.EventRawSha256 != judgment.EventRawSha256 ||
			wire.EventJcsSha256 != judgment.EventJcsSha256 || wire.EventJson == "" {
			t.Fatalf("RTW private quality source differs before DC transport: event=%s err=%v", judgment.EventId, err)
		}
		originalEvents[judgment.EventId] = wire.EventJson
	}
	beforeRelease, err := store.Current(ctx, moduleID)
	if err != nil || beforeRelease.ActiveReleaseId != publishedReleaseID || beforeRelease.PointerRevision < 1 {
		t.Fatalf("quality handoff did not start with an active manual Release: %+v %v", beforeRelease, err)
	}
	platform := startRealDCJobPlatform(t, dir, dcRoot)
	sender := &mqs.HTTPSender{Endpoint: platform.BaseURL + "/v1/events", Token: platform.Token,
		Client: &http.Client{Timeout: 10 * time.Second}}
	delivered := 0
	for ; delivered < 256; delivered++ {
		sent, err := store.DispatchOne(ctx, sender)
		if err != nil {
			t.Fatalf("RTW Outbox to actual shared DC Eventing: %v", err)
		}
		if !sent {
			break
		}
	}
	if delivered < 3 || delivered == 256 {
		t.Fatalf("shared RTW producer did not yield two quality and other events: %d", delivered)
	}
	var accepted, judgments, other int
	var firstOffset, lastOffset int64
	err = platform.Pool.QueryRow(ctx, `SELECT count(*), min(offset_id), max(offset_id),
 count(*) FILTER (WHERE envelope->>'event_type'='knowledge.wiki.quality.judged.v1'),
 count(*) FILTER (WHERE envelope->>'event_type'<>'knowledge.wiki.quality.judged.v1')
 FROM eventing.event WHERE producer='ridethewind.knowledge'`).Scan(
		&accepted, &firstOffset, &lastOffset, &judgments, &other)
	if err != nil || accepted != delivered || firstOffset != 1 || lastOffset != int64(delivered) ||
		judgments != 2 || other < 1 {
		t.Fatalf("DC source is not one contiguous quality prefix: accepted=%d first=%d last=%d judged=%d other=%d delivered=%d err=%v",
			accepted, firstOffset, lastOffset, judgments, other, delivered, err)
	}
	ids := []string{manual.EventId, ai.EventId}
	var exactEvents int
	if err := platform.Pool.QueryRow(ctx, `SELECT count(*) FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=ANY($1)`, ids).Scan(&exactEvents); err != nil || exactEvents != 2 {
		t.Fatalf("DC lost original RTW human quality EventIDs: count=%d err=%v", exactEvents, err)
	}
	resultPath := filepath.Join(dir, "btw-real-wiki-quality-result.json")
	fixturePath := filepath.Join(dir, "btw-real-wiki-quality-fixture.json")
	fixture, err := json.Marshal(struct {
		RTWURL          string   `json:"rtw_url"`
		RTWToken        string   `json:"rtw_token"`
		DCURL           string   `json:"dc_url"`
		DCToken         string   `json:"dc_token"`
		ExpectedEvents  int      `json:"expected_events"`
		QualityEventIDs []string `json:"quality_event_ids"`
		ResultPath      string   `json:"result_path"`
	}{rtwURL, workerToken, platform.BaseURL, platform.Token, delivered, ids, resultPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, append(fixture, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	consumer := exec.CommandContext(ctx, "bash", "internal/warehouse/wikiqualitysource/acceptance.sh")
	consumer.Dir = btwRoot
	consumer.Env = append(os.Environ(), "SEA_RTW_REAL_WIKI_QUALITY_FIXTURE="+fixturePath)
	output, consumerErr := consumer.CombinedOutput()
	outputPath := filepath.Join(dir, "btw-real-wiki-quality-consumer.log")
	if err := os.WriteFile(outputPath, output, 0600); err != nil {
		t.Fatal(err)
	}
	if consumerErr != nil {
		t.Fatalf("BTW quality ODS real-source consumer failed: %v; task-log=%s", consumerErr, outputPath)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Committed       int64    `json:"committed_offset"`
		Acknowledged    int64    `json:"acknowledged_offset"`
		Judgments       int      `json:"judgments"`
		TechnicalSkips  int      `json:"technical_skips"`
		QualityEventIDs []string `json:"quality_event_ids"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Committed != int64(delivered) ||
		result.Acknowledged != int64(delivered) || result.Judgments != 2 ||
		result.TechnicalSkips != other || !sameQualityEventIDs(result.QualityEventIDs, ids) {
		t.Fatalf("BTW Wiki quality ODS/result/ACK differs from DC prefix: %+v", result)
	}
	var cursor int64
	if err := platform.Pool.QueryRow(ctx, `SELECT acknowledged_offset FROM eventing.consumer
 WHERE consumer='btw-warehouse-wiki-quality' AND producer='ridethewind.knowledge'`).Scan(&cursor); err != nil || cursor != int64(delivered) {
		t.Fatalf("DC quality consumer did not ACK original contiguous source prefix: %d %v", cursor, err)
	}
	for _, judgment := range []types.WikiFactJudgmentRecord{manual, ai} {
		after, err := quality.GetWikiQualityEvent(ctx, judgment.EventId)
		if err != nil || after.EventJson != originalEvents[judgment.EventId] ||
			after.EventRawSha256 != judgment.EventRawSha256 ||
			after.EventJcsSha256 != judgment.EventJcsSha256 {
			t.Fatalf("transport/ODS altered original RTW quality Event bytes: event=%s err=%v", judgment.EventId, err)
		}
	}
	var manualHead, aiHead string
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, manual.PageId).Scan(&manualHead); err != nil || manualHead != manualWikiID {
		t.Fatalf("quality transport moved manual page editing head: %s %v", manualHead, err)
	}
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, ai.PageId).Scan(&aiHead); err != nil || aiHead != ai.WikiRevisionId {
		t.Fatalf("quality transport moved AI accepted page editing head: %s %v", aiHead, err)
	}
	afterRelease, err := store.Current(ctx, moduleID)
	if err != nil || afterRelease.PointerRevision != beforeRelease.PointerRevision ||
		afterRelease.ActiveReleaseId != beforeRelease.ActiveReleaseId ||
		afterRelease.ActiveBuildId != beforeRelease.ActiveBuildId {
		t.Fatalf("human quality source/ODS changed manual publication pointer: before=%+v after=%+v err=%v",
			beforeRelease, afterRelease, err)
	}
	if evidenceDir := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0700); err != nil {
			t.Fatal(err)
		}
		report, err := json.Marshal(struct {
			SchemaVersion      string   `json:"schema_version"`
			RTWEditingHead     string   `json:"rtw_editing_head"`
			ManualWikiRevision string   `json:"manual_wiki_revision_id"`
			AIWikiRevision     string   `json:"ai_wiki_revision_id"`
			FactID             string   `json:"fact_id"`
			QualityEventIDs    []string `json:"quality_event_ids"`
			EventRawSHA256     []string `json:"event_raw_sha256"`
			EventJCSSHA256     []string `json:"event_jcs_sha256"`
			DCSourceEvents     int      `json:"dc_source_events"`
			Judgments          int      `json:"judgments"`
			TechnicalSkips     int      `json:"technical_skips"`
			ODSAckOffset       int64    `json:"ods_ack_offset"`
			PublishedRelease   string   `json:"published_release_id"`
			PointerRevision    int64    `json:"pointer_revision"`
			FactSetComplete    bool     `json:"fact_set_complete"`
			ProductionVerified bool     `json:"production_verified"`
		}{SchemaVersion: "sea.wiki.quality-cross-source.v1", RTWEditingHead: manualHead,
			ManualWikiRevision: manual.WikiRevisionId, AIWikiRevision: ai.WikiRevisionId,
			FactID: manual.FactId, QualityEventIDs: ids,
			EventRawSHA256: []string{manual.EventRawSha256, ai.EventRawSha256},
			EventJCSSHA256: []string{manual.EventJcsSha256, ai.EventJcsSha256},
			DCSourceEvents: accepted, Judgments: judgments, TechnicalSkips: other,
			ODSAckOffset: cursor, PublishedRelease: afterRelease.ActiveReleaseId,
			PointerRevision: afterRelease.PointerRevision,
			FactSetComplete: false, ProductionVerified: false})
		if err != nil || os.WriteFile(filepath.Join(evidenceDir, "wiki-quality-cross-source.json"),
			append(report, '\n'), 0600) != nil {
			t.Fatal("retain bounded RTW/DC/BTW quality source report")
		}
	}
	t.Logf("real RTW/DC/BTW Wiki quality source: accepted=%d human_revisions=2 technical_skips=%d ods_ack=%d",
		accepted, other, cursor)
}

func sameQualityEventIDs(got, expected []string) bool {
	if len(got) != 2 || len(expected) != 2 {
		return false
	}
	return (got[0] == expected[0] && got[1] == expected[1]) ||
		(got[0] == expected[1] && got[1] == expected[0])
}
