package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

type wikiFactSetCrossSourceReport struct {
	SchemaVersion         string   `json:"schema_version"`
	FactSetRevisionID     string   `json:"fact_set_revision_id"`
	SourceScopeRevision   string   `json:"source_scope_revision"`
	CatalogTargetWikiID   string   `json:"catalog_target_wiki_revision_id"`
	CatalogEventID        string   `json:"catalog_event_id"`
	CatalogEventRawSHA    string   `json:"catalog_event_raw_sha256"`
	CatalogEventJCSSHA    string   `json:"catalog_event_jcs_sha256"`
	FactSetPayloadJCSSHA  string   `json:"fact_set_jcs_sha256"`
	QualityEventIDs       []string `json:"quality_event_ids"`
	CatalogQualityIDs     []string `json:"catalog_quality_event_ids"`
	CatalogOffset         int64    `json:"catalog_offset"`
	CatalogQualityOffsets []int64  `json:"catalog_quality_offsets"`
	DCSourceEvents        int      `json:"dc_source_events"`
	DCAckCutoff           int64    `json:"dc_ack_cutoff"`
	DCAcknowledgedAtLeast int64    `json:"dc_acknowledged_at_least"`
	DCPrefixIndexJCSSHA   string   `json:"dc_prefix_index_jcs_sha256"`
	DCDeliveryBatchCount  int      `json:"dc_delivery_batch_count"`
	DCCatalogInputHash    string   `json:"dc_catalog_input_hash"`
	DCTargetQualityInputs []string `json:"dc_target_quality_input_hashes"`
	DCFullPrefixVerified  bool     `json:"dc_full_prefix_verified"`
	Catalogs              int      `json:"catalogs"`
	Judgments             int      `json:"judgments"`
	TechnicalSkips        int      `json:"technical_skips"`
	ODSAckOffset          int64    `json:"ods_ack_offset"`
	ManualWikiHead        string   `json:"manual_wiki_head"`
	CatalogWikiHead       string   `json:"catalog_wiki_head"`
	PublishedReleaseID    string   `json:"published_release_id"`
	PointerRevision       int64    `json:"pointer_revision"`
	FactsCompleteDeclared bool     `json:"facts_complete_declared"`
	HumanCatalogVerified  bool     `json:"human_catalog_verified"`
	D07Evaluable          bool     `json:"d07_evaluable"`
	ProductionVerified    bool     `json:"production_verified"`
}

// The explicit Holder is only a source/effect proof: a declared FactSet plus
// three individually frozen human quality Events enter one actual DC producer
// prefix and the same BTW quality consumer must store Catalog before ACK.
// It never labels an admin declaration as objectively complete or D07 passed.
func runRealWikiFactSetHandoff(t *testing.T, dir string, store *model.Store,
	rtwURL, workerToken, moduleID, manualWikiID, publishedReleaseID string,
	catalog types.WikiFactSetRecord, required []types.WikiFactJudgmentRecord,
	baseline types.WikiFactJudgmentRecord) {
	t.Helper()
	btwRoot := os.Getenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT")
	if btwRoot == "" {
		return // DC's platform root may be shared by another Holder selector.
	}
	dcRoot := os.Getenv("SEA_DC_EVENT_PLATFORM_ROOT")
	if dcRoot == "" || !filepath.IsAbs(btwRoot) || !filepath.IsAbs(dcRoot) {
		t.Fatal("FactSet Holder requires absolute fixed BTW and DC source roots")
	}
	if len(required) != 2 || len(catalog.Facts) != 2 ||
		catalog.WikiRevisionId == baseline.WikiRevisionId ||
		catalog.PageId == baseline.PageId || !catalog.FactsComplete ||
		catalog.DeclarationSource != "admin_jwt_allowlist_declaration" ||
		catalog.FactSetRevisionId == "" || catalog.SourceScopeRevision == "" ||
		catalog.EventId == "" || baseline.EventId == "" {
		t.Fatal("Holder has no separate target Wiki FactCatalog and baseline human label")
	}
	facts := make(map[string]types.FactSetFact, len(catalog.Facts))
	for _, fact := range catalog.Facts {
		if !fact.Required {
			t.Fatal("Holder target Catalog contains an optional Fact instead of two required Facts")
		}
		facts[fact.FactId] = fact
	}
	if len(facts) != len(catalog.Facts) || required[0].FactId == required[1].FactId {
		t.Fatal("Catalog or judgments repeated one FactID instead of two required Facts")
	}
	for _, judgment := range required {
		fact, ok := facts[judgment.FactId]
		if !ok || judgment.WikiRevisionId != catalog.WikiRevisionId ||
			judgment.SourceRevisionId != fact.SourceRevisionId ||
			judgment.SourceQuoteSha256 != fact.SourceQuoteSha256 ||
			judgment.EventId == "" || judgment.EventId == catalog.EventId ||
			judgment.EventId == baseline.EventId {
			t.Fatalf("required Fact quality Event was substituted by baseline Wiki label: %+v", judgment)
		}
	}
	if required[0].EventId == required[1].EventId ||
		baseline.FactId == required[0].FactId && baseline.WikiRevisionId == required[0].WikiRevisionId {
		t.Fatal("two target Facts were conflated with the baseline quality Event")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sets := model.New(store.DB, store.Objects, model.WithWikiFactSets())
	quality := model.New(store.DB, store.Objects, model.WithWikiQualityJudgments())
	pinned, err := sets.GetWikiFactSetRevision(ctx, types.WikiFactSetRevisionPath{
		ModuleId: moduleID, PageId: catalog.PageId,
		FactSetRevisionId: catalog.FactSetRevisionId})
	if err != nil || pinned.WikiRevisionId != catalog.WikiRevisionId ||
		pinned.SourceScopeRevision != catalog.SourceScopeRevision ||
		pinned.FactSetJcsSha256 != catalog.FactSetJcsSha256 {
		t.Fatalf("source PG could not pin target FactSet revision: %+v %v", pinned, err)
	}
	current, err := sets.GetWikiFactSetScope(ctx, types.WikiFactSetScopePath{
		ModuleId: moduleID, PageId: catalog.PageId,
		SourceScopeRevision: catalog.SourceScopeRevision})
	if err != nil || current.FactSetRevisionId != catalog.FactSetRevisionId {
		t.Fatalf("source PG FactSet head already differs before DC: %+v %v", current, err)
	}
	catalogWire, err := sets.GetWikiFactSetEvent(ctx, catalog.EventId)
	if err != nil || catalogWire.EventJson == "" ||
		catalogWire.EventRawSha256 != catalog.EventRawSha256 ||
		catalogWire.EventJcsSha256 != catalog.EventJcsSha256 ||
		catalogWire.FactSetJcsSha256 != catalog.FactSetJcsSha256 ||
		object.Hash([]byte(catalogWire.EventJson)) != catalog.EventRawSha256 {
		t.Fatalf("RTW private Catalog original Event three hashes differ from source PG: %+v %v", catalogWire, err)
	}
	canon, err := jsoncanonicalizer.Transform([]byte(catalogWire.EventJson))
	if err != nil || object.Hash(canon) != catalog.EventJcsSha256 {
		t.Fatalf("Catalog original Event JCS differs independently: %v", err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(catalogWire.EventJson), &envelope) != nil {
		t.Fatal("RTW Catalog EventSpec has no versioned payload")
	}
	canon, err = jsoncanonicalizer.Transform(envelope.Payload)
	if err != nil || object.Hash(canon) != catalog.FactSetJcsSha256 {
		t.Fatalf("Catalog payload JCS differs from its immutable revision hash: %v", err)
	}
	allQuality := append([]types.WikiFactJudgmentRecord{baseline}, required...)
	allQualityIDs := make([]string, 0, len(allQuality))
	targetQualityIDs := make([]string, 0, len(required))
	originalQuality := make(map[string]string, len(allQuality))
	for i, judgment := range allQuality {
		wire, err := quality.GetWikiQualityEvent(ctx, judgment.EventId)
		if err != nil || wire.EventJson == "" ||
			wire.EventRawSha256 != judgment.EventRawSha256 ||
			wire.EventJcsSha256 != judgment.EventJcsSha256 ||
			object.Hash([]byte(wire.EventJson)) != judgment.EventRawSha256 {
			t.Fatalf("RTW quality original Event differs for judgment %d: %v", i, err)
		}
		originalQuality[judgment.EventId] = wire.EventJson
		allQualityIDs = append(allQualityIDs, judgment.EventId)
		if i > 0 {
			targetQualityIDs = append(targetQualityIDs, judgment.EventId)
		}
	}
	beforeRelease, err := store.Current(ctx, moduleID)
	if err != nil || beforeRelease.ActiveReleaseId != publishedReleaseID ||
		beforeRelease.PointerRevision != 1 {
		t.Fatalf("Catalog Holder did not start from a manual Release: %+v %v", beforeRelease, err)
	}
	var beforeManualHead, beforeCatalogHead string
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, baseline.PageId).
		Scan(&beforeManualHead); err != nil || beforeManualHead != manualWikiID {
		t.Fatalf("baseline editing head moved before Catalog dispatch: %s %v", beforeManualHead, err)
	}
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, catalog.PageId).
		Scan(&beforeCatalogHead); err != nil || beforeCatalogHead != catalog.WikiRevisionId {
		t.Fatalf("Catalog AI editing head moved before dispatch: %s %v", beforeCatalogHead, err)
	}
	platform := startRealDCJobPlatform(t, dir, dcRoot)
	sender := &mqs.HTTPSender{Endpoint: platform.BaseURL + "/v1/events",
		Token: platform.Token, Client: &http.Client{Timeout: 10 * time.Second}}
	delivered := 0
	for ; delivered < 256; delivered++ {
		sent, err := store.DispatchOne(ctx, sender)
		if err != nil {
			t.Fatalf("RTW original Outbox to actual DC Eventing: %v", err)
		}
		if !sent {
			break
		}
	}
	if delivered < 5 || delivered == 256 {
		t.Fatalf("shared producer did not send Catalog+three quality+technical events: %d", delivered)
	}
	var accepted, catalogs, judgments, technical int
	var first, last int64
	err = platform.Pool.QueryRow(ctx, `SELECT count(*),min(offset_id),max(offset_id),
 count(*) FILTER (WHERE envelope->>'event_type'='knowledge.wiki.fact-set.frozen.v1'),
 count(*) FILTER (WHERE envelope->>'event_type'='knowledge.wiki.quality.judged.v1'),
 count(*) FILTER (WHERE envelope->>'event_type' NOT IN
 ('knowledge.wiki.fact-set.frozen.v1','knowledge.wiki.quality.judged.v1'))
 FROM eventing.event WHERE producer='ridethewind.knowledge'`).Scan(
		&accepted, &first, &last, &catalogs, &judgments, &technical)
	if err != nil || accepted != delivered || first != 1 || last != int64(delivered) ||
		catalogs != 1 || judgments != 3 || technical < 1 ||
		technical != delivered-catalogs-judgments {
		t.Fatalf("DC PG producer prefix is not exactly one Catalog/three quality plus technical: accepted=%d first=%d last=%d catalog=%d judgments=%d technical=%d sent=%d err=%v",
			accepted, first, last, catalogs, judgments, technical, delivered, err)
	}
	expectedIDs := append([]string{catalog.EventId}, allQualityIDs...)
	var found int
	if err := platform.Pool.QueryRow(ctx, `SELECT count(*) FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=ANY($1)`, expectedIDs).
		Scan(&found); err != nil || found != 4 {
		t.Fatalf("DC PG lost a specific Catalog/quality original EventID: %d %v", found, err)
	}
	var baselineOffset, catalogOffset int64
	qualityOffsets := make([]int64, 0, len(required))
	if err := platform.Pool.QueryRow(ctx, `SELECT offset_id FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=$1`, baseline.EventId).
		Scan(&baselineOffset); err != nil {
		t.Fatal(err)
	}
	if err := platform.Pool.QueryRow(ctx, `SELECT offset_id FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=$1`, catalog.EventId).
		Scan(&catalogOffset); err != nil {
		t.Fatal(err)
	}
	for _, judgment := range required {
		var offset int64
		if err := platform.Pool.QueryRow(ctx, `SELECT offset_id FROM eventing.event
 WHERE producer='ridethewind.knowledge' AND event_id=$1`, judgment.EventId).
			Scan(&offset); err != nil || offset <= catalogOffset {
			t.Fatalf("required quality Event did not follow its Catalog on DC prefix: %d %v", offset, err)
		}
		qualityOffsets = append(qualityOffsets, offset)
	}
	if baselineOffset >= catalogOffset || catalogOffset < 1 {
		t.Fatalf("baseline quality and Catalog producer offsets misordered: baseline=%d Catalog=%d",
			baselineOffset, catalogOffset)
	}
	resultPath := filepath.Join(dir, "btw-real-wiki-fact-set-result.json")
	fixturePath := filepath.Join(dir, "btw-real-wiki-fact-set-fixture.json")
	fixture, err := json.Marshal(struct {
		RTWURL                 string   `json:"rtw_url"`
		RTWToken               string   `json:"rtw_token"`
		DCURL                  string   `json:"dc_url"`
		DCToken                string   `json:"dc_token"`
		ExpectedEvents         int      `json:"expected_events"`
		CatalogEventID         string   `json:"catalog_event_id"`
		QualityEventIDs        []string `json:"quality_event_ids"`
		CatalogQualityEventIDs []string `json:"catalog_quality_event_ids"`
		ResultPath             string   `json:"result_path"`
		FactSetRevisionID      string   `json:"fact_set_revision_id"`
		SourceScopeRevision    string   `json:"source_scope_revision"`
	}{rtwURL, workerToken, platform.BaseURL, platform.Token, delivered,
		catalog.EventId, allQualityIDs, targetQualityIDs, resultPath,
		catalog.FactSetRevisionId, catalog.SourceScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	writeRealFactSet0600(t, fixturePath, append(fixture, '\n'))
	consumer := exec.CommandContext(ctx, "bash", "internal/warehouse/wikiqualitysource/acceptance.sh")
	consumer.Dir = btwRoot
	consumer.Env = append(os.Environ(), "SEA_RTW_REAL_WIKI_FACT_SET_FIXTURE="+fixturePath)
	output, consumerErr := consumer.CombinedOutput()
	outputPath := filepath.Join(dir, "btw-real-wiki-fact-set-consumer.log")
	writeRealFactSet0600(t, outputPath, output)
	if consumerErr != nil {
		t.Fatalf("BTW same quality consumer rejected original Catalog prefix: %v; task-log=%s",
			consumerErr, outputPath)
	}
	info, err := os.Lstat(resultPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("BTW consumer did not freeze a private regular result: %v %v", info, err)
	}
	resultBytes, err := readRealFactSetResult(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Committed         int64    `json:"committed_offset"`
		Acknowledged      int64    `json:"acknowledged_offset"`
		Catalogs          int      `json:"catalogs"`
		Judgments         int      `json:"judgments"`
		TechnicalSkips    int      `json:"technical_skips"`
		CatalogEventID    string   `json:"catalog_event_id"`
		QualityEventIDs   []string `json:"quality_event_ids"`
		CatalogQualityIDs []string `json:"catalog_quality_event_ids"`
	}
	if json.Unmarshal(resultBytes, &result) != nil ||
		result.Committed != int64(delivered) || result.Acknowledged != int64(delivered) ||
		result.Catalogs != 1 || result.Judgments != 3 || result.TechnicalSkips != technical ||
		result.CatalogEventID != catalog.EventId ||
		!sameRealFactSetEventIDs(result.QualityEventIDs, allQualityIDs) ||
		!sameRealFactSetEventIDs(result.CatalogQualityIDs, targetQualityIDs) {
		t.Fatalf("BTW ODS/catalog/current quality/ACK differs from DC full prefix: %+v", result)
	}
	var cursor int64
	if err := platform.Pool.QueryRow(ctx, `SELECT acknowledged_offset FROM eventing.consumer
 WHERE consumer='btw-warehouse-wiki-quality' AND producer='ridethewind.knowledge'`).
		Scan(&cursor); err != nil || cursor != int64(delivered) {
		t.Fatalf("same BTW quality consumer skipped Catalog or did not ACK full prefix: %d %v", cursor, err)
	}
	// Read the actual DC service-token evidence after BTW ACK. PG counts alone
	// cannot prove the consumer's historical batch receipts and full Event wire.
	pins := make(map[string]realDCPin, 4)
	pins[catalog.EventId], err = pinRealDCRTWEvent(catalogWire.EventJson,
		catalog.EventRawSha256, catalog.EventJcsSha256,
		"knowledge.wiki.fact-set.frozen.v1", catalogOffset)
	if err != nil {
		t.Fatalf("RTW Catalog original bytes/JCS cannot be pinned to DC: %v", err)
	}
	qualityOffsetsByID := map[string]int64{baseline.EventId: baselineOffset}
	for i, judgment := range required {
		qualityOffsetsByID[judgment.EventId] = qualityOffsets[i]
	}
	for _, judgment := range allQuality {
		pins[judgment.EventId], err = pinRealDCRTWEvent(
			originalQuality[judgment.EventId], judgment.EventRawSha256,
			judgment.EventJcsSha256, "knowledge.wiki.quality.judged.v1",
			qualityOffsetsByID[judgment.EventId])
		if err != nil {
			t.Fatalf("RTW single quality original bytes/JCS cannot be pinned to DC: %v", err)
		}
	}
	dcProof, err := readRealDCAcknowledgedWikiFactSetPrefix(ctx,
		platform.BaseURL, platform.Token, int64(delivered), pins)
	if err != nil || !dcProof.FullPrefixVerified ||
		dcProof.AcknowledgedAtLeast < int64(delivered) ||
		len(dcProof.OriginalEventInputs) != 4 {
		t.Fatalf("DC read-only historical full prefix/immutable batch ACK differs from RTW original Events: %+v %v", dcProof, err)
	}
	afterCatalog, err := sets.GetWikiFactSetEvent(ctx, catalog.EventId)
	if err != nil || afterCatalog.EventJson != catalogWire.EventJson ||
		afterCatalog.EventRawSha256 != catalogWire.EventRawSha256 ||
		afterCatalog.EventJcsSha256 != catalogWire.EventJcsSha256 ||
		afterCatalog.FactSetJcsSha256 != catalogWire.FactSetJcsSha256 {
		t.Fatalf("DC/ODS altered source Catalog original Event: %+v %v", afterCatalog, err)
	}
	for _, judgment := range allQuality {
		after, err := quality.GetWikiQualityEvent(ctx, judgment.EventId)
		if err != nil || after.EventJson != originalQuality[judgment.EventId] ||
			after.EventRawSha256 != judgment.EventRawSha256 ||
			after.EventJcsSha256 != judgment.EventJcsSha256 {
			t.Fatalf("DC/ODS altered source quality Event %s: %v", judgment.EventId, err)
		}
	}
	afterPinned, err := sets.GetWikiFactSetRevision(ctx, types.WikiFactSetRevisionPath{
		ModuleId: moduleID, PageId: catalog.PageId,
		FactSetRevisionId: catalog.FactSetRevisionId})
	if err != nil || afterPinned.FactSetRevisionId != pinned.FactSetRevisionId ||
		afterPinned.WikiRevisionId != pinned.WikiRevisionId ||
		afterPinned.SourceScopeRevision != pinned.SourceScopeRevision ||
		afterPinned.FactSetJcsSha256 != pinned.FactSetJcsSha256 {
		t.Fatalf("transport changed historical FactSetRevision target/scope: %+v %v", afterPinned, err)
	}
	afterCurrent, err := sets.GetWikiFactSetScope(ctx, types.WikiFactSetScopePath{
		ModuleId: moduleID, PageId: catalog.PageId,
		SourceScopeRevision: catalog.SourceScopeRevision})
	if err != nil || afterCurrent.FactSetRevisionId != current.FactSetRevisionId ||
		afterCurrent.FactSetJcsSha256 != current.FactSetJcsSha256 {
		t.Fatalf("technical ODS altered FactSet source scope head: %+v %v", afterCurrent, err)
	}
	var afterManualHead, afterCatalogHead string
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, baseline.PageId).
		Scan(&afterManualHead); err != nil || afterManualHead != beforeManualHead {
		t.Fatalf("Catalog ODS moved baseline Wiki editing head: %s %v", afterManualHead, err)
	}
	if err := store.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, moduleID, catalog.PageId).
		Scan(&afterCatalogHead); err != nil || afterCatalogHead != beforeCatalogHead {
		t.Fatalf("Catalog ODS moved target Wiki editing head: %s %v", afterCatalogHead, err)
	}
	afterRelease, err := store.Current(ctx, moduleID)
	if err != nil || afterRelease.PointerRevision != beforeRelease.PointerRevision ||
		afterRelease.ActiveReleaseId != beforeRelease.ActiveReleaseId ||
		afterRelease.ActiveBuildId != beforeRelease.ActiveBuildId {
		t.Fatalf("Catalog ODS changed manual published Release: %+v %+v %v",
			beforeRelease, afterRelease, err)
	}
	if evidenceDir := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR"); evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0700); err != nil {
			t.Fatal(err)
		}
		report, err := json.Marshal(wikiFactSetCrossSourceReport{
			SchemaVersion:        "sea.wiki.fact-set-cross-source.v2",
			FactSetRevisionID:    catalog.FactSetRevisionId,
			SourceScopeRevision:  catalog.SourceScopeRevision,
			CatalogTargetWikiID:  catalog.WikiRevisionId,
			CatalogEventID:       catalog.EventId,
			CatalogEventRawSHA:   catalog.EventRawSha256,
			CatalogEventJCSSHA:   catalog.EventJcsSha256,
			FactSetPayloadJCSSHA: catalog.FactSetJcsSha256,
			QualityEventIDs:      allQualityIDs, CatalogQualityIDs: targetQualityIDs,
			CatalogOffset: catalogOffset, CatalogQualityOffsets: qualityOffsets,
			DCSourceEvents: accepted, Catalogs: catalogs,
			DCAckCutoff:           dcProof.Cutoff,
			DCAcknowledgedAtLeast: dcProof.AcknowledgedAtLeast,
			DCPrefixIndexJCSSHA:   dcProof.IndexJCSSHA,
			DCDeliveryBatchCount:  dcProof.BatchReceipts,
			DCCatalogInputHash:    dcProof.OriginalEventInputs[catalog.EventId],
			DCTargetQualityInputs: []string{
				dcProof.OriginalEventInputs[required[0].EventId],
				dcProof.OriginalEventInputs[required[1].EventId]},
			DCFullPrefixVerified: dcProof.FullPrefixVerified,
			Judgments:            judgments, TechnicalSkips: technical, ODSAckOffset: cursor,
			ManualWikiHead: afterManualHead, CatalogWikiHead: afterCatalogHead,
			PublishedReleaseID:    afterRelease.ActiveReleaseId,
			PointerRevision:       afterRelease.PointerRevision,
			FactsCompleteDeclared: true, HumanCatalogVerified: false,
			D07Evaluable: false, ProductionVerified: false})
		if err != nil {
			t.Fatal(err)
		}
		writeRealFactSet0600(t, filepath.Join(evidenceDir, "wiki-fact-set-cross-source.json"),
			append(report, '\n'))
	}
	t.Logf("RTW/DC/BTW same wiki quality consumer: events=%d Catalog=1 target_required=2 total_quality=3 technical=%d ACK=%d DC_historical_batches=%d",
		accepted, technical, cursor, dcProof.BatchReceipts)
}

func sameRealFactSetEventIDs(got, expected []string) bool {
	if len(got) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(expected))
	for _, id := range expected {
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	for _, id := range got {
		if !seen[id] {
			return false
		}
		delete(seen, id)
	}
	return len(seen) == 0
}

func writeRealFactSet0600(t *testing.T, path string, data []byte) {
	t.Helper()
	if !filepath.IsAbs(path) {
		t.Fatal("one-time FactSet source evidence path must be absolute")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	n, err := f.Write(data)
	if err != nil || n != len(data) {
		_ = f.Close()
		t.Fatalf("one-time FactSet source evidence short write: %d/%d %v", n, len(data), err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("one-time FactSet source evidence is not plain private file: %v %v", info, err)
	}
}

func readRealFactSetResult(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("FactSet consumer result exceeds one MiB")
	}
	return raw, nil
}
