package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type wikiCompileJobRecord struct {
	CompileID, RequestedEventID, OperationID, SourceHash, SubmitHash, JobID string
	ReceivedAt                                                              time.Time
	CancelEventID                                                           *string
	CancelVersion                                                           int64
	TechnicalState                                                          string
}

func frozenWikiCompileFieldsSame(left, right types.Compile) bool {
	return left.CompileId == right.CompileId && left.ModuleId == right.ModuleId &&
		left.PageId == right.PageId && left.BaseRevisionId == right.BaseRevisionId &&
		reflect.DeepEqual(left.SourceRevisionIds, right.SourceRevisionIds) &&
		left.Guidance == right.Guidance && left.InputHash == right.InputHash &&
		left.Generation == right.Generation
}

func (s *Store) CheckWikiCompileJobCandidate(ctx context.Context) error {
	if s == nil || s.DB == nil || !s.wikiCompileJobs {
		return ErrInvalid
	}
	var sidecar *string
	if err := s.DB.QueryRow(ctx, "SELECT to_regclass('knowledge_compile_jobs')::text").Scan(&sidecar); err != nil || sidecar == nil {
		return ErrUnavailable
	}
	// A previously delivered H04 compile event has no Jobs mapping. It cannot
	// be silently treated as the new technical job receipt during this switch.
	var historical int64
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM knowledge_outbox o
	 WHERE o.event_type IN ('knowledge.wiki.compile.requested.v1','knowledge.wiki.compile.cancelled.v1',
	 'knowledge.wiki.compile.superseded.v1')
	 AND o.delivered_at IS NOT NULL AND NOT EXISTS (
	  SELECT 1 FROM knowledge_compile_jobs j WHERE j.requested_event_id=o.event_id OR j.cancel_event_id=o.event_id)`).Scan(&historical); err != nil {
		return err
	}
	if historical != 0 {
		return conflict("previously delivered compile events lack a DC Jobs mapping")
	}
	return nil
}

// DispatchWikiCompileJobOnce is the optional technical lane for the requested,
// administrator-cancelled and superseded Compile Outbox event types. The
// ordinary H04 dispatcher excludes them while
// this lane is enabled; every other event retains its original event sender.
// RTW Compile.State/Revision/Release are never changed by a DC job receipt.
func (s *Store) DispatchWikiCompileJobOnce(ctx context.Context, transport WikiCompileJobTransport) (sent bool, resultErr error) {
	if s == nil || s.DB == nil || !s.wikiCompileJobs || transport == nil {
		return false, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var raw, correlationRaw []byte
	err = tx.QueryRow(ctx, `SELECT payload,correlation FROM knowledge_outbox WHERE delivered_at IS NULL
	 AND event_type IN ('knowledge.wiki.compile.requested.v1','knowledge.wiki.compile.cancelled.v1',
	 'knowledge.wiki.compile.superseded.v1')
	 ORDER BY created_at,(payload->>'aggregate_version')::bigint,event_id
	 FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&raw, &correlationRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var event Event
	if err := decodeWikiCompileJSON(raw, &event); err != nil {
		return false, invalid("frozen compile Outbox EventSpec invalid")
	}
	var correlation telemetry.Correlation
	if err := json.Unmarshal(correlationRaw, &correlation); err != nil {
		return false, err
	}
	ctx = telemetry.Resume(ctx, correlation, event.OperationID)
	stageName := "knowledge.compile.job.submit"
	if event.EventType == wikiCompileCancelledEvent || event.EventType == wikiCompileSupersededEvent {
		stageName = "knowledge.compile.job.cancel"
	}
	ctx, finish := s.Observability.Begin(ctx, stageName, event.OperationID,
		map[string]any{"event_id": event.EventID, "event_type": event.EventType,
			"aggregate_id": event.AggregateID, "aggregate_version": event.AggregateVersion})
	defer func() {
		info := telemetry.ErrorInfo{}
		if resultErr != nil {
			info = ClassifyError(resultErr)
		}
		finish(resultErr, info, map[string]any{"technical_status": map[bool]string{true: "accepted", false: "unconfirmed"}[sent]})
	}()
	switch event.EventType {
	case wikiCompileRequestedEvent:
		if resultErr = s.submitWikiCompileJob(ctx, tx, transport, event, raw); resultErr != nil {
			return false, resultErr
		}
	case wikiCompileCancelledEvent, wikiCompileSupersededEvent:
		if resultErr = s.cancelWikiCompileJob(ctx, tx, transport, event); resultErr != nil {
			return false, resultErr
		}
	default:
		return false, ErrInvalid
	}
	if _, err := tx.Exec(ctx, "UPDATE knowledge_outbox SET delivered_at=now() WHERE event_id=$1 AND delivered_at IS NULL", event.EventID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) submitWikiCompileJob(ctx context.Context, tx pgx.Tx,
	transport WikiCompileJobTransport, event Event, raw []byte) error {
	var frozen types.Compile
	if !validWikiCompileEvent(event, wikiCompileRequestedEvent) ||
		decodeWikiCompileJSON(event.Payload, &frozen) != nil {
		return invalid("frozen Wiki Compile request invalid")
	}
	current, err := readJSON[types.Compile](ctx, tx,
		"SELECT data FROM knowledge_compiles WHERE id=$1 FOR UPDATE", frozen.CompileId)
	if err != nil {
		return err
	}
	if !frozenWikiCompileFieldsSame(frozen, current) {
		return conflict("Wiki Compile source changed after the requested Outbox commit")
	}
	// SKIP LOCKED can select this new requested row while another dispatcher
	// owns the old supersede Outbox row. The source replacement event must
	// have a committed DC cancellation receipt before any new Submit effect.
	var uncancelledPredecessors int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM knowledge_outbox prior
	 WHERE prior.event_type='knowledge.wiki.compile.superseded.v1'
	 AND prior.payload->'payload'->>'replacement_compile_id'=$1
	 AND prior.delivered_at IS NULL`, frozen.CompileId).Scan(&uncancelledPredecessors); err != nil {
		return err
	}
	if uncancelledPredecessors != 0 {
		return ErrWikiCompilePredecessorPending
	}
	submit, ticket, submitHash, err := buildWikiCompileSubmit(event, raw, frozen)
	if err != nil {
		return err
	}
	receipt, err := transport.Submit(ctx, submit)
	if err != nil {
		return err
	}
	if receipt.Producer != submit.Producer || receipt.OperationID != submit.OperationID ||
		receipt.InputHash != submitHash || receipt.TechnicalStatus != "accepted" ||
		receipt.JobID == "" || receipt.ReceivedAt == "" {
		return conflict("DC Wiki Compile submission receipt differs from frozen request")
	}
	if _, err := uuid.Parse(receipt.JobID); err != nil {
		return conflict("DC Wiki Compile receipt job ID invalid")
	}
	job, err := transport.Get(ctx, receipt.JobID)
	if err != nil {
		return fmt.Errorf("read DC original Wiki Compile job after submission: %w", err)
	}
	if !wikiCompileJobMatches(job, submit, submitHash, receipt.JobID) {
		return conflict("DC original Wiki Compile job does not match submission receipt")
	}
	received, err := time.Parse(time.RFC3339Nano, receipt.ReceivedAt)
	if err != nil {
		return conflict("DC Wiki Compile receipt time invalid")
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_compile_jobs
	 (compile_id,requested_event_id,requested_operation_id,source_event_jcs_sha256,
	  submit_input_hash,job_id,technical_received_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, ticket.CompileID,
		event.EventID, event.OperationID, ticket.SourceEventJCSSHA256, submitHash,
		receipt.JobID, received)
	if err != nil {
		return err
	}
	stored, err := readWikiCompileJobRecord(ctx, tx, ticket.CompileID)
	if err != nil {
		return err
	}
	if stored.RequestedEventID != event.EventID || stored.OperationID != event.OperationID ||
		stored.SourceHash != ticket.SourceEventJCSSHA256 || stored.SubmitHash != submitHash ||
		stored.JobID != receipt.JobID || !stored.ReceivedAt.Equal(received) || stored.CancelVersion != 0 {
		return conflict("RTW Wiki Compile technical sidecar conflicts with DC original job")
	}
	return nil
}

func readWikiCompileJobRecord(ctx context.Context, tx pgx.Tx, compileID string) (wikiCompileJobRecord, error) {
	var row wikiCompileJobRecord
	err := tx.QueryRow(ctx, `SELECT compile_id,requested_event_id,requested_operation_id,
	 source_event_jcs_sha256,submit_input_hash,job_id::text,technical_received_at,
	 cancel_event_id,cancel_version,technical_state FROM knowledge_compile_jobs
	 WHERE compile_id=$1 FOR UPDATE`, compileID).
		Scan(&row.CompileID, &row.RequestedEventID, &row.OperationID, &row.SourceHash,
			&row.SubmitHash, &row.JobID, &row.ReceivedAt, &row.CancelEventID,
			&row.CancelVersion, &row.TechnicalState)
	return row, err
}

func (s *Store) cancelWikiCompileJob(ctx context.Context, tx pgx.Tx,
	transport WikiCompileJobTransport, event Event) error {
	var payload struct {
		Compile               types.Compile `json:"compile"`
		ReplacementCompileID  string        `json:"replacement_compile_id"`
		ReplacementGeneration int64         `json:"replacement_generation"`
		Reason                string        `json:"reason"`
	}
	if (event.EventType != wikiCompileCancelledEvent && event.EventType != wikiCompileSupersededEvent) ||
		!validWikiCompileEvent(event, event.EventType) ||
		decodeWikiCompileJSON(event.Payload, &payload) != nil ||
		payload.Compile.CancelVersion != 1 || payload.Reason == "" ||
		payload.Compile.ModuleId != event.AggregateID {
		return invalid("frozen Wiki Compile cancellation invalid")
	}
	expectedState := "CANCELLED"
	dcReason := "rtw_compile_cancelled"
	if event.EventType == wikiCompileSupersededEvent {
		expectedState, dcReason = "SUPERSEDED", "rtw_compile_superseded"
		if payload.Reason != "new_generation" || payload.ReplacementCompileID == "" ||
			payload.ReplacementGeneration <= payload.Compile.Generation {
			return invalid("Wiki Compile replacement linkage invalid")
		}
		replacement, err := readJSON[types.Compile](ctx, tx,
			"SELECT data FROM knowledge_compiles WHERE id=$1", payload.ReplacementCompileID)
		if errors.Is(err, ErrNotFound) {
			return conflict("Wiki Compile replacement does not exist in RTW source")
		}
		if err != nil {
			return err
		}
		if replacement.ModuleId != payload.Compile.ModuleId ||
			replacement.PageId != payload.Compile.PageId ||
			replacement.Generation != payload.ReplacementGeneration {
			return conflict("Wiki Compile replacement does not exist in RTW source")
		}
	} else if payload.ReplacementCompileID != "" || payload.ReplacementGeneration != 0 {
		return invalid("administrator Wiki Compile cancel cannot carry a replacement")
	}
	current, err := readJSON[types.Compile](ctx, tx,
		"SELECT data FROM knowledge_compiles WHERE id=$1 FOR UPDATE", payload.Compile.CompileId)
	if err != nil {
		return err
	}
	if !frozenWikiCompileFieldsSame(payload.Compile, current) ||
		current.State != expectedState || payload.Compile.State != expectedState ||
		current.CancelVersion != payload.Compile.CancelVersion {
		return conflict("Wiki Compile cancellation no longer matches source")
	}
	stored, err := readWikiCompileJobRecord(ctx, tx, payload.Compile.CompileId)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("Wiki Compile requested job receipt must precede cancellation")
	}
	if err != nil {
		return err
	}
	if stored.CancelVersion != 0 || stored.CancelEventID != nil {
		return conflict("Wiki Compile job cancellation already recorded under another event")
	}
	job, err := transport.Get(ctx, stored.JobID)
	if err != nil {
		return fmt.Errorf("read DC original Wiki Compile job before cancellation: %w", err)
	}
	if job.JobID != stored.JobID || job.InputHash != stored.SubmitHash ||
		job.Request.OperationID != stored.OperationID || job.Request.JobType != WikiCompileJobType ||
		job.Request.Producer != "ridethewind.knowledge" {
		return conflict("DC Wiki Compile original job missing before cancellation")
	}
	cancel := WikiCompileJobCancel{OperationID: event.OperationID,
		ExpectedCancelVersion: 0, Reason: dcReason}
	receipt, err := transport.Cancel(ctx, stored.JobID, cancel)
	if err != nil {
		return err
	}
	if receipt.JobID != stored.JobID || receipt.OperationID != event.OperationID ||
		receipt.CancelVersion != payload.Compile.CancelVersion ||
		(receipt.TechnicalState != "cancelled" && receipt.TechnicalState != "cancel_requested") {
		return conflict("DC Wiki Compile cancellation receipt disagrees with RTW fence")
	}
	after, err := transport.Get(ctx, stored.JobID)
	if err != nil {
		return fmt.Errorf("read DC Wiki Compile job after cancellation: %w", err)
	}
	if after.CancelVersion != receipt.CancelVersion || after.State != receipt.TechnicalState {
		return conflict("DC Wiki Compile job cancellation not durable")
	}
	tag, err := tx.Exec(ctx, `UPDATE knowledge_compile_jobs SET cancel_event_id=$2,
	 cancel_version=$3,technical_state=$4 WHERE compile_id=$1 AND cancel_event_id IS NULL
	 AND cancel_version=0`, stored.CompileID, event.EventID, receipt.CancelVersion,
		receipt.TechnicalState)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return conflict("Wiki Compile cancellation sidecar changed before source ACK")
	}
	return nil
}
