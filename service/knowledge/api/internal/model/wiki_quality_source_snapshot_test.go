package model_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

type snapshotInterleavingObjects struct {
	object.Store
	once   sync.Once
	onRead func()
}

func (o *snapshotInterleavingObjects) Get(ctx context.Context, key, hash string) ([]byte, error) {
	o.once.Do(o.onRead)
	return o.Store.Get(ctx, key, hash)
}

func snapshotQualityRequest(catalog types.WikiFactSetRecord, fact types.FactSetFact,
	key string) types.JudgeWikiFactReq {
	return types.JudgeWikiFactReq{ModuleId: catalog.ModuleId, PageId: catalog.PageId,
		WikiRevisionId: catalog.WikiRevisionId, OriginCompileId: catalog.OriginCompileId,
		SourceRevisionId: fact.SourceRevisionId, SourceContentSha256: fact.SourceContentSha256,
		Locator: fact.Locator, SourceQuote: fact.SourceQuote,
		SourceQuoteSha256: fact.SourceQuoteSha256,
		Assessment:        "missing", Grade: "0", RubricVersion: "sea.wiki.fact-coverage.v1",
		Reason:         "original Source fact is absent from the fixed Wiki claim",
		IdempotencyKey: key}
}

func snapshotTarget(catalog types.WikiFactSetRecord) model.WikiQualitySnapshotTarget {
	return model.WikiQualitySnapshotTarget{ModuleID: catalog.ModuleId, PageID: catalog.PageId,
		FactSetRevisionID:   catalog.FactSetRevisionId,
		WikiRevisionID:      catalog.WikiRevisionId,
		SourceScopeRevision: catalog.SourceScopeRevision}
}

func TestWikiQualitySourceSnapshotPinsHistoricalCatalogAndCurrentRequiredHeads(t *testing.T) {
	ordinary, sets, module, sources, _, wiki, req := wikiFactSetFixture(t)
	quality := model.New(ordinary.DB, ordinary.Objects,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	catalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", req)
	catalog = must(t, catalog, err)
	target := snapshotTarget(catalog)
	initial, err := quality.ReadWikiQualitySourceSnapshot(ctx, target)
	initial = must(t, initial, err)
	if initial.AllRequiredHaveHead || !initial.WikiAndSourcesAvailableAtRead ||
		len(initial.RequiredFacts) != 2 ||
		initial.RequiredFacts[0].Judgment != nil || initial.RequiredFacts[1].Judgment != nil ||
		initial.FactSetEvent.EventRawSha256 != catalog.EventRawSha256 ||
		initial.FactSetEvent.FactSetJcsSha256 != catalog.FactSetJcsSha256 {
		t.Fatalf("unjudged historical Catalog was treated as quality evidence: %+v", initial)
	}
	judgments := make([]types.WikiFactJudgmentRecord, 0, 2)
	for i, fact := range catalog.Facts {
		request := snapshotQualityRequest(catalog, fact, "snapshot-judgment-"+string(rune('a'+i)))
		judgment, judgeErr := quality.JudgeWikiFact(ctx, "test-admin", request)
		judgments = append(judgments, must(t, judgment, judgeErr))
	}
	ready, err := quality.ReadWikiQualitySourceSnapshot(ctx, target)
	ready = must(t, ready, err)
	if !ready.AllRequiredHaveHead || !ready.WikiAndSourcesAvailableAtRead ||
		len(ready.Sources) != 2 ||
		len(ready.RequiredFacts) != 2 || ready.FactSet.FactSetRevisionId != catalog.FactSetRevisionId {
		t.Fatalf("complete required-head read lost pinned Catalog: %+v", ready)
	}
	for i, entry := range ready.RequiredFacts {
		if entry.Judgment == nil || entry.Event == nil || entry.SourceWithdrawnAtRead ||
			entry.Judgment.JudgeRevisionId != judgments[i].JudgeRevisionId ||
			entry.Judgment.EventId != judgments[i].EventId ||
			entry.Judgment.FactId != catalog.Facts[i].FactId ||
			object.Hash([]byte(entry.Event.EventJson)) != judgments[i].EventRawSha256 ||
			entry.Event.EventJcsSha256 != judgments[i].EventJcsSha256 {
			t.Fatalf("required Fact %d escaped same-Wiki original head/Event check: %+v", i, entry)
		}
	}
	manual, err := ordinary.CreateWiki(ctx, "test-admin", types.CreateWikiReq{
		ModuleId: module.Id, PageId: wiki.EntityId, BaseRevisionId: wiki.RevisionId,
		Title: "new manual Wiki target", Content: "later human edition of both facts",
		SourceRefs: []types.SourceRef{{RevisionId: sources[0].RevisionId, Locator: "paragraph:2"}},
		IdempotencyKey: "snapshot-later-manual-wiki"})
	manual = must(t, manual, err)
	manualReq := req
	manualReq.WikiRevisionId = manual.RevisionId
	manualReq.OriginCompileId = ""
	manualReq.BaseFactSetRevisionId = catalog.FactSetRevisionId
	manualReq.IdempotencyKey = "snapshot-later-manual-fact-set"
	newCatalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", manualReq)
	newCatalog = must(t, newCatalog, err)
	currentScope, err := sets.GetWikiFactSetScope(ctx, types.WikiFactSetScopePath{
		ModuleId: module.Id, PageId: wiki.EntityId,
		SourceScopeRevision: catalog.SourceScopeRevision})
	currentScope = must(t, currentScope, err)
	stillHistorical, err := quality.ReadWikiQualitySourceSnapshot(ctx, target)
	stillHistorical = must(t, stillHistorical, err)
	if currentScope.FactSetRevisionId != newCatalog.FactSetRevisionId ||
		stillHistorical.FactSet.FactSetRevisionId != catalog.FactSetRevisionId ||
		stillHistorical.FactSet.WikiRevisionId != wiki.RevisionId ||
		!stillHistorical.AllRequiredHaveHead {
		t.Fatalf("new scope head was mistaken for old target: current=%+v pinned=%+v",
			currentScope, stillHistorical)
	}
	wrong := target
	wrong.WikiRevisionID = "wiki_unrelated"
	if _, err := quality.ReadWikiQualitySourceSnapshot(ctx, wrong); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("historical Catalog accepted a different Wiki target: %v", err)
	}
	wrong = target
	wrong.SourceScopeRevision = "scope_" + object.Hash([]byte("other approved scope"))
	if _, err := quality.ReadWikiQualitySourceSnapshot(ctx, wrong); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("historical Catalog accepted a different Source scope: %v", err)
	}
	defaultOff := model.New(ordinary.DB, ordinary.Objects)
	if _, err := defaultOff.ReadWikiQualitySourceSnapshot(ctx, target); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("default-off Store exposed new source snapshot: %v", err)
	}
}

func TestWikiQualitySourceSnapshotDoesNotMixLaterJudgmentAndWithdrawal(t *testing.T) {
	ordinary, sets, module, _, _, _, req := wikiFactSetFixture(t)
	quality := model.New(ordinary.DB, ordinary.Objects,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	catalog, err := sets.FreezeWikiFactSet(ctx, "test-admin", req)
	catalog = must(t, catalog, err)
	judgments := make([]types.WikiFactJudgmentRecord, 0, 2)
	for i, fact := range catalog.Facts {
		request := snapshotQualityRequest(catalog, fact, "snapshot-before-"+string(rune('a'+i)))
		judgment, judgeErr := quality.JudgeWikiFact(ctx, "test-admin", request)
		judgments = append(judgments, must(t, judgment, judgeErr))
	}
	var after types.WikiFactJudgmentRecord
	var callbackErr error
	interleaved := &snapshotInterleavingObjects{Store: ordinary.Objects}
	interleaved.onRead = func() {
		rejudge := snapshotQualityRequest(catalog, catalog.Facts[0], "snapshot-after-head-move")
		rejudge.BaseJudgeRevisionId = judgments[0].JudgeRevisionId
		rejudge.Reason = "a later admin revision after the first snapshot SQL read"
		after, callbackErr = quality.JudgeWikiFact(ctx, "test-admin", rejudge)
		if callbackErr != nil {
			return
		}
		_, callbackErr = ordinary.Withdraw(ctx, "test-admin", types.WithdrawReq{
			ModuleId: module.Id, TargetKind: "revision",
			TargetId:       catalog.Facts[1].SourceRevisionId,
			Reason:         "withdraw the second Source after the snapshot starts",
			IdempotencyKey: "snapshot-interleaved-source-withdrawal"})
	}
	reader := model.New(ordinary.DB, interleaved,
		model.WithWikiFactSets(), model.WithWikiQualityJudgments())
	before, err := reader.ReadWikiQualitySourceSnapshot(ctx, snapshotTarget(catalog))
	before = must(t, before, err)
	if callbackErr != nil || after.JudgeRevisionId == "" {
		t.Fatalf("interleaved later writes did not commit: after=%+v err=%v", after, callbackErr)
	}
	if !before.WikiAndSourcesAvailableAtRead || !before.AllRequiredHaveHead ||
		before.RequiredFacts[0].Judgment == nil ||
		before.RequiredFacts[0].Judgment.JudgeRevisionId != judgments[0].JudgeRevisionId ||
		before.RequiredFacts[1].SourceWithdrawnAtRead || before.Sources[1].Withdrawn {
		t.Fatalf("one PG snapshot mixed post-read head/withdrawal: %+v", before)
	}
	current, err := quality.ReadWikiQualitySourceSnapshot(ctx, snapshotTarget(catalog))
	current = must(t, current, err)
	if current.WikiAndSourcesAvailableAtRead || !current.AllRequiredHaveHead ||
		current.RequiredFacts[0].Judgment == nil ||
		current.RequiredFacts[0].Judgment.JudgeRevisionId != after.JudgeRevisionId ||
		!current.RequiredFacts[1].SourceWithdrawnAtRead {
		t.Fatalf("later PG snapshot did not see committed head/withdrawal: %+v", current)
	}
	if before.FactSet.FactSetRevisionId != current.FactSet.FactSetRevisionId ||
		before.FactSetEvent.EventRawSha256 != current.FactSetEvent.EventRawSha256 {
		t.Fatal("later withdrawal changed immutable historical Catalog source")
	}
}
