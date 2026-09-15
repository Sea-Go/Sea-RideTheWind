package model

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// An old database without the isolated append-only sidecar fails before the
// explicitly enabled administrator HTTP surface can accept a FactSet.
func (s *Store) CheckWikiFactSetSchema(ctx context.Context) error {
	for _, query := range []string{
		"SELECT revision_id,fact_set_id,module_id,page_id,source_scope_revision,wiki_revision_id,base_fact_set_revision_id,fact_set_revision,fact_set_jcs_sha256,facts_complete,data FROM knowledge_wiki_fact_set_revisions LIMIT 0",
		"SELECT module_id,page_id,source_scope_revision,fact_set_id,revision_id FROM knowledge_wiki_fact_set_heads LIMIT 0",
		"SELECT event_id,revision_id,event_json,event_raw_sha256,event_jcs_sha256 FROM knowledge_wiki_fact_set_events LIMIT 0",
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

func (s *Store) FreezeWikiFactSet(ctx context.Context, actor string,
	req types.FreezeWikiFactSetReq) (types.WikiFactSetRecord, error) {
	return observe(ctx, s, "knowledge.wiki.fact-set.freeze",
		operationID("wiki-fact-set/"+req.WikiRevisionId+"/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.WikiFactSetRecord, error) {
			if !s.wikiFactSets {
				return types.WikiFactSetRecord{}, ErrUnavailable
			}
			if err := validateFreezeWikiFactSet(actor, req); err != nil {
				return types.WikiFactSetRecord{}, err
			}
			scope := "wiki-fact-set/" + req.WikiRevisionId + "/" + actor
			return command(ctx, s, scope, req.IdempotencyKey, req,
				func(tx pgx.Tx) (types.WikiFactSetRecord, error) {
					m, err := module(ctx, tx, req.ModuleId, true)
					if err != nil {
						return types.WikiFactSetRecord{}, err
					}
					if err = enabled(m); err != nil {
						return types.WikiFactSetRecord{}, err
					}
					return s.commitWikiFactSet(ctx, tx, actor, req)
				})
		})
}

func (s *Store) commitWikiFactSet(ctx context.Context, tx pgx.Tx, actor string,
	req types.FreezeWikiFactSetReq) (types.WikiFactSetRecord, error) {
	target, err := s.wikiFactSetTarget(ctx, tx, req)
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	var factSetID, priorID string
	err = tx.QueryRow(ctx, `SELECT fact_set_id,revision_id FROM knowledge_wiki_fact_set_heads
 WHERE module_id=$1 AND page_id=$2 AND source_scope_revision=$3`,
		req.ModuleId, req.PageId, target.sourceScopeRevision).Scan(&factSetID, &priorID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.WikiFactSetRecord{}, err
	}
	if priorID != req.BaseFactSetRevisionId {
		return types.WikiFactSetRecord{}, conflict("FactSet base no longer current for this exact Source scope")
	}
	var priorRevision int64
	if priorID != "" {
		var previousSHA string
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT data,fact_set_jcs_sha256 FROM knowledge_wiki_fact_set_revisions
 WHERE revision_id=$1`, priorID).Scan(&raw, &previousSHA); err != nil {
			return types.WikiFactSetRecord{}, err
		}
		var previous wikiFactSetRevision
		h, hashErr := wikiQualityJCSHash(raw)
		if json.Unmarshal(raw, &previous) != nil || hashErr != nil || h != previousSHA ||
			validateFrozenWikiFactSet(previous, previousSHA) != nil ||
			previous.FactSetRevisionID != priorID || previous.FactSetID != factSetID ||
			previous.ModuleID != req.ModuleId || previous.PageID != req.PageId ||
			previous.SourceScopeRevision != target.sourceScopeRevision {
			return types.WikiFactSetRecord{}, ErrArtifactUnavailable
		}
		priorRevision, err = strconv.ParseInt(previous.FactSetRevision, 10, 64)
		if err != nil || priorRevision == int64(^uint64(0)>>1) {
			return types.WikiFactSetRecord{}, ErrArtifactUnavailable
		}
	} else {
		factSetID = id("fact_set")
	}
	var frozenAt time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&frozenAt); err != nil {
		return types.WikiFactSetRecord{}, err
	}
	r := wikiFactSetRevision{SchemaVersion: wikiFactSetRevisionSchema,
		FactSetID: factSetID, FactSetRevisionID: id("fact_set_revision"),
		FactSetRevision:       strconv.FormatInt(priorRevision+1, 10),
		BaseFactSetRevisionID: req.BaseFactSetRevisionId,
		ModuleID:              req.ModuleId, PageID: req.PageId,
		WikiRevisionID: req.WikiRevisionId, BaseWikiRevisionID: target.wiki.BaseRevisionId,
		WikiOriginKind: target.originKind, OriginCompileID: target.originCompileID,
		WikiContentSHA256:   target.wiki.ContentHash,
		SourceScopeRevision: target.sourceScopeRevision,
		SourceRevisions:     target.sources, Facts: target.facts, FactsComplete: true,
		DeclarationSource: wikiFactSetDeclarationSource, ActorID: actor,
		Reason: req.Reason, FrozenAt: frozenAt.UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(r)
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	setSHA, err := wikiQualityJCSHash(payload)
	if err != nil || validateFrozenWikiFactSet(r, setSHA) != nil {
		return types.WikiFactSetRecord{}, ErrArtifactUnavailable
	}
	if err := saveJSON(ctx, tx, `INSERT INTO knowledge_wiki_fact_set_revisions
 (revision_id,fact_set_id,module_id,page_id,source_scope_revision,wiki_revision_id,
 base_fact_set_revision_id,fact_set_revision,fact_set_jcs_sha256,facts_complete,data)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r,
		r.FactSetRevisionID, r.FactSetID, req.ModuleId, req.PageId,
		target.sourceScopeRevision, req.WikiRevisionId, req.BaseFactSetRevisionId,
		priorRevision+1, setSHA, true); err != nil {
		return types.WikiFactSetRecord{}, err
	}
	if priorID == "" {
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_wiki_fact_set_heads
 (module_id,page_id,source_scope_revision,fact_set_id,revision_id)
 VALUES($1,$2,$3,$4,$5)`, req.ModuleId, req.PageId,
			target.sourceScopeRevision, factSetID, r.FactSetRevisionID)
	} else {
		tag, updateErr := tx.Exec(ctx, `UPDATE knowledge_wiki_fact_set_heads SET revision_id=$5
 WHERE module_id=$1 AND page_id=$2 AND source_scope_revision=$3 AND revision_id=$4`,
			req.ModuleId, req.PageId, target.sourceScopeRevision, priorID, r.FactSetRevisionID)
		err = updateErr
		if err == nil && tag.RowsAffected() != 1 {
			err = conflict("FactSet head moved before CAS")
		}
	}
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	event, eventJSON, err := emitWithReceipt(ctx, tx, wikiFactSetEventType, req.ModuleId, r)
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	rawSHA := object.Hash(eventJSON)
	jcsSHA, err := wikiQualityJCSHash(eventJSON)
	if err != nil {
		return types.WikiFactSetRecord{}, ErrArtifactUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_wiki_fact_set_events
 (event_id,revision_id,event_json,event_raw_sha256,event_jcs_sha256)
 VALUES($1,$2,$3,$4,$5)`, event.EventID, r.FactSetRevisionID,
		string(eventJSON), rawSHA, jcsSHA)
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	return wikiFactSetRecord(r, setSHA, event.EventID, rawSHA, jcsSHA), nil
}

func validWikiFactSetTarget(moduleID, pageID string) bool {
	return citationIdentity(moduleID) && qualityPageID(pageID)
}

func (s *Store) GetWikiFactSetScope(ctx context.Context,
	req types.WikiFactSetScopePath) (types.WikiFactSetRecord, error) {
	return observe(ctx, s, "knowledge.wiki.fact-set.scope.get",
		"wiki-fact-set-scope:"+req.SourceScopeRevision, req,
		func(ctx context.Context) (types.WikiFactSetRecord, error) {
			if !s.wikiFactSets {
				return types.WikiFactSetRecord{}, ErrUnavailable
			}
			if !validWikiFactSetTarget(req.ModuleId, req.PageId) ||
				!validWikiFactSetScope(req.SourceScopeRevision) {
				return types.WikiFactSetRecord{}, invalid("canonical module/page/Source scope required")
			}
			var revisionID, factSetID string
			err := s.DB.QueryRow(ctx, `SELECT revision_id,fact_set_id FROM knowledge_wiki_fact_set_heads
 WHERE module_id=$1 AND page_id=$2 AND source_scope_revision=$3`,
				req.ModuleId, req.PageId, req.SourceScopeRevision).Scan(&revisionID, &factSetID)
			if errors.Is(err, pgx.ErrNoRows) {
				return types.WikiFactSetRecord{}, ErrNotFound
			}
			if err != nil {
				return types.WikiFactSetRecord{}, err
			}
			record, err := s.loadWikiFactSetRevision(ctx, req.ModuleId, req.PageId, revisionID)
			if err != nil {
				return record, err
			}
			if record.SourceScopeRevision != req.SourceScopeRevision ||
				record.FactSetId != factSetID {
				return types.WikiFactSetRecord{}, ErrArtifactUnavailable
			}
			return record, nil
		})
}

func (s *Store) GetWikiFactSetRevision(ctx context.Context,
	req types.WikiFactSetRevisionPath) (types.WikiFactSetRecord, error) {
	return observe(ctx, s, "knowledge.wiki.fact-set.revision.get",
		"wiki-fact-set-revision:"+req.FactSetRevisionId, req,
		func(ctx context.Context) (types.WikiFactSetRecord, error) {
			if !s.wikiFactSets {
				return types.WikiFactSetRecord{}, ErrUnavailable
			}
			if !validWikiFactSetTarget(req.ModuleId, req.PageId) ||
				!citationIdentity(req.FactSetRevisionId) {
				return types.WikiFactSetRecord{}, invalid("canonical module/page/FactSet revision required")
			}
			return s.loadWikiFactSetRevision(ctx, req.ModuleId, req.PageId,
				req.FactSetRevisionId)
		})
}

func (s *Store) loadWikiFactSetRevision(ctx context.Context, moduleID, pageID, revisionID string) (
	types.WikiFactSetRecord, error) {
	var raw []byte
	var setSHA, eventID, rowSetID, rowScope, rowWiki string
	var rowRevision int64
	err := s.DB.QueryRow(ctx, `SELECT r.data,r.fact_set_jcs_sha256,e.event_id,
 r.fact_set_id,r.source_scope_revision,r.wiki_revision_id,r.fact_set_revision
 FROM knowledge_wiki_fact_set_revisions r
 JOIN knowledge_wiki_fact_set_events e ON e.revision_id=r.revision_id
 WHERE r.revision_id=$1 AND r.module_id=$2 AND r.page_id=$3`,
		revisionID, moduleID, pageID).Scan(&raw, &setSHA, &eventID,
		&rowSetID, &rowScope, &rowWiki, &rowRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.WikiFactSetRecord{}, ErrNotFound
	}
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	var r wikiFactSetRevision
	h, hashErr := wikiQualityJCSHash(raw)
	if json.Unmarshal(raw, &r) != nil || hashErr != nil || h != setSHA ||
		validateFrozenWikiFactSet(r, setSHA) != nil ||
		r.FactSetRevisionID != revisionID || r.ModuleID != moduleID || r.PageID != pageID ||
		r.FactSetID != rowSetID || r.SourceScopeRevision != rowScope ||
		r.WikiRevisionID != rowWiki || r.FactSetRevision != strconv.FormatInt(rowRevision, 10) {
		return types.WikiFactSetRecord{}, ErrArtifactUnavailable
	}
	event, err := s.GetWikiFactSetEvent(ctx, eventID)
	if err != nil {
		return types.WikiFactSetRecord{}, err
	}
	return wikiFactSetRecord(r, setSHA, eventID,
		event.EventRawSha256, event.EventJcsSha256), nil
}
