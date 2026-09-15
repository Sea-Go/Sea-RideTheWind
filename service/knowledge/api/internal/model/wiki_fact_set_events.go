package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// Private Event lookup reads the original emitted bytes rather than rebuilding
// an EventSpec from outbox JSONB or the current Wiki editing/Release pointers.
func (s *Store) GetWikiFactSetEvent(ctx context.Context, eventID string) (
	types.WikiFactSetEventReceipt, error) {
	return observe(ctx, s, "knowledge.wiki.fact-set.event.read",
		"wiki-fact-set-event:"+eventID, types.WikiFactSetEventPath{EventId: eventID},
		func(ctx context.Context) (types.WikiFactSetEventReceipt, error) {
			var result types.WikiFactSetEventReceipt
			if !s.wikiFactSets {
				return result, ErrUnavailable
			}
			if !citationIdentity(eventID) {
				return result, invalid("Wiki FactSet Event identity required")
			}
			var revisionID, setSHA string
			var data []byte
			err := s.DB.QueryRow(ctx, `SELECT e.event_id,e.event_json,e.event_raw_sha256,
 e.event_jcs_sha256,e.revision_id,r.fact_set_jcs_sha256,r.data
 FROM knowledge_wiki_fact_set_events e
 JOIN knowledge_outbox o ON o.event_id=e.event_id
 JOIN knowledge_wiki_fact_set_revisions r ON r.revision_id=e.revision_id
 WHERE e.event_id=$1 AND o.event_type=$2 AND o.payload=e.event_json::jsonb
 AND r.data=(o.payload->'payload')`, eventID, wikiFactSetEventType).
				Scan(&result.EventId, &result.EventJson, &result.EventRawSha256,
					&result.EventJcsSha256, &revisionID, &setSHA, &data)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrNotFound
			}
			if err != nil {
				return result, err
			}
			result.FactSetJcsSha256 = setSHA
			raw := []byte(result.EventJson)
			h, hashErr := wikiQualityJCSHash(raw)
			if object.Hash(raw) != result.EventRawSha256 || hashErr != nil ||
				h != result.EventJcsSha256 || !citationHash(setSHA) {
				return types.WikiFactSetEventReceipt{}, ErrArtifactUnavailable
			}
			var event Event
			if json.Unmarshal(raw, &event) != nil || event.EventID != eventID ||
				event.EventType != wikiFactSetEventType || event.SchemaVersion != 1 ||
				event.Producer != "ridethewind.knowledge" || event.AggregateVersion < 1 ||
				event.OperationID == "" {
				return types.WikiFactSetEventReceipt{}, ErrArtifactUnavailable
			}
			var r wikiFactSetRevision
			payloadHash, payloadErr := wikiQualityJCSHash(event.Payload)
			dataHash, dataErr := wikiQualityJCSHash(data)
			if json.Unmarshal(event.Payload, &r) != nil || payloadErr != nil || dataErr != nil ||
				payloadHash != setSHA || dataHash != setSHA ||
				r.FactSetRevisionID != revisionID || r.ModuleID != event.AggregateID ||
				validateFrozenWikiFactSet(r, setSHA) != nil ||
				s.verifyFrozenWikiFactSetBytes(ctx, r) != nil {
				return types.WikiFactSetEventReceipt{}, ErrArtifactUnavailable
			}
			return result, nil
		})
}

// Historical quality evidence stays readable after a later withdrawal. This
// verifies the frozen Source and Wiki bytes and exact first quote span; it
// does not requalify a new FactSet against the current withdrawal state.
func (s *Store) verifyFrozenWikiFactSetBytes(ctx context.Context, r wikiFactSetRevision) error {
	wiki, err := revision(ctx, s.DB, r.WikiRevisionID)
	if err != nil || wiki.Kind != "wiki" || wiki.ModuleId != r.ModuleID ||
		wiki.EntityId != r.PageID || wiki.BaseRevisionId != r.BaseWikiRevisionID ||
		wiki.ContentHash != r.WikiContentSHA256 ||
		wiki.ObjectKey != object.Key(r.WikiContentSHA256) {
		return ErrArtifactUnavailable
	}
	if r.WikiOriginKind == "ai_accepted" {
		if !citationIdentity(r.OriginCompileID) ||
			wiki.CreatedBy != "btw.compile/"+r.OriginCompileID {
			return ErrArtifactUnavailable
		}
	} else if r.WikiOriginKind != "manual_revision" || r.OriginCompileID != "" ||
		wiki.CreatedBy == "" || strings.HasPrefix(wiki.CreatedBy, "btw.compile/") {
		return ErrArtifactUnavailable
	}
	wikiBytes, err := s.Objects.Get(ctx, wiki.ObjectKey, wiki.ContentHash)
	if err != nil || object.Hash(wikiBytes) != wiki.ContentHash || !utf8.Valid(wikiBytes) {
		return ErrArtifactUnavailable
	}
	originals := make(map[string][]byte, len(r.SourceRevisions))
	var totalBytes int64
	for _, claim := range r.SourceRevisions {
		source, err := revision(ctx, s.DB, claim.RevisionId)
		if err != nil || source.Kind != "source" || source.ModuleId != r.ModuleID ||
			source.ContentHash != claim.ContentSha256 ||
			source.ObjectKey != object.Key(claim.ContentSha256) {
			return ErrArtifactUnavailable
		}
		original, err := s.Objects.Get(ctx, source.ObjectKey, source.ContentHash)
		if err != nil || object.Hash(original) != source.ContentHash || !utf8.Valid(original) {
			return ErrArtifactUnavailable
		}
		totalBytes += int64(len(original))
		if totalBytes > 64<<20 {
			return ErrArtifactUnavailable
		}
		originals[claim.RevisionId] = original
	}
	paragraphs := map[string][2]int{}
	for _, fact := range r.Facts {
		original, ok := originals[fact.SourceRevisionId]
		if !ok {
			return ErrArtifactUnavailable
		}
		key := fact.SourceRevisionId + "\x00" + fact.Locator
		span, cached := paragraphs[key]
		if !cached {
			_, start, end, valid := citationParagraph(string(original), fact.Locator)
			if !valid || start < 0 || end <= start || end > len(original) {
				return ErrArtifactUnavailable
			}
			span = [2]int{start, end}
			paragraphs[key] = span
		}
		quoteOffset := bytes.Index(original[span[0]:span[1]], []byte(fact.SourceQuote))
		start, startErr := strconv.ParseInt(fact.SourceByteStart, 10, 64)
		end, endErr := strconv.ParseInt(fact.SourceByteEnd, 10, 64)
		if quoteOffset < 0 || startErr != nil || endErr != nil ||
			start != int64(span[0]+quoteOffset) || end != start+int64(len(fact.SourceQuote)) ||
			end > int64(len(original)) ||
			!bytes.Equal(original[start:end], []byte(fact.SourceQuote)) {
			return ErrArtifactUnavailable
		}
	}
	return nil
}
