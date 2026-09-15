package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sea-try-go/service/common/communityfact"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const commentFactProducer = "rtw.comment-rpc"

func validCommentDelivery(row CommentDomainFactOutbox, event communityfact.Event) bool {
	if row.AggregateID == nil || row.FactVersion == nil || row.DeliveryEnvelope == nil ||
		event.EventID != row.EventID || event.EventType != row.EventType ||
		event.SchemaVersion != 1 || event.Producer != commentFactProducer ||
		event.AggregateID != *row.AggregateID || event.AggregateVersion != *row.FactVersion ||
		event.OperationID == "" || !json.Valid(event.Payload) {
		return false
	}
	occurred, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if err != nil {
		return false
	}
	var payload commentFact
	if err := communityfact.StrictDecode(strings.NewReader(string(event.Payload)), &payload); err != nil ||
		payload.SchemaVersion != "rtw.community-fact.v1" || payload.EventID != event.EventID ||
		payload.EventType != event.EventType || payload.Producer != event.Producer ||
		payload.AggregateID != event.AggregateID || payload.AggregateVersion != nil ||
		payload.OperationID != event.OperationID || payload.CommentID != event.AggregateID ||
		payload.SubjectRef == "" || payload.TargetType == "" || payload.TargetID == "" ||
		!payload.OccurredAt.Equal(occurred) {
		return false
	}
	payloadHash, err := communityfact.CanonicalHash(event.Payload)
	if err != nil {
		return false
	}
	rowHash, err := communityfact.CanonicalHash([]byte(row.Payload))
	return err == nil && payloadHash == rowHash
}

// DispatchFactOnce keeps one source row locked until a matching DataCenter
// technical receipt is committed. Earlier versions of the same comment stream
// must be accepted first, so SKIP LOCKED cannot reorder a retract.
func (m *CommentModel) DispatchFactOnce(ctx context.Context, sender communityfact.Sender, logger *slog.Logger) (bool, error) {
	started := time.Now()
	if m == nil || m.conn == nil || sender == nil || logger == nil {
		return false, errors.New("comment fact dispatcher is not configured")
	}
	tx := m.conn.WithContext(ctx).Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	defer tx.Rollback()
	var row CommentDomainFactOutbox
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("delivery_status IN ? AND delivery_envelope IS NOT NULL", []int32{communityfact.DeliveryPending, communityfact.DeliveryFailed}).
		Where(`NOT EXISTS (SELECT 1 FROM comment_domain_fact_outbox predecessor
			WHERE predecessor.aggregate_id=comment_domain_fact_outbox.aggregate_id
			AND predecessor.fact_version < comment_domain_fact_outbox.fact_version
			AND predecessor.delivery_status <> ?)`, communityfact.DeliveryAccepted).
		Order("created_at,event_id").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var event communityfact.Event
	if err := communityfact.StrictDecode(strings.NewReader(*row.DeliveryEnvelope), &event); err != nil || !validCommentDelivery(row, event) {
		if updateErr := tx.Model(&CommentDomainFactOutbox{}).Where("event_id = ?", row.EventID).
			Update("delivery_status", communityfact.DeliveryBlocked).Error; updateErr != nil {
			return false, updateErr
		}
		if err := tx.Commit().Error; err != nil {
			return false, err
		}
		logger.WarnContext(ctx, "comment fact frozen envelope blocked", "event", "comment.fact_delivery.blocked",
			"event_id", row.EventID, "producer", commentFactProducer, "outcome", "manual_migration_required",
			"duration_ms", float64(time.Since(started).Microseconds())/1000, "error_code", "INVALID_FROZEN_ENVELOPE")
		return false, communityfact.ErrFrozenEnvelopeBlocked
	}
	logger.InfoContext(ctx, "comment fact delivery started", "event", "comment.fact_delivery.started",
		"event_id", event.EventID, "producer", event.Producer, "aggregate_version", event.AggregateVersion)
	receipt, sendErr := sender.Send(ctx, event, json.RawMessage(*row.DeliveryEnvelope))
	received, receiptOK := communityfact.ValidateReceipt(event, receipt)
	if sendErr != nil || !receiptOK {
		if sendErr == nil {
			sendErr = errors.New("DataCenter technical receipt does not match comment event")
		}
		if err := tx.Model(&CommentDomainFactOutbox{}).Where("event_id = ?", row.EventID).
			Updates(map[string]any{"delivery_status": communityfact.DeliveryFailed,
				"retry_count": gorm.Expr("retry_count + 1")}).Error; err != nil {
			return false, err
		}
		if err := tx.Commit().Error; err != nil {
			return false, err
		}
		logger.WarnContext(ctx, "comment fact delivery deferred", "event", "comment.fact_delivery.deferred",
			"event_id", event.EventID, "producer", event.Producer, "outcome", "retryable",
			"duration_ms", float64(time.Since(started).Microseconds())/1000, "retry_count", row.RetryCount+1)
		return false, sendErr
	}
	deliveredAt := time.Now().UTC()
	updates := map[string]any{"delivery_status": communityfact.DeliveryAccepted,
		"technical_receipt_id": receipt.ReceiptID, "technical_input_hash": receipt.InputHash,
		"technical_offset": receipt.Offset, "technical_received_at": received, "delivered_at": deliveredAt}
	if err := tx.Model(&CommentDomainFactOutbox{}).Where("event_id = ?", row.EventID).Updates(updates).Error; err != nil {
		return false, err
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	logger.InfoContext(ctx, "comment fact technical receipt committed", "event", "comment.fact_delivery.accepted",
		"event_id", event.EventID, "producer", event.Producer, "outcome", "succeeded",
		"duration_ms", float64(time.Since(started).Microseconds())/1000,
		"receipt_id", receipt.ReceiptID, "offset", receipt.Offset)
	return true, nil
}

func decodeCommentDelivery(raw string) (communityfact.Event, error) {
	var event communityfact.Event
	if err := communityfact.StrictDecode(strings.NewReader(raw), &event); err != nil {
		return communityfact.Event{}, fmt.Errorf("decode comment delivery envelope: %w", err)
	}
	return event, nil
}
