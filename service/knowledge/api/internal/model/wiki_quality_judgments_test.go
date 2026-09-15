package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
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

func wikiQualityFixture(t *testing.T) (*model.Store, types.Module, types.Revision, types.Compile, types.Revision) {
	t.Helper()
	ordinary := testenv.Store(t)
	quality := model.New(ordinary.DB, ordinary.Objects, model.WithWikiQualityJudgments())
	if err := quality.CheckWikiQualitySchema(ctx); err != nil {
		t.Fatal(err)
	}
	module := createModule(t, ordinary, "Wiki fact human judgment")
	source := source(t, ordinary, module, "Wiki quality source")
	compile, err := ordinary.CreateCompile(ctx, "admin-1", types.CreateCompileReq{ModuleId: module.Id,
		PageId: "《可维护事实页》", SourceRevisionIds: []string{source.RevisionId},
		Guidance: "only use the frozen source fact", IdempotencyKey: "wiki-quality-compile"})
	compile = must(t, compile, err)
	_, err = ordinary.ClaimCompile(ctx, types.ClaimCompileReq{CompileId: compile.CompileId,
		Generation: compile.Generation, InputHash: compile.InputHash,
		AttemptId: "quality-attempt-1", LeaseEpoch: 1,
		LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	markdown := "事实原文：Second paragraph.\n这条事实来自冻结资料。"
	key, hash, err := ordinary.Objects.Put(ctx, []byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := ordinary.AcceptCompile(ctx, types.AcceptCompileReq{CompileId: compile.CompileId,
		Generation: compile.Generation, InputHash: compile.InputHash,
		AttemptId: "quality-attempt-1", LeaseEpoch: 1,
		ObjectKey: key, ContentHash: hash, Title: "可维护事实页",
		SourceRefs: []types.SourceRef{{RevisionId: source.RevisionId, Locator: "paragraph:3"}}})
	accepted = must(t, accepted, err)
	wiki, err := ordinary.GetRevision(ctx, accepted.RevisionId)
	wiki = must(t, wiki, err)
	return quality, module, source, accepted, wiki
}

func wikiQualityRequest(module types.Module, source types.Revision,
	wiki types.Revision, compileID, key string) types.JudgeWikiFactReq {
	quote := "Second paragraph."
	return types.JudgeWikiFactReq{ModuleId: module.Id, PageId: wiki.EntityId,
		WikiRevisionId: wiki.RevisionId, OriginCompileId: compileID,
		SourceRevisionId: source.RevisionId, SourceContentSha256: source.ContentHash,
		Locator: "paragraph:3", SourceQuote: quote, SourceQuoteSha256: object.Hash([]byte(quote)),
		WikiClaimText: quote, WikiClaimSha256: object.Hash([]byte(quote)),
		Assessment: "covered", Grade: "3", RubricVersion: "sea.wiki.fact-coverage.v1",
		Reason:         "frozen source paragraph and original Wiki claim match with a faithful citation",
		IdempotencyKey: key}
}

func TestWikiQualityFactAIManualRejudgeOriginalEventAndWithdrawal(t *testing.T) {
	quality, module, source, compile, aiWiki := wikiQualityFixture(t)
	request := wikiQualityRequest(module, source, aiWiki, compile.CompileId, "human-wiki-quality-first")
	first, err := quality.JudgeWikiFact(ctx, "admin-1", request)
	first = must(t, first, err)
	if first.Grade != "3" || first.Assessment != "covered" || !first.CitationPresent ||
		first.WikiOriginKind != "ai_accepted" || first.OriginCompileId != compile.CompileId ||
		first.SourceContentSha256 != source.ContentHash || first.JudgeRevision != "1" ||
		first.FactId == "" || first.EventId == "" || first.EventRawSha256 == first.EventJcsSha256 {
		t.Fatalf("AI fact judgment lost independent source and Event hash domains: %+v", first)
	}
	originalSource, err := quality.GetRevision(ctx, source.RevisionId)
	originalSource = must(t, originalSource, err)
	quoteStart, startErr := strconv.Atoi(first.SourceByteStart)
	quoteEnd, endErr := strconv.Atoi(first.SourceByteEnd)
	if startErr != nil || endErr != nil || quoteStart < 0 || quoteEnd > len(originalSource.Content) ||
		quoteEnd <= quoteStart || originalSource.Content[quoteStart:quoteEnd] != request.SourceQuote {
		t.Fatalf("quality Event byte span does not name the selected source quote: %+v", first)
	}
	replay, err := quality.JudgeWikiFact(ctx, "admin-1", request)
	if err != nil || replay != first {
		t.Fatalf("same admin command did not replay original human receipt: %+v %v", replay, err)
	}
	conflict := request
	conflict.Reason = "a different claim under the same source command"
	if _, err := quality.JudgeWikiFact(ctx, "admin-1", conflict); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key/different human label body did not reject: %v", err)
	}
	wire, err := quality.GetWikiQualityEvent(ctx, first.EventId)
	wire = must(t, wire, err)
	canonical, err := jsoncanonicalizer.Transform([]byte(wire.EventJson))
	if err != nil || object.Hash([]byte(wire.EventJson)) != first.EventRawSha256 ||
		object.Hash(canonical) != first.EventJcsSha256 ||
		wire.EventRawSha256 != first.EventRawSha256 || wire.EventJcsSha256 != first.EventJcsSha256 ||
		bytes.Equal(canonical, []byte(wire.EventJson)) {
		t.Fatalf("private source Event bytes were confused with JCS/transport hash: %+v %v", wire, err)
	}
	var event model.Event
	if json.Unmarshal([]byte(wire.EventJson), &event) != nil ||
		event.EventType != "knowledge.wiki.quality.judged.v1" ||
		event.Producer != "ridethewind.knowledge" || event.AggregateID != module.Id {
		t.Fatalf("human quality EventSpec changed producer/aggregate: %+v", event)
	}
	var payload map[string]any
	if json.Unmarshal(event.Payload, &payload) != nil || payload["fact_id"] != first.FactId ||
		payload["judge_revision"] != "1" || payload["judgment_source"] != "human_admin_jwt_allowlist" ||
		payload["source_quote"] != request.SourceQuote || payload["wiki_origin_kind"] != "ai_accepted" ||
		payload["rubric_version"] != "sea.wiki.fact-coverage.v1" {
		t.Fatalf("Event payload lost frozen human fact scope: %+v", payload)
	}
	current, err := quality.GetWikiFactJudgment(ctx, types.WikiFactJudgmentPath{ModuleId: module.Id,
		PageId: request.PageId, WikiRevisionId: aiWiki.RevisionId, FactId: first.FactId})
	if err != nil || current.JudgeRevisionId != first.JudgeRevisionId || current.EventId != first.EventId {
		t.Fatalf("admin current judgment differs from original source Event: %+v %v", current, err)
	}
	page, err := quality.ListWikiFactJudgments(ctx, types.ListWikiFactJudgmentsReq{ModuleId: module.Id,
		PageId: request.PageId, WikiRevisionId: aiWiki.RevisionId, Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].FactId != first.FactId {
		t.Fatalf("Wiki fact judgment list lost per-revision fact key: %+v %v", page, err)
	}

	manual, err := quality.CreateWiki(ctx, "admin-2", types.CreateWikiReq{ModuleId: module.Id,
		PageId: request.PageId, BaseRevisionId: aiWiki.RevisionId, Title: "人工维护事实页",
		Content:        "补全限定：Second paragraph. 仅在冻结来源的语境成立。",
		SourceRefs:     []types.SourceRef{{RevisionId: source.RevisionId, Locator: "paragraph:3"}},
		IdempotencyKey: "human-maintained-wiki"})
	manual = must(t, manual, err)
	manualRequest := wikiQualityRequest(module, source, manual, "", "human-wiki-quality-manual")
	manualRequest.Grade, manualRequest.Reason = "2", "manual edit remains source-faithful but needs context"
	manualJudgment, err := quality.JudgeWikiFact(ctx, "admin-2", manualRequest)
	manualJudgment = must(t, manualJudgment, err)
	if manualJudgment.WikiOriginKind != "manual_revision" || manualJudgment.OriginCompileId != "" ||
		manualJudgment.FactId != first.FactId || manualJudgment.JudgeRevision != "1" ||
		manualJudgment.BaseWikiRevisionId != aiWiki.RevisionId || manualJudgment.Grade != "2" {
		t.Fatalf("human edit reused historical AI judgment/Compile identity: %+v", manualJudgment)
	}
	rejudge := request
	rejudge.BaseJudgeRevisionId, rejudge.IdempotencyKey = first.JudgeRevisionId, "human-wiki-quality-ai-rejudge"
	rejudge.Grade, rejudge.Reason = "2", "AI revision has an incomplete qualification, manual revision stays separate"
	second, err := quality.JudgeWikiFact(ctx, "admin-1", rejudge)
	second = must(t, second, err)
	if second.JudgeRevision != "2" || second.BaseJudgeRevisionId != first.JudgeRevisionId ||
		second.JudgmentId != first.JudgmentId || second.FactId != manualJudgment.FactId {
		t.Fatalf("AI rejudgment moved another Wiki revision's head: %+v", second)
	}
	stale := request
	stale.IdempotencyKey = "human-wiki-quality-stale-base"
	if _, err := quality.JudgeWikiFact(ctx, "admin-1", stale); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale fact judgment base escaped CAS: %v", err)
	}
	head, err := quality.GetWikiPageHead(ctx, types.WikiPageHeadPath{ModuleId: module.Id, PageId: request.PageId})
	if err != nil || head.RevisionId != manual.RevisionId || head.BaseRevisionId != aiWiki.RevisionId {
		t.Fatalf("judgment changed Wiki editing head instead of its own head: %+v %v", head, err)
	}
	var published int
	if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_publications WHERE module_id=$1", module.Id).Scan(&published); err != nil || published != 0 {
		t.Fatalf("human fact judgment automatically published a Release: count=%d err=%v", published, err)
	}
	_, err = quality.Withdraw(ctx, "admin-2", types.WithdrawReq{ModuleId: module.Id,
		TargetKind: "revision", TargetId: source.RevisionId, Reason: "source version is withdrawn",
		IdempotencyKey: "withdraw-source-after-human-quality"})
	if err != nil {
		t.Fatal(err)
	}
	afterWithdraw := manualRequest
	afterWithdraw.BaseJudgeRevisionId = manualJudgment.JudgeRevisionId
	afterWithdraw.IdempotencyKey = "human-wiki-quality-undetermined-after-withdrawal"
	afterWithdraw.Assessment, afterWithdraw.Grade = "undetermined", ""
	afterWithdraw.WikiClaimText, afterWithdraw.WikiClaimSha256 = "", ""
	afterWithdraw.Reason = "source withdrawn; historical fact quote remains, current judgment is undetermined"
	undetermined, err := quality.JudgeWikiFact(ctx, "admin-2", afterWithdraw)
	undetermined = must(t, undetermined, err)
	if undetermined.Assessment != "undetermined" || undetermined.Grade != "" ||
		!undetermined.SourceWithdrawn || undetermined.JudgeRevision != "2" ||
		undetermined.SourceQuoteSha256 != manualJudgment.SourceQuoteSha256 {
		t.Fatalf("withdrawn source was graded or lost original fact quote: %+v", undetermined)
	}
	badPositive := afterWithdraw
	badPositive.BaseJudgeRevisionId, badPositive.IdempotencyKey = undetermined.JudgeRevisionId, "positive-after-source-withdrawn"
	badPositive.Assessment, badPositive.Grade = "covered", "3"
	badPositive.WikiClaimText, badPositive.WikiClaimSha256 = request.WikiClaimText, request.WikiClaimSha256
	if _, err := quality.JudgeWikiFact(ctx, "admin-2", badPositive); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("withdrawn source received new positive quality grade: %v", err)
	}
	oldEvent, err := quality.GetWikiQualityEvent(ctx, first.EventId)
	if err != nil || oldEvent.EventJson != wire.EventJson || oldEvent.EventRawSha256 != wire.EventRawSha256 {
		t.Fatalf("later source withdrawal rewrote earlier human EventSpec: %+v %v", oldEvent, err)
	}
	// Existing Wiki/Compile workflow still reads its historical accepted bytes.
	accepted, err := quality.GetCompile(ctx, compile.CompileId)
	if err != nil || accepted.State != "ACCEPTED" || accepted.RevisionId != aiWiki.RevisionId {
		t.Fatalf("quality labels altered the original business Compile: %+v %v", accepted, err)
	}
}

func TestWikiQualityFactQuoteSpanPreservesCRLFIndentAndFirstRepeat(t *testing.T) {
	ordinary := testenv.Store(t)
	quality := model.New(ordinary.DB, ordinary.Objects, model.WithWikiQualityJudgments())
	module := createModule(t, ordinary, "Wiki quality raw paragraph")
	content := "header\r\n\r\n    prefix short fact\r\n    suffix short fact"
	source, err := ordinary.CreateSource(ctx, "admin-1", types.CreateSourceReq{ModuleId: module.Id,
		Title: "raw text source", Content: content, MediaType: "text/plain",
		Provenance: "synthetic CRLF/indent source", IdempotencyKey: "raw-quality-source"})
	source = must(t, source, err)
	wiki, err := ordinary.CreateWiki(ctx, "admin-1", types.CreateWikiReq{ModuleId: module.Id,
		PageId: "《缩进事实》", Title: "缩进事实", Content: "短句：short fact",
		SourceRefs:     []types.SourceRef{{RevisionId: source.RevisionId, Locator: "paragraph:2"}},
		IdempotencyKey: "raw-quality-wiki"})
	wiki = must(t, wiki, err)
	quote := "short fact"
	record, err := quality.JudgeWikiFact(ctx, "admin-1", types.JudgeWikiFactReq{
		ModuleId: module.Id, PageId: wiki.EntityId, WikiRevisionId: wiki.RevisionId,
		SourceRevisionId: source.RevisionId, SourceContentSha256: source.ContentHash,
		Locator: "paragraph:2", SourceQuote: quote, SourceQuoteSha256: object.Hash([]byte(quote)),
		WikiClaimText: quote, WikiClaimSha256: object.Hash([]byte(quote)),
		Assessment: "covered", Grade: "3", RubricVersion: "sea.wiki.fact-coverage.v1",
		Reason:         "first exact occurrence in the original indented CRLF paragraph",
		IdempotencyKey: "raw-quality-quote-judgment"})
	record = must(t, record, err)
	start, startErr := strconv.Atoi(record.SourceByteStart)
	end, endErr := strconv.Atoi(record.SourceByteEnd)
	if startErr != nil || endErr != nil || start != bytes.Index([]byte(content), []byte(quote)) ||
		end != start+len(quote) || content[start:end] != quote ||
		record.SourceContentSha256 != object.Hash([]byte(content)) ||
		record.WikiOriginKind != "manual_revision" {
		t.Fatalf("CRLF/indent Source quote was replaced by normalized paragraph or wrong repetition: %+v", record)
	}
	wire, err := quality.GetWikiQualityEvent(ctx, record.EventId)
	wire = must(t, wire, err)
	var event model.Event
	if json.Unmarshal([]byte(wire.EventJson), &event) != nil {
		t.Fatal("original quality Event bytes are not valid EventSpec")
	}
	var payload map[string]any
	if json.Unmarshal(event.Payload, &payload) != nil || payload["source_byte_start"] != record.SourceByteStart ||
		payload["source_byte_end"] != record.SourceByteEnd || payload["source_quote"] != quote {
		t.Fatalf("Event payload lost raw quote span after source normalization: %+v", payload)
	}
}

func TestWikiQualityRejectsBadSourceHashLocatorRubricAndGradeBeforeCommit(t *testing.T) {
	quality, module, source, compile, wiki := wikiQualityFixture(t)
	base := wikiQualityRequest(module, source, wiki, compile.CompileId, "quality-bad-source-base")
	for _, tc := range []struct {
		name string
		edit func(*types.JudgeWikiFactReq)
	}{
		{"source SHA", func(r *types.JudgeWikiFactReq) { r.SourceContentSha256 = strings.Repeat("0", 64) }},
		{"absent paragraph", func(r *types.JudgeWikiFactReq) { r.Locator = "paragraph:4" }},
		{"quote from another block", func(r *types.JudgeWikiFactReq) { r.Locator = "paragraph:2" }},
		{"wrong quote SHA", func(r *types.JudgeWikiFactReq) { r.SourceQuoteSha256 = strings.Repeat("0", 64) }},
		{"fake Wiki claim SHA", func(r *types.JudgeWikiFactReq) { r.WikiClaimSha256 = strings.Repeat("0", 64) }},
		{"unsupported rubric", func(r *types.JudgeWikiFactReq) { r.RubricVersion = "search.relevance.v1" }},
		{"missing with grade three", func(r *types.JudgeWikiFactReq) {
			r.Assessment, r.Grade, r.WikiClaimText, r.WikiClaimSha256 = "missing", "3", "", ""
		}},
		{"undetermined with numeric grade", func(r *types.JudgeWikiFactReq) {
			r.Assessment, r.Grade = "undetermined", "0"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.IdempotencyKey = "bad-quality-" + strings.ReplaceAll(tc.name, " ", "-")
			tc.edit(&bad)
			if _, err := quality.JudgeWikiFact(ctx, "admin-1", bad); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("bad source/locator/grade entered domain commit: %v", err)
			}
		})
	}
	var committed int
	if err := quality.DB.QueryRow(context.Background(), "SELECT count(*) FROM knowledge_wiki_quality_revisions").Scan(&committed); err != nil || committed != 0 {
		t.Fatalf("rejected quality requests created source labels: count=%d err=%v", committed, err)
	}
}

func TestWikiQualityManualMissingFactMustBelongToPageSourceLineage(t *testing.T) {
	quality, module, ownedSource, _, aiWiki := wikiQualityFixture(t)
	otherSource := source(t, quality, module, "Unrelated same-module source")
	manual, err := quality.CreateWiki(ctx, "admin-2", types.CreateWikiReq{ModuleId: module.Id,
		PageId: aiWiki.EntityId, BaseRevisionId: aiWiki.RevisionId,
		Title: "Human Wiki with one approved source", Content: "Only Second paragraph.",
		SourceRefs:     []types.SourceRef{{RevisionId: ownedSource.RevisionId, Locator: "paragraph:3"}},
		IdempotencyKey: "manual-page-lineage-approval"})
	manual = must(t, manual, err)
	quote := "First paragraph."
	request := types.JudgeWikiFactReq{ModuleId: module.Id, PageId: manual.EntityId,
		WikiRevisionId: manual.RevisionId, SourceRevisionId: otherSource.RevisionId,
		SourceContentSha256: otherSource.ContentHash, Locator: "paragraph:2",
		SourceQuote: quote, SourceQuoteSha256: object.Hash([]byte(quote)),
		Assessment: "missing", Grade: "0", RubricVersion: "sea.wiki.fact-coverage.v1",
		Reason:         "unrelated source should not contaminate this page's fact set",
		IdempotencyKey: "unrelated-manual-fact-label"}
	if _, err := quality.JudgeWikiFact(ctx, "admin-2", request); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("unapproved same-module source was called a missing manual Wiki fact: %v", err)
	}
	var labels int
	if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_wiki_quality_revisions").Scan(&labels); err != nil || labels != 0 {
		t.Fatalf("unrelated source created an Event/quality label: %d %v", labels, err)
	}
}

func TestWikiQualityDefaultOffAndReentrantSidecarMigration(t *testing.T) {
	ordinary := testenv.Store(t)
	module := createModule(t, ordinary, "Wiki quality migration")
	source := source(t, ordinary, module, "Migration source")
	wiki, err := ordinary.CreateWiki(ctx, "admin-1", types.CreateWikiReq{ModuleId: module.Id,
		PageId: "migration-wiki", Title: "Migration Wiki", Content: "First paragraph.",
		SourceRefs:     []types.SourceRef{{RevisionId: source.RevisionId, Locator: "paragraph:2"}},
		IdempotencyKey: "migration-existing-wiki"})
	wiki = must(t, wiki, err)
	request := wikiQualityRequest(module, source, wiki, "", "migration-quality-key")
	request.SourceQuote = "First paragraph."
	request.SourceQuoteSha256 = object.Hash([]byte(request.SourceQuote))
	request.WikiClaimText, request.WikiClaimSha256 = request.SourceQuote, request.SourceQuoteSha256
	if _, err := ordinary.JudgeWikiFact(ctx, "admin-1", request); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("old default-off Knowledge store accepted a new human quality label: %v", err)
	}
	for _, table := range []string{"knowledge_wiki_quality_events", "knowledge_wiki_quality_heads",
		"knowledge_wiki_quality_revisions"} {
		if _, err := ordinary.DB.Exec(ctx, "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../../scripts/migrate-wiki-quality.sql")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := ordinary.DB.Exec(ctx, string(migration), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("quality sidecar migration attempt=%d failed: %v", attempt+1, err)
		}
	}
	quality := model.New(ordinary.DB, ordinary.Objects, model.WithWikiQualityJudgments())
	if err := quality.CheckWikiQualitySchema(ctx); err != nil {
		t.Fatal(err)
	}
	var revisions int
	if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_wiki_quality_revisions").Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("reentrant migration manufactured historical human labels: %d %v", revisions, err)
	}
	if _, err := quality.GetRevision(ctx, source.RevisionId); err != nil {
		t.Fatalf("sidecar migration disturbed existing Source revision: %v", err)
	}
	if _, err := quality.GetRevision(ctx, wiki.RevisionId); err != nil {
		t.Fatalf("sidecar migration disturbed existing Wiki revision: %v", err)
	}
}

func TestWikiQualityOutboxFailureRollsBackJudgmentHeadAndReplay(t *testing.T) {
	quality, module, source, compile, wiki := wikiQualityFixture(t)
	request := wikiQualityRequest(module, source, wiki, compile.CompileId, "quality-outbox-failure")
	var originalOutbox int
	if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE aggregate_id=$1", module.Id).
		Scan(&originalOutbox); err != nil {
		t.Fatal(err)
	}
	_, err := quality.DB.Exec(ctx, `CREATE FUNCTION reject_quality_outbox_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.event_type='knowledge.wiki.quality.judged.v1' THEN
   RAISE EXCEPTION 'task-only simulated quality Event rejection';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_quality_outbox_for_test BEFORE INSERT ON knowledge_outbox
FOR EACH ROW EXECUTE FUNCTION reject_quality_outbox_for_test();`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quality.JudgeWikiFact(ctx, "admin-1", request); err == nil {
		t.Fatal("Outbox failure did not abort same-transaction judgment")
	}
	for _, table := range []string{"knowledge_wiki_quality_revisions", "knowledge_wiki_quality_heads",
		"knowledge_wiki_quality_events"} {
		var count int
		if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("Outbox failure left a partial human label in %s: %d %v", table, count, err)
		}
	}
	var afterOutbox, replayRows int
	if err := quality.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE aggregate_id=$1", module.Id).
		Scan(&afterOutbox); err != nil || afterOutbox != originalOutbox {
		t.Fatalf("failed quality Event changed original Wiki/Source Outbox rows: %d/%d %v",
			afterOutbox, originalOutbox, err)
	}
	if err := quality.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_operations
 WHERE scope=$1 AND operation_key=$2`, "wiki-quality/"+wiki.RevisionId+"/admin-1",
		request.IdempotencyKey).Scan(&replayRows); err != nil || replayRows != 0 {
		t.Fatalf("failed quality command left a replay row: %d %v", replayRows, err)
	}
}
