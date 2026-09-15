package model_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/jackc/pgx/v5"
)

func wikiFactSetFixture(t *testing.T) (*model.Store, *model.Store, types.Module,
	[]types.Revision, types.Compile, types.Revision, types.FreezeWikiFactSetReq) {
	t.Helper()
	ordinary := testenv.Store(t)
	sets := model.New(ordinary.DB, ordinary.Objects, model.WithWikiFactSets())
	if err := sets.CheckWikiFactSetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	module := createModule(t, ordinary, "complete two-source FactCatalog")
	sourceA, err := ordinary.CreateSource(ctx, "test-admin", types.CreateSourceReq{
		ModuleId: module.Id, Title: "original indented CRLF fact",
		Content:   "header\r\n\r\n    prefix short fact\r\n    suffix short fact",
		MediaType: "text/plain", Provenance: "synthetic original UTF-8 source A",
		IdempotencyKey: "fact-set-source-a"})
	sourceA = must(t, sourceA, err)
	sourceB, err := ordinary.CreateSource(ctx, "test-admin", types.CreateSourceReq{
		ModuleId: module.Id, Title: "contradictory paragraph",
		Content:   "header\r\n\r\ncontradicting short fact",
		MediaType: "text/plain", Provenance: "synthetic original UTF-8 source B",
		IdempotencyKey: "fact-set-source-b"})
	sourceB = must(t, sourceB, err)
	sources := []types.Revision{sourceA, sourceB}
	sort.Slice(sources, func(i, j int) bool { return sources[i].RevisionId < sources[j].RevisionId })
	ids := []string{sources[0].RevisionId, sources[1].RevisionId}
	compile, err := ordinary.CreateCompile(ctx, "test-admin", types.CreateCompileReq{
		ModuleId: module.Id, PageId: "《完整来源事实页》", SourceRevisionIds: ids,
		Guidance:       "quote only the two frozen Source paragraphs",
		IdempotencyKey: "fact-set-ai-compile"})
	compile = must(t, compile, err)
	_, err = ordinary.ClaimCompile(ctx, types.ClaimCompileReq{
		CompileId: compile.CompileId, Generation: compile.Generation,
		InputHash: compile.InputHash, AttemptId: "fact-set-attempt-1", LeaseEpoch: 1,
		LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	key, hash, err := ordinary.Objects.Put(ctx, []byte("These are two Source facts; one AI citation is deliberately incomplete."))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := ordinary.AcceptCompile(ctx, types.AcceptCompileReq{
		CompileId: compile.CompileId, Generation: compile.Generation,
		InputHash: compile.InputHash, AttemptId: "fact-set-attempt-1", LeaseEpoch: 1,
		ObjectKey: key, ContentHash: hash, Title: "未发布的AI修订",
		SourceRefs: []types.SourceRef{{RevisionId: sourceA.RevisionId, Locator: "paragraph:2"}}})
	accepted = must(t, accepted, err)
	wiki, err := ordinary.GetRevision(ctx, accepted.RevisionId)
	wiki = must(t, wiki, err)
	identity := make([]types.FactSetSourceRevision, 0, len(sources))
	for _, source := range sources {
		identity = append(identity, types.FactSetSourceRevision{
			RevisionId: source.RevisionId, ContentSha256: source.ContentHash})
	}
	quoteA, quoteB := "short fact", "contradicting short fact"
	req := types.FreezeWikiFactSetReq{ModuleId: module.Id, PageId: wiki.EntityId,
		WikiRevisionId: wiki.RevisionId, OriginCompileId: compile.CompileId,
		SourceRevisions: identity, Facts: []types.FactSetInputFact{
			{SourceRevisionId: sourceA.RevisionId, Locator: "paragraph:2",
				SourceQuote: quoteA, SourceQuoteSha256: object.Hash([]byte(quoteA)),
				Required: true, ConflictGroup: "conflict_1"},
			{SourceRevisionId: sourceB.RevisionId, Locator: "paragraph:2",
				SourceQuote: quoteB, SourceQuoteSha256: object.Hash([]byte(quoteB)),
				Required: true, ConflictGroup: "conflict_1"}},
		FactsComplete: true, Reason: "administrator declares both frozen facts complete",
		IdempotencyKey: "fact-set-ai-frozen"}
	return ordinary, sets, module, sources, accepted, wiki, req
}

func TestWikiFactSetFullApprovedScopeAIManualCASAndOriginalEvent(t *testing.T) {
	ordinary, sets, module, sources, compile, wiki, req := wikiFactSetFixture(t)
	first, err := sets.FreezeWikiFactSet(ctx, "test-admin", req)
	first = must(t, first, err)
	if first.WikiOriginKind != "ai_accepted" || first.OriginCompileId != compile.CompileId ||
		first.FactSetRevision != "1" || len(first.Facts) != 2 ||
		first.SourceScopeRevision == "" || first.FactSetJcsSha256 == first.EventJcsSha256 {
		t.Fatalf("AI FactSet lost full Source scope or its independent version hash: %+v", first)
	}
	// AI cited only one paragraph, but the complete Catalog contains both
	// Source revisions frozen by the accepted Compile, including the unquoted one.
	if !reflect.DeepEqual(first.SourceRevisions, req.SourceRevisions) {
		t.Fatalf("accepted Compile Source scope was narrowed to Wiki citations: %+v", first)
	}
	originalByID := map[string][]byte{}
	for _, source := range sources {
		original, err := ordinary.Objects.Get(ctx, source.ObjectKey, source.ContentHash)
		if err != nil || object.Hash(original) != source.ContentHash {
			t.Fatalf("independent Source object read differs from RTW revision SHA: %s %v",
				source.RevisionId, err)
		}
		originalByID[source.RevisionId] = original
	}
	for i, fact := range first.Facts {
		original := originalByID[fact.SourceRevisionId]
		start, e1 := strconv.Atoi(fact.SourceByteStart)
		end, e2 := strconv.Atoi(fact.SourceByteEnd)
		if e1 != nil || e2 != nil || start < 0 || end > len(original) ||
			!bytes.Equal(original[start:end], []byte(fact.SourceQuote)) ||
			start != bytes.Index(original, []byte(fact.SourceQuote)) ||
			!fact.Required || fact.ConflictGroup != "conflict_1" ||
			i > 0 && first.Facts[i-1].FactId >= fact.FactId {
			t.Fatalf("Fact quote span/identity failed original CRLF+indent Source bytes: %+v", fact)
		}
	}
	event, err := sets.GetWikiFactSetEvent(ctx, first.EventId)
	event = must(t, event, err)
	if event.EventJson == "" || object.Hash([]byte(event.EventJson)) != first.EventRawSha256 ||
		event.EventJcsSha256 != first.EventJcsSha256 ||
		event.FactSetJcsSha256 != first.FactSetJcsSha256 {
		t.Fatalf("private original FactSet Event/three hash domains diverged: %+v", event)
	}
	canonical, err := jsoncanonicalizer.Transform([]byte(event.EventJson))
	if err != nil || object.Hash(canonical) != first.EventJcsSha256 {
		t.Fatalf("original Event JCS SHA differs from independent canonicalizer: %v", err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(event.EventJson), &envelope) != nil {
		t.Fatal("original FactSet Event payload is not JSON")
	}
	canonical, err = jsoncanonicalizer.Transform(envelope.Payload)
	if err != nil || object.Hash(canonical) != first.FactSetJcsSha256 {
		t.Fatalf("FactSet revision JCS was conflated with Event JCS: %v", err)
	}
	replayed, err := sets.FreezeWikiFactSet(ctx, "test-admin", req)
	replayed = must(t, replayed, err)
	if !reflect.DeepEqual(first, replayed) {
		t.Fatalf("same command reran FactSet head/Event: first=%+v replay=%+v", first, replayed)
	}
	different := req
	different.Reason = "same key, different scope declaration"
	if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", different); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key differing request escaped 409 idempotency: %v", err)
	}
	for name, mutate := range map[string]func(*types.FreezeWikiFactSetReq){
		"subset source list": func(r *types.FreezeWikiFactSetReq) { r.SourceRevisions = r.SourceRevisions[:1] },
		"source hash conflict": func(r *types.FreezeWikiFactSetReq) {
			r.SourceRevisions[0].ContentSha256 = strings.Repeat("0", 64)
		},
		"fake quote": func(r *types.FreezeWikiFactSetReq) {
			r.Facts[0].SourceQuote = "a false fact"
			r.Facts[0].SourceQuoteSha256 = object.Hash([]byte(r.Facts[0].SourceQuote))
		},
		"out of range locator": func(r *types.FreezeWikiFactSetReq) {
			r.Facts[0].Locator = "paragraph:9"
		},
		"lone conflict relation": func(r *types.FreezeWikiFactSetReq) {
			r.Facts[1].ConflictGroup = "other_group"
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := req
			bad.SourceRevisions = append([]types.FactSetSourceRevision(nil), req.SourceRevisions...)
			bad.Facts = append([]types.FactSetInputFact(nil), req.Facts...)
			bad.IdempotencyKey = "fact-set-bad-" + strings.ReplaceAll(name, " ", "-")
			mutate(&bad)
			if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", bad); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("corrupt complete Source/Fact scope reached append-only head: %v", err)
			}
		})
	}
	manual, err := ordinary.CreateWiki(ctx, "test-admin", types.CreateWikiReq{
		ModuleId: module.Id, PageId: wiki.EntityId, BaseRevisionId: wiki.RevisionId,
		Title: "人工改稿", Content: "short fact stays, and a second Source was considered",
		SourceRefs:     []types.SourceRef{{RevisionId: sources[0].RevisionId, Locator: "paragraph:2"}},
		IdempotencyKey: "fact-set-manual-base-edit"})
	manual = must(t, manual, err)
	manualReq := req
	manualReq.WikiRevisionId, manualReq.OriginCompileId = manual.RevisionId, ""
	manualReq.BaseFactSetRevisionId = first.FactSetRevisionId
	manualReq.IdempotencyKey = "fact-set-manual-reapproved"
	second, err := sets.FreezeWikiFactSet(ctx, "test-admin", manualReq)
	second = must(t, second, err)
	if second.FactSetRevision != "2" || second.WikiOriginKind != "manual_revision" ||
		second.SourceScopeRevision != first.SourceScopeRevision ||
		second.BaseFactSetRevisionId != first.FactSetRevisionId ||
		second.FactSetId != first.FactSetId || !reflect.DeepEqual(second.SourceRevisions, first.SourceRevisions) {
		t.Fatalf("manual same-page base lineage lost accepted Compile's full Source scope: %+v", second)
	}
	current, err := sets.GetWikiFactSetScope(ctx, types.WikiFactSetScopePath{
		ModuleId: module.Id, PageId: wiki.EntityId,
		SourceScopeRevision: first.SourceScopeRevision})
	current = must(t, current, err)
	historical, err := sets.GetWikiFactSetRevision(ctx, types.WikiFactSetRevisionPath{
		ModuleId: module.Id, PageId: wiki.EntityId,
		FactSetRevisionId: first.FactSetRevisionId})
	historical = must(t, historical, err)
	if current.FactSetRevisionId != second.FactSetRevisionId ||
		current.WikiRevisionId != manual.RevisionId ||
		historical.FactSetRevisionId != first.FactSetRevisionId ||
		historical.WikiRevisionId != wiki.RevisionId ||
		historical.SourceScopeRevision != current.SourceScopeRevision ||
		historical.EventRawSha256 != first.EventRawSha256 {
		t.Fatalf("FactSet head CAS rewrote history or edited Wiki: current=%+v old=%+v", current, historical)
	}
	stale := req
	stale.IdempotencyKey = "fact-set-stale-base"
	if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", stale); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("old Wiki/source scope FactSet base escaped CAS: %v", err)
	}
	var wikiHead string
	if err := sets.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`,
		module.Id, wiki.EntityId).Scan(&wikiHead); err != nil || wikiHead != manual.RevisionId {
		t.Fatalf("FactSet head wrote Wiki editing head: %s %v", wikiHead, err)
	}
	var publications int
	if err := sets.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_publications WHERE module_id=$1",
		module.Id).Scan(&publications); err != nil || publications != 0 {
		t.Fatalf("FactSet approved a Release without an administrator: %d %v", publications, err)
	}
	_, err = ordinary.Withdraw(ctx, "test-admin", types.WithdrawReq{ModuleId: module.Id,
		TargetKind: "revision", TargetId: sources[1].RevisionId,
		Reason:         "Source revision retired after frozen Catalog",
		IdempotencyKey: "fact-set-withdraw-source-b"})
	if err != nil {
		t.Fatal(err)
	}
	aiRetired := req
	aiRetired.IdempotencyKey = "fact-set-ai-refuses-retired-frozen-source"
	if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", aiRetired); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("AI target silently trimmed a Source frozen by accepted Compile: %v", err)
	}
	withdrawn := manualReq
	withdrawn.BaseFactSetRevisionId = second.FactSetRevisionId
	withdrawn.IdempotencyKey = "fact-set-invalid-after-source-withdraw"
	if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", withdrawn); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("retired Source still qualified a new complete FactSet: %v", err)
	}
	// The old base revision remains in the page lineage for history. RTW's
	// actual withdrawal removes only that retired ancestor from a *new*
	// manual Catalog scope; it never allows silently dropping available Facts.
	newScope := manualReq
	newScope.SourceRevisions = []types.FactSetSourceRevision{req.SourceRevisions[0]}
	newScope.Facts = nil
	for _, fact := range req.Facts {
		if fact.SourceRevisionId == sources[0].RevisionId {
			fact.ConflictGroup = ""
			newScope.Facts = append(newScope.Facts, fact)
		}
	}
	newScope.BaseFactSetRevisionId = ""
	newScope.IdempotencyKey = "fact-set-new-current-source-scope"
	if newScope.SourceRevisions[0].RevisionId != sources[0].RevisionId || len(newScope.Facts) != 1 {
		t.Fatal("retired ancestor test built a noncanonical available Source scope")
	}
	newCatalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", newScope)
	newCatalog = must(t, newCatalog, err)
	if newCatalog.SourceScopeRevision == first.SourceScopeRevision ||
		newCatalog.FactSetRevision != "1" || len(newCatalog.SourceRevisions) != 1 ||
		newCatalog.WikiRevisionId != manual.RevisionId {
		t.Fatalf("withdrawn ancestor prevented new manual approved Source scope: %+v", newCatalog)
	}
	oldCurrent, err := sets.GetWikiFactSetScope(ctx, types.WikiFactSetScopePath{
		ModuleId: module.Id, PageId: manual.EntityId,
		SourceScopeRevision: first.SourceScopeRevision})
	oldCurrent = must(t, oldCurrent, err)
	if oldCurrent.FactSetRevisionId != second.FactSetRevisionId ||
		oldCurrent.WikiRevisionId != manual.RevisionId {
		t.Fatalf("new eligible Source scope rewrote historical scope head: %+v", oldCurrent)
	}
	after, err := sets.GetWikiFactSetEvent(ctx, first.EventId)
	after = must(t, after, err)
	if after.EventJson != event.EventJson || after.EventRawSha256 != event.EventRawSha256 {
		t.Fatalf("Source withdrawal rewrote historical FactSet source Event: %+v", after)
	}
}

func TestWikiFactSetOutboxFailureRollsBackRevisionHeadAndReplay(t *testing.T) {
	_, sets, _, _, _, _, req := wikiFactSetFixture(t)
	_, err := sets.DB.Exec(ctx, `CREATE FUNCTION block_fact_set_event() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected FactSet Outbox refusal'; END $$;
 CREATE TRIGGER block_fact_set_event BEFORE INSERT ON knowledge_outbox
 FOR EACH ROW WHEN (NEW.event_type='knowledge.wiki.fact-set.frozen.v1')
 EXECUTE FUNCTION block_fact_set_event()`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sets.FreezeWikiFactSet(ctx, "test-admin", req); err == nil {
		t.Fatal("Outbox refusal still advanced complete FactSet domain state")
	}
	for _, table := range []string{"knowledge_wiki_fact_set_revisions",
		"knowledge_wiki_fact_set_heads", "knowledge_wiki_fact_set_events"} {
		var count int
		if err := sets.DB.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("Outbox rollback left orphan FactSet table=%s count=%d err=%v", table, count, err)
		}
	}
	if _, err := sets.DB.Exec(ctx, "DROP TRIGGER block_fact_set_event ON knowledge_outbox"); err != nil {
		t.Fatal(err)
	}
	if recovered, err := sets.FreezeWikiFactSet(ctx, "test-admin", req); err != nil ||
		recovered.EventId == "" || recovered.FactSetRevision != "1" {
		t.Fatalf("original same key did not recover after atomic Outbox refusal: %+v %v", recovered, err)
	}
}

func TestWikiFactSetDefaultOffAndSchemaProbe(t *testing.T) {
	ordinary := testenv.Store(t)
	module := createModule(t, ordinary, "old Wiki remains after FactSet sidecar")
	oldSource := source(t, ordinary, module, "old unmodified source")
	_, err := ordinary.CreateWiki(ctx, "test-admin", types.CreateWikiReq{
		ModuleId: module.Id, PageId: "old-wiki-page", Title: "老Wiki",
		Content: "An older human Wiki keeps its Source byte", SourceRefs: []types.SourceRef{{
			RevisionId: oldSource.RevisionId, Locator: "paragraph:2"}},
		IdempotencyKey: "old-wiki-before-catalog-migration"})
	if err != nil {
		t.Fatal(err)
	}
	var oldOutbox string
	var oldHeads int
	if err := ordinary.DB.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(payload ORDER BY event_id),'[]'::jsonb)::text
 FROM knowledge_outbox WHERE aggregate_id=$1`, module.Id).Scan(&oldOutbox); err != nil {
		t.Fatal(err)
	}
	if err := ordinary.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_heads WHERE module_id=$1",
		module.Id).Scan(&oldHeads); err != nil || oldHeads != 2 {
		t.Fatalf("old Source/Wiki head fixture is not real: %d %v", oldHeads, err)
	}
	if err := ordinary.Migrate(ctx); err != nil {
		t.Fatalf("sidecar migration is not reentrant: %v", err)
	}
	if _, err := ordinary.FreezeWikiFactSet(ctx, "test-admin", types.FreezeWikiFactSetReq{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("ordinary Wiki mode accepted a FactSet by default: %v", err)
	}
	var rows int
	if err := ordinary.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_wiki_fact_set_revisions").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("default-off migrated/built a synthetic FactSet: rows=%d err=%v", rows, err)
	}
	if _, err := ordinary.DB.Exec(ctx, "DROP TABLE knowledge_wiki_fact_set_events, knowledge_wiki_fact_set_heads, knowledge_wiki_fact_set_revisions CASCADE"); err != nil {
		t.Fatal(err)
	}
	sets := model.New(ordinary.DB, ordinary.Objects, model.WithWikiFactSets())
	if err := sets.CheckWikiFactSetSchema(ctx); err == nil {
		t.Fatal("enabled FactSet surface passed startup probe without its isolated sidecar")
	}
	script, err := os.ReadFile("../../../scripts/migrate-wiki-fact-set.sql")
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if _, err := ordinary.DB.Exec(ctx, string(script), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("existing Knowledge DB FactSet sidecar pass %d: %v", n+1, err)
		}
	}
	if err := sets.CheckWikiFactSetSchema(ctx); err != nil {
		t.Fatalf("enabled FactSet startup probe failed after isolated migration: %v", err)
	}
	for _, table := range []string{"knowledge_wiki_fact_set_revisions",
		"knowledge_wiki_fact_set_heads", "knowledge_wiki_fact_set_events"} {
		if err := ordinary.DB.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("sidecar backfilled old Wiki/Source into %s: %d %v", table, rows, err)
		}
	}
	var afterOutbox string
	var afterHeads int
	if err := ordinary.DB.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(payload ORDER BY event_id),'[]'::jsonb)::text
 FROM knowledge_outbox WHERE aggregate_id=$1`, module.Id).Scan(&afterOutbox); err != nil {
		t.Fatal(err)
	}
	if err := ordinary.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_heads WHERE module_id=$1",
		module.Id).Scan(&afterHeads); err != nil || afterHeads != oldHeads ||
		afterOutbox != oldOutbox {
		t.Fatalf("FactSet sidecar rewrote old Outbox/head: heads=%d before=%d oldEventSame=%v err=%v",
			afterHeads, oldHeads, afterOutbox == oldOutbox, err)
	}
}
