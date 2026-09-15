package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// GetWikiQualityEvent returns the original emitted EventSpec bytes and both
// independent hashes. JSONB outbox reads cannot reconstruct those bytes.
func (s *Store) GetWikiQualityEvent(ctx context.Context, eventID string) (types.WikiQualityEventReceipt, error) {
	return observe(ctx, s, "knowledge.wiki.quality.event.read", "wiki-quality-event:"+eventID,
		types.WikiQualityEventPath{EventId: eventID},
		func(ctx context.Context) (types.WikiQualityEventReceipt, error) {
			var result types.WikiQualityEventReceipt
			if !s.wikiQualityJudgments {
				return result, ErrUnavailable
			}
			if !citationIdentity(eventID) {
				return result, invalid("Wiki quality event identity required")
			}
			return s.getWikiQualityEventInQuery(ctx, s.DB, eventID)
		})
}

// The snapshot reader passes its read-only transaction here. A private GET
// passes the pool and keeps the existing public response unchanged.
func (s *Store) getWikiQualityEventInQuery(ctx context.Context, q queryer,
	eventID string) (types.WikiQualityEventReceipt, error) {
	if !citationIdentity(eventID) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	var result types.WikiQualityEventReceipt
	var revisionID string
	err := q.QueryRow(ctx, `SELECT e.event_id,e.event_json,e.event_raw_sha256,
 e.event_jcs_sha256,e.revision_id FROM knowledge_wiki_quality_events e
 JOIN knowledge_outbox o ON o.event_id=e.event_id
 JOIN knowledge_wiki_quality_revisions r ON r.revision_id=e.revision_id
 WHERE e.event_id=$1 AND o.event_type=$2 AND o.payload=e.event_json::jsonb
 AND r.data=(o.payload->'payload')`, eventID, wikiQualityEventType).
		Scan(&result.EventId, &result.EventJson, &result.EventRawSha256,
			&result.EventJcsSha256, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	raw := []byte(result.EventJson)
	jcsSHA, hashErr := wikiQualityJCSHash(raw)
	if object.Hash(raw) != result.EventRawSha256 || hashErr != nil ||
		jcsSHA != result.EventJcsSha256 {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	var event Event
	if json.Unmarshal(raw, &event) != nil || event.EventID != eventID ||
		event.EventType != wikiQualityEventType || event.SchemaVersion != 1 ||
		event.Producer != "ridethewind.knowledge" || event.AggregateVersion < 1 ||
		event.OperationID == "" {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	var judgment wikiQualityRevision
	if json.Unmarshal(event.Payload, &judgment) != nil ||
		judgment.SchemaVersion != wikiQualityRevisionSchema ||
		judgment.JudgmentSource != wikiQualityJudgmentSource ||
		judgment.JudgeRevisionID != revisionID ||
		judgment.ModuleID != event.AggregateID || !validWikiFactID(judgment.FactID) ||
		judgment.FactID != wikiQualityFactID(judgment.SourceRevisionID,
			judgment.Locator, judgment.SourceQuoteSHA256) ||
		!citationHash(judgment.SourceContentSHA256) ||
		!citationHash(judgment.WikiContentSHA256) ||
		object.Hash([]byte(judgment.SourceQuote)) != judgment.SourceQuoteSHA256 ||
		!validQualityLocator(judgment.Locator) ||
		!validLogicalSessionID(judgment.ActorID) ||
		(judgment.WikiOriginKind != "ai_accepted" && judgment.WikiOriginKind != "manual_revision") ||
		(judgment.WikiOriginKind == "manual_revision" && judgment.OriginCompileID != "") ||
		(judgment.SourceWithdrawn || judgment.WikiWithdrawn) &&
			(judgment.Assessment != "undetermined" || judgment.Grade != nil) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	if judgment.WikiClaimText == "" && judgment.WikiClaimSHA256 != "" ||
		judgment.WikiClaimText != "" && object.Hash([]byte(judgment.WikiClaimText)) != judgment.WikiClaimSHA256 {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	counter, countErr := strconv.ParseInt(judgment.JudgeRevision, 10, 64)
	start, startErr := strconv.ParseInt(judgment.SourceByteStart, 10, 64)
	end, endErr := strconv.ParseInt(judgment.SourceByteEnd, 10, 64)
	if countErr != nil || counter < 1 || judgment.JudgeRevision != strconv.FormatInt(counter, 10) ||
		startErr != nil || endErr != nil || start < 0 || end <= start ||
		judgment.SourceByteStart != strconv.FormatInt(start, 10) ||
		judgment.SourceByteEnd != strconv.FormatInt(end, 10) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	// The Event is a historical human decision. Withdrawal may have
	// happened later, so compare its frozen hashes/spans against the
	// immutable objects without rewriting old eligibility snapshots.
	source, sourceErr := revision(ctx, q, judgment.SourceRevisionID)
	if sourceErr != nil || source.Kind != "source" || source.ModuleId != judgment.ModuleID ||
		source.ContentHash != judgment.SourceContentSHA256 ||
		source.ObjectKey != object.Key(source.ContentHash) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	sourceBytes, sourceErr := s.Objects.Get(ctx, source.ObjectKey, source.ContentHash)
	if sourceErr != nil || object.Hash(sourceBytes) != source.ContentHash ||
		end > int64(len(sourceBytes)) ||
		!bytes.Equal(sourceBytes[start:end], []byte(judgment.SourceQuote)) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	_, paragraphStart, paragraphEnd, locatorOK := citationParagraph(string(sourceBytes), judgment.Locator)
	if !locatorOK || start < int64(paragraphStart) || end > int64(paragraphEnd) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	wiki, wikiErr := revision(ctx, q, judgment.WikiRevisionID)
	if wikiErr != nil || wiki.Kind != "wiki" || wiki.ModuleId != judgment.ModuleID ||
		wiki.EntityId != judgment.PageID || wiki.ContentHash != judgment.WikiContentSHA256 ||
		wiki.ObjectKey != object.Key(wiki.ContentHash) ||
		wiki.BaseRevisionId != judgment.BaseWikiRevisionID {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	wikiBytes, wikiErr := s.Objects.Get(ctx, wiki.ObjectKey, wiki.ContentHash)
	if wikiErr != nil || object.Hash(wikiBytes) != wiki.ContentHash ||
		judgment.WikiClaimText != "" && !strings.Contains(string(wikiBytes), judgment.WikiClaimText) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	cited := false
	for _, ref := range wiki.SourceRefs {
		if ref.RevisionId == judgment.SourceRevisionID && ref.Locator == judgment.Locator {
			cited = true
		}
	}
	if cited != judgment.CitationPresent ||
		judgment.WikiOriginKind == "ai_accepted" &&
			wiki.CreatedBy != "btw.compile/"+judgment.OriginCompileID ||
		judgment.WikiOriginKind == "manual_revision" &&
			(wiki.CreatedBy == "" || strings.HasPrefix(wiki.CreatedBy, "btw.compile/")) {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	grade := ""
	if judgment.Grade != nil {
		grade = strconv.Itoa(*judgment.Grade)
	}
	if judgment.RubricVersion != wikiQualityRubric {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	if _, err := wikiQualityGrade(judgment.Assessment, grade); err != nil {
		return types.WikiQualityEventReceipt{}, ErrArtifactUnavailable
	}
	return result, nil
}
