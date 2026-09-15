package model

import (
	"context"
	"encoding/json"
	"errors"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// WikiQualitySnapshotTarget pins an immutable FactSet and its own historical
// Wiki/Source scope. The current scope head and editing/Release pointers are
// intentionally absent: they may target a different Wiki revision.
type WikiQualitySnapshotTarget struct {
	ModuleID            string
	PageID              string
	FactSetRevisionID   string
	WikiRevisionID      string
	SourceScopeRevision string
}

// WikiQualitySourceStatus records one frozen Source's current withdrawal bit.
type WikiQualitySourceStatus struct {
	RevisionID    string
	ContentSHA256 string
	Withdrawn     bool
}

// WikiQualityRequiredFact preserves a missing current head as a nil Judgment.
type WikiQualityRequiredFact struct {
	Fact                  types.FactSetFact
	Judgment              *types.WikiFactJudgmentRecord
	Event                 *types.WikiQualityEventReceipt
	SourceWithdrawnAtRead bool
}

// WikiQualitySourceSnapshot is a current RTW read snapshot, not a DC cutoff
// receipt, a realtime UserCenter identity proof, or a human quality verdict.
type WikiQualitySourceSnapshot struct {
	FactSet                       types.WikiFactSetRecord
	FactSetEvent                  types.WikiFactSetEventReceipt
	Sources                       []WikiQualitySourceStatus
	RequiredFacts                 []WikiQualityRequiredFact
	WikiWithdrawn                 bool
	WikiAndSourcesAvailableAtRead bool
	AllRequiredHaveHead           bool
}

// ReadWikiQualitySourceSnapshot never reconstructs a historical DC cutoff.
func (s *Store) ReadWikiQualitySourceSnapshot(ctx context.Context,
	target WikiQualitySnapshotTarget) (WikiQualitySourceSnapshot, error) {
	return observe(ctx, s, "knowledge.wiki.quality.source.snapshot.read",
		"wiki-quality-source-snapshot:"+target.FactSetRevisionID, target,
		func(ctx context.Context) (WikiQualitySourceSnapshot, error) {
			var result WikiQualitySourceSnapshot
			if !s.wikiFactSets || !s.wikiQualityJudgments {
				return result, ErrUnavailable
			}
			if !validWikiFactSetTarget(target.ModuleID, target.PageID) ||
				!citationIdentity(target.FactSetRevisionID) ||
				!citationIdentity(target.WikiRevisionID) ||
				!validWikiFactSetScope(target.SourceScopeRevision) {
				return result, invalid("pinned FactSet/Wiki/Source scope identity required")
			}
			tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{
				IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return result, err
			}
			defer tx.Rollback(context.Background())
			result, err = s.readWikiQualitySourceSnapshotInTx(ctx, tx, target)
			if err != nil {
				return WikiQualitySourceSnapshot{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return WikiQualitySourceSnapshot{}, err
			}
			return result, nil
		})
}

func (s *Store) readWikiQualitySourceSnapshotInTx(ctx context.Context, tx pgx.Tx,
	target WikiQualitySnapshotTarget) (WikiQualitySourceSnapshot, error) {
	var result WikiQualitySourceSnapshot
	set, err := s.loadWikiFactSetRevisionInQuery(ctx, tx,
		target.ModuleID, target.PageID, target.FactSetRevisionID)
	if err != nil {
		return result, err
	}
	if set.WikiRevisionId != target.WikiRevisionID ||
		set.SourceScopeRevision != target.SourceScopeRevision {
		return result, ErrNotFound
	}
	result.FactSet = set
	result.FactSetEvent, err = s.getWikiFactSetEventInQuery(ctx, tx, set.EventId)
	if err != nil {
		return WikiQualitySourceSnapshot{}, err
	}
	wiki, err := revision(ctx, tx, target.WikiRevisionID)
	if err != nil || wiki.Kind != "wiki" || wiki.ModuleId != target.ModuleID ||
		wiki.EntityId != target.PageID || wiki.ContentHash != set.WikiContentSha256 {
		return WikiQualitySourceSnapshot{}, ErrArtifactUnavailable
	}
	result.WikiWithdrawn = wiki.Withdrawn
	result.WikiAndSourcesAvailableAtRead = !wiki.Withdrawn
	result.AllRequiredHaveHead = true
	result.Sources = make([]WikiQualitySourceStatus, 0, len(set.SourceRevisions))
	withdrawn := make(map[string]bool, len(set.SourceRevisions))
	for _, claim := range set.SourceRevisions {
		source, sourceErr := revision(ctx, tx, claim.RevisionId)
		if sourceErr != nil || source.Kind != "source" || source.ModuleId != target.ModuleID ||
			source.ContentHash != claim.ContentSha256 {
			return WikiQualitySourceSnapshot{}, ErrArtifactUnavailable
		}
		result.Sources = append(result.Sources, WikiQualitySourceStatus{
			RevisionID: claim.RevisionId, ContentSHA256: claim.ContentSha256,
			Withdrawn: source.Withdrawn})
		withdrawn[claim.RevisionId] = source.Withdrawn
		result.WikiAndSourcesAvailableAtRead =
			result.WikiAndSourcesAvailableAtRead && !source.Withdrawn
	}
	result.RequiredFacts = make([]WikiQualityRequiredFact, 0, len(set.Facts))
	for _, fact := range set.Facts {
		if !fact.Required {
			continue
		}
		isWithdrawn, exists := withdrawn[fact.SourceRevisionId]
		if !exists {
			return WikiQualitySourceSnapshot{}, ErrArtifactUnavailable
		}
		entry := WikiQualityRequiredFact{Fact: fact, SourceWithdrawnAtRead: isWithdrawn}
		var headID string
		err = tx.QueryRow(ctx, `SELECT revision_id FROM knowledge_wiki_quality_heads
 WHERE wiki_revision_id=$1 AND fact_id=$2`, target.WikiRevisionID, fact.FactId).Scan(&headID)
		if errors.Is(err, pgx.ErrNoRows) {
			result.AllRequiredHaveHead = false
			result.RequiredFacts = append(result.RequiredFacts, entry)
			continue
		}
		if err != nil {
			return WikiQualitySourceSnapshot{}, err
		}
		var eventID string
		err = tx.QueryRow(ctx, `SELECT event_id FROM knowledge_wiki_quality_events
 WHERE revision_id=$1`, headID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return WikiQualitySourceSnapshot{}, ErrArtifactUnavailable
		}
		if err != nil {
			return WikiQualitySourceSnapshot{}, err
		}
		event, eventErr := s.getWikiQualityEventInQuery(ctx, tx, eventID)
		if eventErr != nil {
			return WikiQualitySourceSnapshot{}, eventErr
		}
		var envelope Event
		var judged wikiQualityRevision
		if json.Unmarshal([]byte(event.EventJson), &envelope) != nil ||
			json.Unmarshal(envelope.Payload, &judged) != nil ||
			judged.JudgeRevisionID != headID || judged.FactID != fact.FactId ||
			judged.WikiRevisionID != target.WikiRevisionID ||
			judged.ModuleID != target.ModuleID || judged.PageID != target.PageID ||
			judged.WikiContentSHA256 != set.WikiContentSha256 ||
			judged.SourceRevisionID != fact.SourceRevisionId ||
			judged.SourceContentSHA256 != fact.SourceContentSha256 ||
			judged.Locator != fact.Locator ||
			judged.SourceByteStart != fact.SourceByteStart ||
			judged.SourceByteEnd != fact.SourceByteEnd ||
			judged.SourceQuote != fact.SourceQuote ||
			judged.SourceQuoteSHA256 != fact.SourceQuoteSha256 {
			return WikiQualitySourceSnapshot{}, ErrArtifactUnavailable
		}
		judgment := wikiQualityRecord(judged, event.EventId,
			event.EventRawSha256, event.EventJcsSha256)
		entry.Judgment, entry.Event = &judgment, &event
		result.RequiredFacts = append(result.RequiredFacts, entry)
	}
	return result, nil
}
