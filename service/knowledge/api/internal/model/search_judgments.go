package model

import (
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

// searchJudgmentRevision is an RTW capture fact, not a complete evaluation
// qrel set. In particular, no candidate-pool coverage is asserted here.
type searchJudgmentRevision struct {
	JudgmentID          string `json:"judgment_id"`
	JudgmentRevision    int64  `json:"judgment_revision"`
	RevisionID          string `json:"judgment_revision_id"`
	BaseRevisionID      string `json:"base_revision_id"`
	SearchID            string `json:"search_id"`
	QueryText           string `json:"query_text"`
	QueryTextSHA256     string `json:"query_text_sha256"`
	QueryTime           string `json:"query_time"`
	ModuleID            string `json:"module_id"`
	ReleaseID           string `json:"release_id"`
	Generation          int64  `json:"generation"`
	PublicationRevision string `json:"publication_revision"`
	RequestSHA256       string `json:"request_sha256"`
	IndexManifestRef    string `json:"index_manifest_ref"`
	IndexManifestSHA256 string `json:"index_manifest_sha256"`
	ChunkManifestRef    string `json:"chunk_manifest_ref"`
	ChunkManifestSHA256 string `json:"chunk_manifest_sha256"`
	SourceKind          string `json:"source_kind"`
	ContentID           string `json:"content_id"`
	ContentRevisionID   string `json:"content_revision_id"`
	ChunkID             string `json:"chunk_id"`
	ChunkTextSHA256     string `json:"chunk_text_sha256"`
	ChunkText           string `json:"chunk_text"`
	OriginalRef         string `json:"original_ref"`
	OriginalSHA256      string `json:"original_sha256"`
	ContentAvailableAt  string `json:"content_available_at"`
	Grade               *int   `json:"grade"`
	RubricVersion       string `json:"rubric_version"`
	JudgmentSource      string `json:"judgment_source"`
	ActorID             string `json:"actor_id"`
	JudgedAt            string `json:"judged_at"`
	State               string `json:"state"`
	Reason              string `json:"reason"`
}

type judgmentSource struct {
	query     ProductSearchInput
	hash      string
	snapshot  types.SearchSnapshot
	queryAt   time.Time
	contentAt time.Time
	indexRef  string
	indexHash string
	chunkRef  string
	chunkHash string
	chunk     types.CitationChunk
}

// CheckSearchJudgmentSchema keeps the admin capture switch default-off on an
// older production database instead of returning a late write error.
func (s *Store) CheckSearchJudgmentSchema(ctx context.Context) error {
	for _, query := range []string{
		"SELECT revision_id,judgment_id,module_id,search_id,chunk_id,base_revision_id,state,grade,data FROM knowledge_search_judgment_revisions LIMIT 0",
		"SELECT search_id,chunk_id,judgment_id,revision_id FROM knowledge_search_judgment_heads LIMIT 0",
		"SELECT event_id,revision_id,event_json,event_sha256 FROM knowledge_search_judgment_events LIMIT 0",
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

// A committed product operation supplies the query and publication identity;
// a source read verifies that the candidate belongs to its immutable index.
func (s *Store) searchJudgmentSource(ctx context.Context, moduleID, searchID, revisionID, chunkID string) (judgmentSource, error) {
	var source judgmentSource
	var requestRaw, snapshotRaw []byte
	var status, answerID string
	err := s.DB.QueryRow(ctx, `SELECT request_json,snapshot,request_hash,created_at,status,answer_id
 FROM knowledge_product_search_operations WHERE search_id=$1`, searchID).
		Scan(&requestRaw, &snapshotRaw, &source.hash, &source.queryAt, &status, &answerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, ErrNotFound
	}
	if err != nil {
		return source, err
	}
	if status != "committed" {
		return source, conflict("search has no accepted product answer")
	}
	var accepted bool
	if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_accepted_answers
 WHERE search_id=$1 AND answer_id=$2)`, searchID, answerID).Scan(&accepted); err != nil {
		return source, err
	}
	if !accepted {
		return source, conflict("search has no accepted product answer")
	}
	if err = json.Unmarshal(requestRaw, &source.query); err != nil {
		return source, ErrArtifactUnavailable
	}
	if err = json.Unmarshal(snapshotRaw, &source.snapshot); err != nil {
		return source, ErrArtifactUnavailable
	}
	computed, err := source.query.Hash()
	if err != nil || computed != source.hash || source.query.ModuleID != moduleID ||
		source.snapshot.ModuleId != moduleID || source.snapshot.ReleaseId == "" ||
		source.snapshot.Generation < 1 || source.snapshot.PublicationRevision == "" {
		return source, ErrArtifactUnavailable
	}
	var buildID string
	err = s.DB.QueryRow(ctx, `SELECT p.created_at,p.build_id,b.data->>'index_manifest_ref',
 b.data->>'index_manifest_hash' FROM knowledge_publications p
 JOIN knowledge_builds b ON b.id=p.build_id AND b.module_id=p.module_id AND b.release_id=p.release_id
 WHERE p.module_id=$1 AND p.release_id=$2 AND p.pointer_revision::text=$3
 AND b.generation=$4 AND b.data->>'state'='READY'`, moduleID, source.snapshot.ReleaseId,
		source.snapshot.PublicationRevision, source.snapshot.Generation).
		Scan(&source.contentAt, &buildID, &source.indexRef, &source.indexHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, ErrNotFound
	}
	if err != nil {
		return source, err
	}
	if source.contentAt.After(source.queryAt) {
		return source, ErrArtifactUnavailable
	}
	_, build, index, _, err := s.citationPublication(ctx, citationSnapshot{ModuleID: moduleID,
		ReleaseID: source.snapshot.ReleaseId, Generation: source.snapshot.Generation,
		PublicationRevision: source.snapshot.PublicationRevision})
	if err != nil {
		return source, err
	}
	if build.BuildId != buildID || build.IndexManifestRef != source.indexRef ||
		build.IndexManifestHash != source.indexHash {
		return source, ErrArtifactUnavailable
	}
	source.chunkRef, source.chunkHash = index.ChunkManifest.Key, index.ChunkManifest.SHA256
	source.chunk, err = s.ReadSearchSource(ctx, types.ReadSearchSourceReq{ModuleId: moduleID,
		ReleaseId: source.snapshot.ReleaseId, Generation: source.snapshot.Generation,
		PublicationRevision: source.snapshot.PublicationRevision, RevisionId: revisionID, ChunkId: chunkID})
	if err != nil {
		return source, err
	}
	if source.chunk.RevisionId != revisionID || source.chunk.ChunkId != chunkID ||
		!citationHash(source.chunk.TextHash) || !citationHash(source.chunk.Original.Sha256) {
		return source, ErrArtifactUnavailable
	}
	return source, nil
}

func validateJudgmentIdentity(actor, moduleID, searchID, revisionID, chunkID, key, reason string) error {
	if !validLogicalSessionID(actor) || !citationIdentity(moduleID) || !citationIdentity(searchID) ||
		!citationIdentity(revisionID) || !citationHash(chunkID) ||
		!citationIdentity(key) || len(key) < 8 || len(key) > 200 ||
		strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return invalid("bounded administrator, search, chunk, reason and idempotency key required")
	}
	return nil
}

func (s *Store) RecordSearchJudgment(ctx context.Context, actor string, req types.RecordSearchJudgmentReq) (types.SearchJudgmentReceipt, error) {
	return observe(ctx, s, "knowledge.search.judgment.record", operationID("search-judgment/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.SearchJudgmentReceipt, error) {
			if err := validateJudgmentIdentity(actor, req.ModuleId, req.SearchId, req.ContentRevisionId,
				req.ChunkId, req.IdempotencyKey, req.Reason); err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			grade, err := strconv.Atoi(req.Grade)
			if err != nil || strconv.Itoa(grade) != req.Grade || grade < 0 || grade > 3 ||
				req.RubricVersion != "sea.search.relevance.v1" {
				return types.SearchJudgmentReceipt{}, invalid("grade 0-3 and sea.search.relevance.v1 rubric required")
			}
			if req.BaseRevisionId != "" && !citationIdentity(req.BaseRevisionId) {
				return types.SearchJudgmentReceipt{}, invalid("base revision identity invalid")
			}
			return s.commitSearchJudgment(ctx, actor, req.ModuleId, req.SearchId,
				req.ContentRevisionId, req.ChunkId, req.BaseRevisionId, req.IdempotencyKey,
				req.Reason, req.RubricVersion, &grade)
		})
}

func (s *Store) WithdrawSearchJudgment(ctx context.Context, actor string, req types.WithdrawSearchJudgmentReq) (types.SearchJudgmentReceipt, error) {
	return observe(ctx, s, "knowledge.search.judgment.withdraw", operationID("search-judgment/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.SearchJudgmentReceipt, error) {
			if !validLogicalSessionID(actor) || !citationIdentity(req.ModuleId) || !citationIdentity(req.SearchId) ||
				!citationHash(req.ChunkId) || !citationIdentity(req.BaseRevisionId) ||
				!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 200 ||
				strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 2000 {
				return types.SearchJudgmentReceipt{}, invalid("bounded withdrawal identity, base, reason and key required")
			}
			return s.commitSearchJudgment(ctx, actor, req.ModuleId, req.SearchId,
				"", req.ChunkId, req.BaseRevisionId, req.IdempotencyKey, req.Reason, "", nil)
		})
}

func (s *Store) commitSearchJudgment(ctx context.Context, actor, moduleID, searchID, contentRevisionID,
	chunkID, baseRevisionID, key, reason, rubric string, grade *int) (types.SearchJudgmentReceipt, error) {
	// An idempotent replay must not fail just because the current publication or
	// source was later withdrawn. Source validation therefore runs inside fn,
	// after command() has checked its durable replay row.
	input := struct {
		ModuleID, SearchID, ContentRevisionID, ChunkID, BaseRevisionID, Reason, Rubric string
		Grade                                                                          *int
	}{moduleID, searchID, contentRevisionID, chunkID, baseRevisionID, reason, rubric, grade}
	return command(ctx, s, "search-judgment/"+moduleID+"/"+actor, key, input,
		func(tx pgx.Tx) (types.SearchJudgmentReceipt, error) {
			m, err := module(ctx, tx, moduleID, true)
			if err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			if err = enabled(m); err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			var priorID, judgmentID string
			err = tx.QueryRow(ctx, `SELECT judgment_id,revision_id FROM knowledge_search_judgment_heads
 WHERE search_id=$1 AND chunk_id=$2`, searchID, chunkID).Scan(&judgmentID, &priorID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return types.SearchJudgmentReceipt{}, err
			}
			if priorID != baseRevisionID {
				return types.SearchJudgmentReceipt{}, conflict("judgment base revision no longer current")
			}
			var revision searchJudgmentRevision
			var priorRevision int64
			if grade == nil {
				if priorID == "" {
					return types.SearchJudgmentReceipt{}, conflict("no active judgment to withdraw")
				}
				revision, err = readJSON[searchJudgmentRevision](ctx, tx,
					"SELECT data FROM knowledge_search_judgment_revisions WHERE revision_id=$1", priorID)
				if err != nil {
					return types.SearchJudgmentReceipt{}, err
				}
				if revision.State != "judged" || revision.ModuleID != moduleID || revision.SearchID != searchID || revision.ChunkID != chunkID {
					return types.SearchJudgmentReceipt{}, conflict("judgment is not active in this module")
				}
				priorRevision = revision.JudgmentRevision
			} else {
				source, sourceErr := s.searchJudgmentSource(ctx, moduleID, searchID, contentRevisionID, chunkID)
				if sourceErr != nil {
					return types.SearchJudgmentReceipt{}, sourceErr
				}
				if priorID != "" {
					old, readErr := readJSON[searchJudgmentRevision](ctx, tx,
						"SELECT data FROM knowledge_search_judgment_revisions WHERE revision_id=$1", priorID)
					if readErr != nil {
						return types.SearchJudgmentReceipt{}, readErr
					}
					if old.ModuleID != moduleID || old.SearchID != searchID || old.ChunkID != chunkID ||
						old.ContentRevisionID != contentRevisionID {
						return types.SearchJudgmentReceipt{}, conflict("judgment target changed")
					}
					priorRevision = old.JudgmentRevision
				}
				revision = searchJudgmentRevision{SearchID: searchID, QueryText: source.query.Query,
					QueryTextSHA256: object.Hash([]byte(source.query.Query)), QueryTime: source.queryAt.UTC().Format(time.RFC3339Nano),
					ModuleID: moduleID, ReleaseID: source.snapshot.ReleaseId, Generation: source.snapshot.Generation,
					PublicationRevision: source.snapshot.PublicationRevision, RequestSHA256: source.hash,
					IndexManifestRef: source.indexRef, IndexManifestSHA256: source.indexHash,
					ChunkManifestRef: source.chunkRef, ChunkManifestSHA256: source.chunkHash,
					SourceKind: source.chunk.SourceKind, ContentID: source.chunk.ContentId,
					ContentRevisionID: source.chunk.RevisionId, ChunkID: source.chunk.ChunkId,
					ChunkTextSHA256: source.chunk.TextHash, OriginalRef: source.chunk.Original.Key,
					ChunkText:          source.chunk.Text,
					OriginalSHA256:     source.chunk.Original.Sha256,
					ContentAvailableAt: source.contentAt.UTC().Format(time.RFC3339Nano),
					Grade:              grade, RubricVersion: rubric, JudgmentSource: "human_judgment", State: "judged"}
			}
			if judgmentID == "" {
				judgmentID = id("judgment")
			}
			if priorID != "" && priorRevision < 1 {
				return types.SearchJudgmentReceipt{}, ErrArtifactUnavailable
			}
			revision.JudgmentID, revision.RevisionID, revision.BaseRevisionID = judgmentID, id("judgment_revision"), baseRevisionID
			revision.JudgmentRevision = priorRevision + 1
			revision.ActorID, revision.Reason = actor, reason
			var judgedAt time.Time
			if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&judgedAt); err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			revision.JudgedAt = judgedAt.UTC().Format(time.RFC3339Nano)
			if grade == nil {
				revision.Grade, revision.State = nil, "withdrawn"
			}
			if err = saveJSON(ctx, tx, `INSERT INTO knowledge_search_judgment_revisions
 (revision_id,judgment_id,module_id,search_id,chunk_id,base_revision_id,state,grade,data)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, revision, revision.RevisionID,
				revision.JudgmentID, moduleID, searchID, chunkID, baseRevisionID, revision.State, revision.Grade); err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO knowledge_search_judgment_heads
 (search_id,chunk_id,judgment_id,revision_id) VALUES($1,$2,$3,$4)
 ON CONFLICT(search_id,chunk_id) DO UPDATE SET revision_id=excluded.revision_id`,
				searchID, chunkID, judgmentID, revision.RevisionID)
			if err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			eventType := "knowledge.search.judgment.revised.v1"
			if grade == nil {
				eventType = "knowledge.search.judgment.withdrawn.v1"
			}
			event, raw, err := emitWithReceipt(ctx, tx, eventType, moduleID, revision)
			if err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			hash := object.Hash(raw)
			_, err = tx.Exec(ctx, `INSERT INTO knowledge_search_judgment_events
 (event_id,revision_id,event_json,event_sha256) VALUES($1,$2,$3,$4)`,
				event.EventID, revision.RevisionID, string(raw), hash)
			if err != nil {
				return types.SearchJudgmentReceipt{}, err
			}
			return types.SearchJudgmentReceipt{JudgmentId: judgmentID, RevisionId: revision.RevisionID,
				SearchId: searchID, ChunkId: chunkID, State: revision.State,
				EventId: event.EventID, EventSha256: hash}, nil
		})
}

// GetSearchJudgmentEvent returns the original emission bytes, even after a
// withdrawal. A transport ACK is not an evaluation or label acceptance.
func (s *Store) GetSearchJudgmentEvent(ctx context.Context, eventID string) (types.SearchJudgmentEventReceipt, error) {
	return observe(ctx, s, "knowledge.search.judgment.event.read", "judgment-event:"+eventID,
		types.SearchJudgmentEventPath{EventId: eventID}, func(ctx context.Context) (types.SearchJudgmentEventReceipt, error) {
			if !citationIdentity(eventID) {
				return types.SearchJudgmentEventReceipt{}, invalid("event identity required")
			}
			var out types.SearchJudgmentEventReceipt
			var revisionID string
			err := s.DB.QueryRow(ctx, `SELECT e.event_id,e.event_json,e.event_sha256,e.revision_id
 FROM knowledge_search_judgment_events e JOIN knowledge_outbox o ON o.event_id=e.event_id
 JOIN knowledge_search_judgment_revisions r ON r.revision_id=e.revision_id
 WHERE e.event_id=$1 AND o.payload=e.event_json::jsonb AND r.data=(o.payload->'payload')`,
				eventID).Scan(&out.EventId, &out.EventJson, &out.EventSha256, &revisionID)
			if errors.Is(err, pgx.ErrNoRows) {
				return types.SearchJudgmentEventReceipt{}, ErrNotFound
			}
			if err != nil {
				return types.SearchJudgmentEventReceipt{}, err
			}
			if object.Hash([]byte(out.EventJson)) != out.EventSha256 {
				return types.SearchJudgmentEventReceipt{}, ErrArtifactUnavailable
			}
			var event Event
			if err = json.Unmarshal([]byte(out.EventJson), &event); err != nil || event.EventID != eventID ||
				event.Producer != "ridethewind.knowledge" || event.SchemaVersion != 1 || event.AggregateVersion < 1 ||
				(event.EventType != "knowledge.search.judgment.revised.v1" && event.EventType != "knowledge.search.judgment.withdrawn.v1") {
				return types.SearchJudgmentEventReceipt{}, ErrArtifactUnavailable
			}
			var payload searchJudgmentRevision
			if err = json.Unmarshal(event.Payload, &payload); err != nil || payload.RevisionID != revisionID ||
				payload.ModuleID != event.AggregateID || payload.JudgmentSource != "human_judgment" ||
				payload.JudgmentRevision < 1 || object.Hash([]byte(payload.QueryText)) != payload.QueryTextSHA256 ||
				object.Hash([]byte(payload.ChunkText)) != payload.ChunkTextSHA256 ||
				(event.EventType == "knowledge.search.judgment.revised.v1" && (payload.State != "judged" || payload.Grade == nil)) ||
				(event.EventType == "knowledge.search.judgment.withdrawn.v1" && (payload.State != "withdrawn" || payload.Grade != nil || payload.BaseRevisionID == "")) {
				return types.SearchJudgmentEventReceipt{}, ErrArtifactUnavailable
			}
			return out, nil
		})
}
