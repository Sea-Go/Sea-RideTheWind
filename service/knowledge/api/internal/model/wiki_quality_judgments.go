package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

type wikiQualityTarget struct {
	wiki, source    types.Revision
	originKind      string
	originCompileID string
	sourceStart     int
	sourceEnd       int
	citationPresent bool
}

// A missing fact may lack a citation in the current manual Wiki, but it must
// still belong to this page's immutable source lineage. Otherwise an admin
// could label an unrelated module Source as a missing page fact.
func manualWikiFactSourceScope(ctx context.Context, tx pgx.Tx,
	wiki types.Revision, sourceID string) (bool, error) {
	seen := map[string]bool{}
	for depth := 0; depth < 64; depth++ {
		if seen[wiki.RevisionId] || wiki.Kind != "wiki" {
			return false, ErrArtifactUnavailable
		}
		seen[wiki.RevisionId] = true
		for _, ref := range wiki.SourceRefs {
			if ref.RevisionId == sourceID {
				return true, nil
			}
		}
		if strings.HasPrefix(wiki.CreatedBy, "btw.compile/") {
			compileID := strings.TrimPrefix(wiki.CreatedBy, "btw.compile/")
			compile, err := readJSON[types.Compile](ctx, tx,
				"SELECT data FROM knowledge_compiles WHERE id=$1 AND module_id=$2 AND page_id=$3",
				compileID, wiki.ModuleId, wiki.EntityId)
			if err != nil || compile.State != "ACCEPTED" || compile.RevisionId != wiki.RevisionId {
				return false, ErrArtifactUnavailable
			}
			for _, id := range compile.SourceRevisionIds {
				if id == sourceID {
					return true, nil
				}
			}
		}
		if wiki.BaseRevisionId == "" {
			return false, nil
		}
		base, err := revision(ctx, tx, wiki.BaseRevisionId)
		if err != nil || base.ModuleId != wiki.ModuleId || base.EntityId != wiki.EntityId ||
			base.Kind != "wiki" {
			return false, ErrArtifactUnavailable
		}
		wiki = base
	}
	return false, conflict("manual Wiki fact source lineage exceeds v1 bound")
}

// CheckWikiQualitySchema runs only when the new admin capture mode is enabled.
// An old database without the isolated sidecar fails before accepting HTTP.
func (s *Store) CheckWikiQualitySchema(ctx context.Context) error {
	for _, query := range []string{
		"SELECT revision_id,judgment_id,module_id,page_id,wiki_revision_id,fact_id,base_judge_revision_id,judge_revision,assessment,grade,data FROM knowledge_wiki_quality_revisions LIMIT 0",
		"SELECT wiki_revision_id,fact_id,judgment_id,revision_id FROM knowledge_wiki_quality_heads LIMIT 0",
		"SELECT event_id,revision_id,event_json,event_raw_sha256,event_jcs_sha256 FROM knowledge_wiki_quality_events LIMIT 0",
	} {
		rows, err := s.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) wikiQualityTarget(ctx context.Context, tx pgx.Tx,
	req types.JudgeWikiFactReq, grade *int) (wikiQualityTarget, error) {
	var target wikiQualityTarget
	wiki, err := revision(ctx, tx, req.WikiRevisionId)
	if err != nil {
		return target, err
	}
	if wiki.Kind != "wiki" || wiki.ModuleId != req.ModuleId || wiki.EntityId != req.PageId ||
		!citationHash(wiki.ContentHash) || wiki.ObjectKey != object.Key(wiki.ContentHash) {
		return target, invalid("quality target is not this module/page Wiki revision")
	}
	wikiBytes, err := s.Objects.Get(ctx, wiki.ObjectKey, wiki.ContentHash)
	if err != nil || object.Hash(wikiBytes) != wiki.ContentHash {
		return target, ErrArtifactUnavailable
	}
	wiki.Content = string(wikiBytes)
	if req.WikiClaimText != "" && !strings.Contains(wiki.Content, req.WikiClaimText) {
		return target, invalid("human claim quote is absent from original Wiki bytes")
	}
	if wiki.Withdrawn && req.Assessment != "undetermined" {
		return target, conflict("withdrawn Wiki can only be reassessed as undetermined")
	}
	target.wiki = wiki
	if strings.HasPrefix(wiki.CreatedBy, "btw.compile/") {
		compileID := strings.TrimPrefix(wiki.CreatedBy, "btw.compile/")
		if !citationIdentity(compileID) || (req.OriginCompileId != "" && req.OriginCompileId != compileID) {
			return target, invalid("AI Wiki origin compile differs from immutable provenance")
		}
		compile, err := readJSON[types.Compile](ctx, tx,
			"SELECT data FROM knowledge_compiles WHERE id=$1 AND module_id=$2 AND page_id=$3",
			compileID, req.ModuleId, req.PageId)
		if err != nil || compile.State != "ACCEPTED" || compile.RevisionId != wiki.RevisionId ||
			compile.ModuleId != wiki.ModuleId || compile.PageId != wiki.EntityId {
			return target, conflict("AI Wiki target lacks matching accepted Compile")
		}
		found := false
		for _, id := range compile.SourceRevisionIds {
			if id == req.SourceRevisionId {
				found = true
			}
		}
		if !found {
			return target, invalid("fact source was not in this accepted Compile")
		}
		target.originKind, target.originCompileID = "ai_accepted", compileID
	} else {
		if req.OriginCompileId != "" || wiki.CreatedBy == "" ||
			len(wiki.SourceRefs) < 1 || len(wiki.SourceRefs) > 64 {
			return target, invalid("manual Wiki target needs its own actor/source refs, not old Compile ID")
		}
		if wiki.BaseRevisionId != "" {
			base, err := revision(ctx, tx, wiki.BaseRevisionId)
			if err != nil || base.Kind != "wiki" || base.ModuleId != wiki.ModuleId || base.EntityId != wiki.EntityId {
				return target, invalid("manual Wiki base chain is not the same page")
			}
		}
		inScope, err := manualWikiFactSourceScope(ctx, tx, wiki, req.SourceRevisionId)
		if err != nil {
			return target, err
		}
		if !inScope {
			return target, invalid("fact source is not in this manual Wiki page lineage")
		}
		target.originKind = "manual_revision"
	}
	for _, ref := range wiki.SourceRefs {
		if ref.RevisionId == req.SourceRevisionId && ref.Locator == req.Locator {
			target.citationPresent = true
		}
	}
	if grade != nil && *grade >= 2 && !target.citationPresent {
		return target, invalid("grade 2-3 requires this exact source paragraph citation")
	}
	source, err := revision(ctx, tx, req.SourceRevisionId)
	if err != nil {
		return target, err
	}
	if source.Kind != "source" || source.ModuleId != req.ModuleId ||
		source.ContentHash != req.SourceContentSha256 ||
		!citationHash(source.ContentHash) || source.ObjectKey != object.Key(source.ContentHash) {
		return target, invalid("fact source identity/SHA differs from immutable revision")
	}
	sourceBytes, err := s.Objects.Get(ctx, source.ObjectKey, source.ContentHash)
	if err != nil || object.Hash(sourceBytes) != source.ContentHash {
		return target, ErrArtifactUnavailable
	}
	source.Content = string(sourceBytes)
	_, paragraphStart, paragraphEnd, ok := citationParagraph(source.Content, req.Locator)
	if !ok || paragraphStart < 0 || paragraphEnd <= paragraphStart || paragraphEnd > len(sourceBytes) {
		return target, invalid("fact locator is absent from original paragraph bytes")
	}
	// citationParagraph locates the whole block. The event's byte span refers
	// to the selected quote itself, choosing its first exact occurrence within
	// that immutable block if the same quote appears more than once.
	quoteOffset := bytes.Index(sourceBytes[paragraphStart:paragraphEnd], []byte(req.SourceQuote))
	if quoteOffset < 0 {
		return target, invalid("fact quote is absent from original paragraph byte range")
	}
	if source.Withdrawn && req.Assessment != "undetermined" {
		return target, conflict("withdrawn Source can only be reassessed as undetermined")
	}
	target.source = source
	target.sourceStart = paragraphStart + quoteOffset
	target.sourceEnd = target.sourceStart + len(req.SourceQuote)
	return target, nil
}

func wikiQualityRecord(r wikiQualityRevision, eventID, rawSHA, jcsSHA string) types.WikiFactJudgmentRecord {
	grade := ""
	if r.Grade != nil {
		grade = strconv.Itoa(*r.Grade)
	}
	return types.WikiFactJudgmentRecord{SchemaVersion: r.SchemaVersion,
		JudgmentId: r.JudgmentID, FactId: r.FactID, JudgeRevisionId: r.JudgeRevisionID,
		JudgeRevision: r.JudgeRevision, BaseJudgeRevisionId: r.BaseJudgeRevisionID,
		ModuleId: r.ModuleID, PageId: r.PageID, WikiRevisionId: r.WikiRevisionID,
		BaseWikiRevisionId: r.BaseWikiRevisionID, WikiOriginKind: r.WikiOriginKind,
		OriginCompileId: r.OriginCompileID, WikiContentSha256: r.WikiContentSHA256,
		SourceRevisionId: r.SourceRevisionID, SourceContentSha256: r.SourceContentSHA256,
		Locator: r.Locator, SourceByteStart: r.SourceByteStart, SourceByteEnd: r.SourceByteEnd,
		SourceQuote: r.SourceQuote, SourceQuoteSha256: r.SourceQuoteSHA256,
		WikiClaimText: r.WikiClaimText, WikiClaimSha256: r.WikiClaimSHA256,
		CitationPresent: r.CitationPresent, Assessment: r.Assessment, Grade: grade,
		RubricVersion: r.RubricVersion, Reason: r.Reason, ActorId: r.ActorID,
		JudgedAt: r.JudgedAt, SourceWithdrawn: r.SourceWithdrawn, WikiWithdrawn: r.WikiWithdrawn,
		EventId: eventID, EventRawSha256: rawSHA, EventJcsSha256: jcsSHA}
}

func (s *Store) JudgeWikiFact(ctx context.Context, actor string,
	req types.JudgeWikiFactReq) (types.WikiFactJudgmentRecord, error) {
	return observe(ctx, s, "knowledge.wiki.quality.judge",
		operationID("wiki-quality/"+req.WikiRevisionId+"/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.WikiFactJudgmentRecord, error) {
			if !s.wikiQualityJudgments {
				return types.WikiFactJudgmentRecord{}, ErrUnavailable
			}
			grade, err := validateWikiQualityRequest(actor, req)
			if err != nil {
				return types.WikiFactJudgmentRecord{}, err
			}
			factID := wikiQualityFactID(req.SourceRevisionId, req.Locator, req.SourceQuoteSha256)
			scope := "wiki-quality/" + req.WikiRevisionId + "/" + actor
			return command(ctx, s, scope, req.IdempotencyKey, req,
				func(tx pgx.Tx) (types.WikiFactJudgmentRecord, error) {
					m, err := module(ctx, tx, req.ModuleId, true)
					if err != nil {
						return types.WikiFactJudgmentRecord{}, err
					}
					if err = enabled(m); err != nil {
						return types.WikiFactJudgmentRecord{}, err
					}
					return s.commitWikiQualityFact(ctx, tx, actor, req, factID, grade)
				})
		})
}

func (s *Store) commitWikiQualityFact(ctx context.Context, tx pgx.Tx, actor string,
	req types.JudgeWikiFactReq, factID string, grade *int) (types.WikiFactJudgmentRecord, error) {
	var priorID, judgmentID string
	err := tx.QueryRow(ctx, `SELECT judgment_id,revision_id FROM knowledge_wiki_quality_heads
 WHERE wiki_revision_id=$1 AND fact_id=$2`, req.WikiRevisionId, factID).Scan(&judgmentID, &priorID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.WikiFactJudgmentRecord{}, err
	}
	if priorID != req.BaseJudgeRevisionId {
		return types.WikiFactJudgmentRecord{}, conflict("Wiki fact judgment base no longer current")
	}
	var priorRevision int64
	if priorID != "" {
		previous, err := readJSON[wikiQualityRevision](ctx, tx,
			"SELECT data FROM knowledge_wiki_quality_revisions WHERE revision_id=$1", priorID)
		if err != nil || previous.JudgeRevisionID != priorID || previous.JudgmentID != judgmentID ||
			previous.WikiRevisionID != req.WikiRevisionId || previous.FactID != factID ||
			previous.SourceRevisionID != req.SourceRevisionId || previous.Locator != req.Locator ||
			previous.SourceQuoteSHA256 != req.SourceQuoteSha256 {
			return types.WikiFactJudgmentRecord{}, ErrArtifactUnavailable
		}
		priorRevision, err = strconv.ParseInt(previous.JudgeRevision, 10, 64)
		if err != nil || priorRevision < 1 || strconv.FormatInt(priorRevision, 10) != previous.JudgeRevision ||
			priorRevision == int64(^uint64(0)>>1) {
			return types.WikiFactJudgmentRecord{}, ErrArtifactUnavailable
		}
	}
	target, err := s.wikiQualityTarget(ctx, tx, req, grade)
	if err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	if judgmentID == "" {
		judgmentID = id("wiki_judgment")
	}
	var judgedAt time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&judgedAt); err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	r := wikiQualityRevision{SchemaVersion: wikiQualityRevisionSchema,
		JudgmentSource: wikiQualityJudgmentSource, JudgmentID: judgmentID,
		FactID: factID, JudgeRevisionID: id("judge_revision"),
		JudgeRevision:       strconv.FormatInt(priorRevision+1, 10),
		BaseJudgeRevisionID: req.BaseJudgeRevisionId,
		ModuleID:            req.ModuleId, PageID: req.PageId,
		WikiRevisionID: req.WikiRevisionId, BaseWikiRevisionID: target.wiki.BaseRevisionId,
		WikiOriginKind: target.originKind, OriginCompileID: target.originCompileID,
		WikiContentSHA256: target.wiki.ContentHash, SourceRevisionID: req.SourceRevisionId,
		SourceContentSHA256: target.source.ContentHash, Locator: req.Locator,
		SourceByteStart: strconv.Itoa(target.sourceStart), SourceByteEnd: strconv.Itoa(target.sourceEnd),
		SourceQuote: req.SourceQuote, SourceQuoteSHA256: req.SourceQuoteSha256,
		WikiClaimText: req.WikiClaimText, WikiClaimSHA256: req.WikiClaimSha256,
		CitationPresent: target.citationPresent, Assessment: req.Assessment, Grade: grade,
		RubricVersion: wikiQualityRubric, Reason: req.Reason, ActorID: actor,
		JudgedAt:        judgedAt.UTC().Format(time.RFC3339Nano),
		SourceWithdrawn: target.source.Withdrawn, WikiWithdrawn: target.wiki.Withdrawn}
	judgeRevision := priorRevision + 1
	if err := saveJSON(ctx, tx, `INSERT INTO knowledge_wiki_quality_revisions
 (revision_id,judgment_id,module_id,page_id,wiki_revision_id,fact_id,
 base_judge_revision_id,judge_revision,assessment,grade,data)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r,
		r.JudgeRevisionID, judgmentID, req.ModuleId, req.PageId, req.WikiRevisionId,
		factID, req.BaseJudgeRevisionId, judgeRevision, req.Assessment, grade); err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	if priorID == "" {
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_wiki_quality_heads
 (wiki_revision_id,fact_id,judgment_id,revision_id) VALUES($1,$2,$3,$4)`,
			req.WikiRevisionId, factID, judgmentID, r.JudgeRevisionID)
	} else {
		tag, updateErr := tx.Exec(ctx, `UPDATE knowledge_wiki_quality_heads SET revision_id=$4
 WHERE wiki_revision_id=$1 AND fact_id=$2 AND revision_id=$3`,
			req.WikiRevisionId, factID, priorID, r.JudgeRevisionID)
		err = updateErr
		if err == nil && tag.RowsAffected() != 1 {
			err = conflict("Wiki fact judgment head moved before CAS")
		}
	}
	if err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	event, eventJSON, err := emitWithReceipt(ctx, tx, wikiQualityEventType, req.ModuleId, r)
	if err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	rawSHA := object.Hash(eventJSON)
	jcsSHA, err := wikiQualityJCSHash(eventJSON)
	if err != nil {
		return types.WikiFactJudgmentRecord{}, ErrArtifactUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_wiki_quality_events
 (event_id,revision_id,event_json,event_raw_sha256,event_jcs_sha256)
 VALUES($1,$2,$3,$4,$5)`, event.EventID, r.JudgeRevisionID,
		string(eventJSON), rawSHA, jcsSHA)
	if err != nil {
		return types.WikiFactJudgmentRecord{}, err
	}
	return wikiQualityRecord(r, event.EventID, rawSHA, jcsSHA), nil
}

func qualityJudgmentTarget(ctx context.Context, s *Store, moduleID, pageID, wikiID string) error {
	if !citationIdentity(moduleID) || !qualityPageID(pageID) || !citationIdentity(wikiID) {
		return invalid("module/page/Wiki revision identity required")
	}
	wiki, err := revision(ctx, s.DB, wikiID)
	if err != nil {
		return err
	}
	if wiki.Kind != "wiki" || wiki.ModuleId != moduleID || wiki.EntityId != pageID {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetWikiFactJudgment(ctx context.Context,
	req types.WikiFactJudgmentPath) (types.WikiFactJudgmentRecord, error) {
	return observe(ctx, s, "knowledge.wiki.quality.get", "wiki-quality-read:"+req.WikiRevisionId+"/"+req.FactId,
		req, func(ctx context.Context) (types.WikiFactJudgmentRecord, error) {
			if !s.wikiQualityJudgments {
				return types.WikiFactJudgmentRecord{}, ErrUnavailable
			}
			if err := qualityJudgmentTarget(ctx, s, req.ModuleId, req.PageId, req.WikiRevisionId); err != nil {
				return types.WikiFactJudgmentRecord{}, err
			}
			if !validWikiFactID(req.FactId) {
				return types.WikiFactJudgmentRecord{}, invalid("canonical fact ID required")
			}
			var raw []byte
			var eventID, rawSHA, jcsSHA string
			err := s.DB.QueryRow(ctx, `SELECT r.data,e.event_id,e.event_raw_sha256,e.event_jcs_sha256
 FROM knowledge_wiki_quality_heads h
 JOIN knowledge_wiki_quality_revisions r ON r.revision_id=h.revision_id
 JOIN knowledge_wiki_quality_events e ON e.revision_id=r.revision_id
 WHERE h.wiki_revision_id=$1 AND h.fact_id=$2`, req.WikiRevisionId, req.FactId).
				Scan(&raw, &eventID, &rawSHA, &jcsSHA)
			if errors.Is(err, pgx.ErrNoRows) {
				return types.WikiFactJudgmentRecord{}, ErrNotFound
			}
			if err != nil {
				return types.WikiFactJudgmentRecord{}, err
			}
			var record wikiQualityRevision
			if json.Unmarshal(raw, &record) != nil || record.WikiRevisionID != req.WikiRevisionId ||
				record.FactID != req.FactId || record.ModuleID != req.ModuleId || record.PageID != req.PageId ||
				record.SchemaVersion != wikiQualityRevisionSchema || record.JudgmentSource != wikiQualityJudgmentSource ||
				!citationHash(rawSHA) || !citationHash(jcsSHA) {
				return types.WikiFactJudgmentRecord{}, ErrArtifactUnavailable
			}
			return wikiQualityRecord(record, eventID, rawSHA, jcsSHA), nil
		})
}

func (s *Store) ListWikiFactJudgments(ctx context.Context,
	req types.ListWikiFactJudgmentsReq) (types.ListWikiFactJudgmentsResp, error) {
	return observe(ctx, s, "knowledge.wiki.quality.list", "wiki-quality-list:"+req.WikiRevisionId,
		req, func(ctx context.Context) (types.ListWikiFactJudgmentsResp, error) {
			var result types.ListWikiFactJudgmentsResp
			result.Items = []types.WikiFactJudgmentRecord{}
			if !s.wikiQualityJudgments {
				return result, ErrUnavailable
			}
			if err := qualityJudgmentTarget(ctx, s, req.ModuleId, req.PageId, req.WikiRevisionId); err != nil {
				return result, err
			}
			if req.Limit < 1 || req.Limit > 100 || (req.Cursor != "" && !validWikiFactID(req.Cursor)) {
				return result, invalid("bounded fact judgment list/cursor required")
			}
			rows, err := s.DB.Query(ctx, `SELECT h.fact_id,r.data,e.event_id,e.event_raw_sha256,e.event_jcs_sha256
 FROM knowledge_wiki_quality_heads h
 JOIN knowledge_wiki_quality_revisions r ON r.revision_id=h.revision_id
 JOIN knowledge_wiki_quality_events e ON e.revision_id=r.revision_id
 WHERE h.wiki_revision_id=$1 AND h.fact_id>$2
 ORDER BY h.fact_id LIMIT $3`, req.WikiRevisionId, req.Cursor, req.Limit+1)
			if err != nil {
				return result, err
			}
			defer rows.Close()
			for rows.Next() {
				var factID, eventID, rawSHA, jcsSHA string
				var raw []byte
				if err = rows.Scan(&factID, &raw, &eventID, &rawSHA, &jcsSHA); err != nil {
					return result, err
				}
				var record wikiQualityRevision
				if json.Unmarshal(raw, &record) != nil || record.FactID != factID ||
					record.WikiRevisionID != req.WikiRevisionId || record.ModuleID != req.ModuleId ||
					record.PageID != req.PageId || record.SchemaVersion != wikiQualityRevisionSchema ||
					!citationHash(rawSHA) || !citationHash(jcsSHA) {
					return result, ErrArtifactUnavailable
				}
				if len(result.Items) == req.Limit {
					result.NextCursor = result.Items[len(result.Items)-1].FactId
					break
				}
				result.Items = append(result.Items, wikiQualityRecord(record, eventID, rawSHA, jcsSHA))
			}
			if err = rows.Err(); err != nil {
				return result, err
			}
			return result, nil
		})
}

func (s *Store) GetWikiPageHead(ctx context.Context,
	req types.WikiPageHeadPath) (types.WikiPageHeadSnapshot, error) {
	return observe(ctx, s, "knowledge.wiki.head.get", "wiki-head:"+req.ModuleId+"/"+req.PageId,
		req, func(ctx context.Context) (types.WikiPageHeadSnapshot, error) {
			result := types.WikiPageHeadSnapshot{ModuleId: req.ModuleId, PageId: req.PageId}
			if !s.wikiQualityJudgments {
				return result, ErrUnavailable
			}
			if !citationIdentity(req.ModuleId) || !qualityPageID(req.PageId) {
				return result, invalid("module/page head identity required")
			}
			if _, err := module(ctx, s.DB, req.ModuleId, false); err != nil {
				return result, err
			}
			var revisionID string
			err := s.DB.QueryRow(ctx, `SELECT revision_id FROM knowledge_heads
 WHERE module_id=$1 AND kind='wiki' AND entity_id=$2`, req.ModuleId, req.PageId).Scan(&revisionID)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, nil // a new page has no editable revision yet
			}
			if err != nil {
				return result, err
			}
			wiki, err := revision(ctx, s.DB, revisionID)
			if err != nil || wiki.Kind != "wiki" || wiki.ModuleId != req.ModuleId || wiki.EntityId != req.PageId {
				return result, ErrArtifactUnavailable
			}
			result.RevisionId, result.BaseRevisionId = wiki.RevisionId, wiki.BaseRevisionId
			result.ContentSha256, result.Title = wiki.ContentHash, wiki.Title
			result.CreatedBy, result.Withdrawn = wiki.CreatedBy, wiki.Withdrawn
			return result, nil
		})
}
