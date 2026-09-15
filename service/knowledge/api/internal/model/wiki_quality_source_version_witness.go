package model

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	wikiQualitySourceVersionSchema = "rtw.wiki-quality-source-version-candidate.v1"
	wikiQualitySourceVersionMax    = 4096
	wikiQualitySourceVersionPage   = 128
	wikiQualitySourceVersionBytes  = 16 << 20
)

// WikiQualitySourceVersionEvent is a canonical JSONB Event at one frozen RTW
// module version. Non-FactSet/Quality Event raw bytes were not stored, so its
// JCS digest is never presented as an original raw-byte SHA.
type WikiQualitySourceVersionEvent struct {
	AggregateVersion int64
	EventID          string
	EventType        string
	EventJCSSHA256   string
	JCSSource        string
	DeliveredAt      string
	TargetKind       string
	TargetID         string
}

type WikiQualitySourceVersionPage struct {
	FromVersion int64
	ToVersion   int64
	Events      []WikiQualitySourceVersionEvent
}

type WikiQualitySourceVersionCatalog struct {
	FactSetRevisionID   string
	WikiRevisionID      string
	SourceScopeRevision string
	EventID             string
	EventRawSHA256      string
	EventJCSSHA256      string
	FactSetJCSSHA256    string
}

type WikiQualitySourceVersionRequiredHead struct {
	FactID                string
	SourceRevisionID      string
	SourceContentSHA256   string
	Present               bool
	JudgeRevisionID       string
	EventID               string
	EventRawSHA256        string
	EventJCSSHA256        string
	SourceWithdrawnAtRead bool
}

// Candidate only proves one RTW repeatable-read source state and a complete,
// delivered 1..V Outbox version list. DC cutoff qualification is a separate
// verifier that must map every EventID to the acknowledged DC prefix.
type WikiQualitySourceVersionCandidate struct {
	SchemaVersion                 string
	SourceVersion                 int64
	EventSequenceAtRead           int64
	Catalog                       WikiQualitySourceVersionCatalog
	Sources                       []WikiQualitySourceStatus
	RequiredHeads                 []WikiQualitySourceVersionRequiredHead
	ModuleLifecycleAtRead         string
	WikiWithdrawnAtRead           bool
	WikiAndSourcesAvailableAtRead bool
	AllRequiredHaveHead           bool
	EventCount                    int
	Pages                         []WikiQualitySourceVersionPage
}

func (s *Store) ReadWikiQualitySourceVersionCandidate(ctx context.Context,
	target WikiQualitySnapshotTarget, sourceVersion int64) (WikiQualitySourceVersionCandidate, error) {
	return observe(ctx, s, "knowledge.wiki.quality.source.version.read",
		"wiki-quality-source-version:"+target.FactSetRevisionID,
		struct {
			WikiQualitySnapshotTarget
			SourceVersion int64
		}{target, sourceVersion},
		func(ctx context.Context) (WikiQualitySourceVersionCandidate, error) {
			if !s.wikiFactSets || !s.wikiQualityJudgments {
				return WikiQualitySourceVersionCandidate{}, ErrUnavailable
			}
			if !validWikiFactSetTarget(target.ModuleID, target.PageID) ||
				!citationIdentity(target.FactSetRevisionID) ||
				!citationIdentity(target.WikiRevisionID) ||
				!validWikiFactSetScope(target.SourceScopeRevision) || sourceVersion < 1 {
				return WikiQualitySourceVersionCandidate{}, invalid("fixed Wiki/FactSet/Scope and positive source version required")
			}
			if sourceVersion > wikiQualitySourceVersionMax {
				return WikiQualitySourceVersionCandidate{}, ErrUnavailable
			}
			tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{
				IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return WikiQualitySourceVersionCandidate{}, err
			}
			defer tx.Rollback(context.Background())
			result, err := s.readWikiQualitySourceVersionCandidateInTx(ctx, tx, target, sourceVersion)
			if err != nil {
				return WikiQualitySourceVersionCandidate{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return WikiQualitySourceVersionCandidate{}, err
			}
			return result, nil
		})
}

func (s *Store) readWikiQualitySourceVersionCandidateInTx(ctx context.Context,
	tx pgx.Tx, target WikiQualitySnapshotTarget, version int64) (WikiQualitySourceVersionCandidate, error) {
	var result WikiQualitySourceVersionCandidate
	snapshot, err := s.readWikiQualitySourceSnapshotInTx(ctx, tx, target)
	if err != nil {
		return result, err
	}
	result.SchemaVersion = wikiQualitySourceVersionSchema
	result.SourceVersion = version
	result.Catalog = WikiQualitySourceVersionCatalog{
		FactSetRevisionID:   snapshot.FactSet.FactSetRevisionId,
		WikiRevisionID:      snapshot.FactSet.WikiRevisionId,
		SourceScopeRevision: snapshot.FactSet.SourceScopeRevision,
		EventID:             snapshot.FactSet.EventId,
		EventRawSHA256:      snapshot.FactSetEvent.EventRawSha256,
		EventJCSSHA256:      snapshot.FactSetEvent.EventJcsSha256,
		FactSetJCSSHA256:    snapshot.FactSetEvent.FactSetJcsSha256}
	result.Sources = snapshot.Sources
	module, moduleErr := module(ctx, tx, target.ModuleID, false)
	if moduleErr != nil || module.Id != target.ModuleID ||
		(module.Lifecycle != "ENABLED" && module.Lifecycle != "WITHDRAWN") {
		return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
	}
	result.ModuleLifecycleAtRead = module.Lifecycle
	result.WikiWithdrawnAtRead = snapshot.WikiWithdrawn
	result.WikiAndSourcesAvailableAtRead = snapshot.WikiAndSourcesAvailableAtRead
	result.AllRequiredHaveHead = snapshot.AllRequiredHaveHead
	result.RequiredHeads = make([]WikiQualitySourceVersionRequiredHead, 0, len(snapshot.RequiredFacts))
	for _, fact := range snapshot.RequiredFacts {
		head := WikiQualitySourceVersionRequiredHead{
			FactID: fact.Fact.FactId, SourceRevisionID: fact.Fact.SourceRevisionId,
			SourceContentSHA256:   fact.Fact.SourceContentSha256,
			SourceWithdrawnAtRead: fact.SourceWithdrawnAtRead}
		if fact.Judgment != nil && fact.Event != nil {
			head.Present = true
			head.JudgeRevisionID = fact.Judgment.JudgeRevisionId
			head.EventID = fact.Event.EventId
			head.EventRawSHA256 = fact.Event.EventRawSha256
			head.EventJCSSHA256 = fact.Event.EventJcsSha256
		}
		result.RequiredHeads = append(result.RequiredHeads, head)
	}
	if err := tx.QueryRow(ctx, `SELECT event_sequence FROM knowledge_modules WHERE id=$1`,
		target.ModuleID).Scan(&result.EventSequenceAtRead); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WikiQualitySourceVersionCandidate{}, ErrNotFound
		}
		return WikiQualitySourceVersionCandidate{}, err
	}
	if result.EventSequenceAtRead != version {
		return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
	}
	var count int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM knowledge_outbox WHERE aggregate_id=$1`,
		target.ModuleID).Scan(&count); err != nil {
		return WikiQualitySourceVersionCandidate{}, err
	}
	if count != version || count > wikiQualitySourceVersionMax {
		return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT event_id,event_type,aggregate_id,payload,delivered_at
 FROM knowledge_outbox WHERE aggregate_id=$1 ORDER BY event_id`, target.ModuleID)
	if err != nil {
		return WikiQualitySourceVersionCandidate{}, err
	}
	defer rows.Close()
	ordered := make([]WikiQualitySourceVersionEvent, count)
	seen := make([]bool, count)
	var totalBytes int
	for rows.Next() {
		var eventID, eventType, aggregateID string
		var raw []byte
		var delivery pgtype.Timestamptz
		if err = rows.Scan(&eventID, &eventType, &aggregateID, &raw, &delivery); err != nil {
			return WikiQualitySourceVersionCandidate{}, err
		}
		totalBytes += len(raw)
		if totalBytes > wikiQualitySourceVersionBytes || !delivery.Valid {
			return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
		}
		entry, entryErr := sourceVersionEventFromJSONB(raw, eventID, eventType,
			aggregateID, target.ModuleID, delivery.Time)
		if entryErr != nil || entry.AggregateVersion > version ||
			seen[entry.AggregateVersion-1] {
			return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
		}
		seen[entry.AggregateVersion-1] = true
		ordered[entry.AggregateVersion-1] = entry
	}
	if err := rows.Err(); err != nil {
		return WikiQualitySourceVersionCandidate{}, err
	}
	// pgx cannot issue another query on this transaction while rows remain
	// open. Original FactSet/Quality sidecars are checked after the full list.
	rows.Close()
	for i := range ordered {
		entry := &ordered[i]
		if !seen[i] {
			return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
		}
		if entry.EventType == wikiFactSetEventType {
			original, originalErr := s.getWikiFactSetEventInQuery(ctx, tx, entry.EventID)
			if originalErr != nil || original.EventJcsSha256 != entry.EventJCSSHA256 {
				return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
			}
			entry.JCSSource = "original_fact_set_event_sidecar"
		} else if entry.EventType == wikiQualityEventType {
			original, originalErr := s.getWikiQualityEventInQuery(ctx, tx, entry.EventID)
			if originalErr != nil || original.EventJcsSha256 != entry.EventJCSSHA256 {
				return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
			}
			entry.JCSSource = "original_quality_event_sidecar"
		}
	}
	byID := make(map[string]WikiQualitySourceVersionEvent, len(ordered))
	for _, event := range ordered {
		byID[event.EventID] = event
	}
	catalogEvent, ok := byID[result.Catalog.EventID]
	if !ok || catalogEvent.EventType != wikiFactSetEventType ||
		catalogEvent.EventJCSSHA256 != result.Catalog.EventJCSSHA256 {
		return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
	}
	for _, head := range result.RequiredHeads {
		if !head.Present {
			continue
		}
		judgmentEvent, found := byID[head.EventID]
		if !found || judgmentEvent.EventType != wikiQualityEventType ||
			judgmentEvent.EventJCSSHA256 != head.EventJCSSHA256 {
			return WikiQualitySourceVersionCandidate{}, ErrArtifactUnavailable
		}
	}
	if err := reconcileSourceVersionWithdrawals(ctx, tx, target.ModuleID,
		result, ordered); err != nil {
		return WikiQualitySourceVersionCandidate{}, err
	}
	result.EventCount = len(ordered)
	result.Pages = make([]WikiQualitySourceVersionPage, 0,
		(len(ordered)+wikiQualitySourceVersionPage-1)/wikiQualitySourceVersionPage)
	for start := 0; start < len(ordered); start += wikiQualitySourceVersionPage {
		end := start + wikiQualitySourceVersionPage
		if end > len(ordered) {
			end = len(ordered)
		}
		result.Pages = append(result.Pages, WikiQualitySourceVersionPage{
			FromVersion: int64(start + 1), ToVersion: int64(end), Events: ordered[start:end]})
	}
	return result, nil
}

// Every read-time withdrawal bit must have a typed immutable source Event in
// this exact module's 1..V list. Repeated withdrawals are allowed; direct
// edits or a foreign-module target cannot be promoted to source evidence.
func reconcileSourceVersionWithdrawals(ctx context.Context, tx pgx.Tx,
	moduleID string, result WikiQualitySourceVersionCandidate,
	events []WikiQualitySourceVersionEvent) error {
	withdrawnRevision := make(map[string]bool)
	moduleWithdrawal := false
	for _, event := range events {
		if event.EventType != "knowledge.content.withdrawn.v1" {
			continue
		}
		if event.TargetKind == "module" {
			if event.TargetID != moduleID {
				return ErrArtifactUnavailable
			}
			moduleWithdrawal = true
			continue
		}
		if event.TargetKind != "revision" || event.TargetID == "" {
			return ErrArtifactUnavailable
		}
		if !withdrawnRevision[event.TargetID] {
			revision, err := revision(ctx, tx, event.TargetID)
			if err != nil || revision.ModuleId != moduleID || !revision.Withdrawn {
				return ErrArtifactUnavailable
			}
		}
		withdrawnRevision[event.TargetID] = true
	}
	if moduleWithdrawal != (result.ModuleLifecycleAtRead == "WITHDRAWN") ||
		withdrawnRevision[result.Catalog.WikiRevisionID] != result.WikiWithdrawnAtRead {
		return ErrArtifactUnavailable
	}
	for _, source := range result.Sources {
		if withdrawnRevision[source.RevisionID] != source.Withdrawn {
			return ErrArtifactUnavailable
		}
	}
	return nil
}

func sourceVersionEventFromJSONB(raw []byte, eventID, eventType, aggregateID,
	moduleID string, deliveredAt time.Time) (WikiQualitySourceVersionEvent, error) {
	var result WikiQualitySourceVersionEvent
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 9 {
		return result, ErrArtifactUnavailable
	}
	for _, key := range []string{"aggregate_version", "operation_id", "event_id", "event_type",
		"schema_version", "producer", "aggregate_id", "occurred_at", "payload"} {
		if _, ok := fields[key]; !ok {
			return result, ErrArtifactUnavailable
		}
	}
	var event Event
	if json.Unmarshal(raw, &event) != nil || event.EventID != eventID ||
		event.EventType != eventType || event.AggregateID != aggregateID ||
		aggregateID != moduleID || event.Producer != "ridethewind.knowledge" ||
		event.SchemaVersion != 1 || event.AggregateVersion < 1 ||
		!citationIdentity(eventID) || event.OperationID == "" || event.EventType == "" ||
		!json.Valid(event.Payload) || deliveredAt.IsZero() {
		return result, ErrArtifactUnavailable
	}
	var payloadFields map[string]json.RawMessage
	if json.Unmarshal(event.Payload, &payloadFields) != nil || payloadFields == nil {
		return result, ErrArtifactUnavailable
	}
	if _, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err != nil {
		return result, ErrArtifactUnavailable
	}
	h, err := wikiQualityJCSHash(raw)
	if err != nil {
		return result, ErrArtifactUnavailable
	}
	result = WikiQualitySourceVersionEvent{
		AggregateVersion: event.AggregateVersion, EventID: eventID,
		EventType: eventType, EventJCSSHA256: h,
		JCSSource:   "outbox_jsonb_canonical_at_read",
		DeliveredAt: deliveredAt.UTC().Format(time.RFC3339Nano)}
	if event.EventType == "knowledge.content.withdrawn.v1" {
		if len(payloadFields) != 2 || payloadFields["request"] == nil ||
			payloadFields["actor"] == nil {
			return WikiQualitySourceVersionEvent{}, ErrArtifactUnavailable
		}
		var requestFields map[string]json.RawMessage
		if json.Unmarshal(payloadFields["request"], &requestFields) != nil ||
			len(requestFields) != 5 {
			return WikiQualitySourceVersionEvent{}, ErrArtifactUnavailable
		}
		for _, key := range []string{"ModuleId", "target_kind", "target_id", "reason", "idempotency_key"} {
			if requestFields[key] == nil {
				return WikiQualitySourceVersionEvent{}, ErrArtifactUnavailable
			}
		}
		var payload struct {
			Request types.WithdrawReq `json:"request"`
			Actor   string            `json:"actor"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil ||
			payload.Request.ModuleId != moduleID || payload.Actor == "" ||
			(payload.Request.TargetKind != "revision" && payload.Request.TargetKind != "module") ||
			!citationIdentity(payload.Request.TargetId) || payload.Request.Reason == "" ||
			payload.Request.IdempotencyKey == "" ||
			(payload.Request.TargetKind == "module" && payload.Request.TargetId != moduleID) {
			return WikiQualitySourceVersionEvent{}, ErrArtifactUnavailable
		}
		result.TargetKind = payload.Request.TargetKind
		result.TargetID = payload.Request.TargetId
	}
	return result, nil
}
