package model

import (
	"context"
	"encoding/json"
	"errors"
	"sea-try-go/service/knowledge/api/internal/telemetry"

	"github.com/jackc/pgx/v5"
)

// Event is an RTW-owned immutable fact. Payload keeps each domain's versioned schema.
type Event struct {
	AggregateVersion int64           `json:"aggregate_version"`
	OperationID      string          `json:"operation_id"`
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	Producer         string          `json:"producer"`
	AggregateID      string          `json:"aggregate_id"`
	OccurredAt       string          `json:"occurred_at"`
	Payload          json.RawMessage `json:"payload"`
}

// TechnicalReceipt confirms durable transport acceptance only, not domain acceptance or publication.
type TechnicalReceipt struct {
	EventID         string `json:"event_id"`
	TechnicalStatus string `json:"technical_status"`
}
type Sender interface {
	Send(context.Context, Event) (TechnicalReceipt, error)
}

// DispatchOne locks a pending event until its acknowledgement commits. Lost receipts
// replay the same event ID; the receiver must deduplicate before acknowledging.
func (s *Store) DispatchOne(ctx context.Context, sender Sender) (sent bool, resultErr error) {
	selected := false
	defer func() {
		if resultErr != nil && !selected {
			s.Observability.PollFailure(ctx, resultErr)
		}
	}()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var raw, correlationRaw []byte
	err = tx.QueryRow(ctx, "SELECT payload,correlation FROM knowledge_outbox WHERE delivered_at IS NULL ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&raw, &correlationRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var event Event
	if err = json.Unmarshal(raw, &event); err != nil {
		return false, err
	}
	var correlation telemetry.Correlation
	if err = json.Unmarshal(correlationRaw, &correlation); err != nil {
		return false, err
	}
	ctx = telemetry.Resume(ctx, correlation, event.OperationID)
	ctx, finish := s.Observability.Begin(ctx, "knowledge.outbox.deliver", event.OperationID, map[string]any{"event_id": event.EventID, "event_type": event.EventType, "aggregate_id": event.AggregateID, "aggregate_version": event.AggregateVersion})
	selected = true
	defer func() {
		info := telemetry.ErrorInfo{}
		if resultErr != nil {
			info = ClassifyError(resultErr)
		}
		finish(resultErr, info, map[string]any{"technical_status": map[bool]string{true: "accepted", false: "unconfirmed"}[sent]})
	}()
	receipt, err := sender.Send(ctx, event)
	if err != nil {
		return false, err
	}
	if receipt.EventID != event.EventID || receipt.TechnicalStatus != "accepted" {
		return false, conflict("technical acknowledgement does not match event")
	}
	if _, err = tx.Exec(ctx, "UPDATE knowledge_outbox SET delivered_at=now() WHERE event_id=$1 AND delivered_at IS NULL", event.EventID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
