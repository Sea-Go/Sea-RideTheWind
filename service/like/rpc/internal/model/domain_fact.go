package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// LikeDomainFactOutbox is append-only and is not consumed by the legacy hot
// ranking relay. A separate H09.a delivery adapter must claim it later.
type LikeDomainFactOutbox struct {
	EventID             string     `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	EventType           string     `gorm:"column:event_type;type:varchar(64);not null"`
	Payload             string     `gorm:"column:payload;type:jsonb;not null"`
	AggregateID         *string    `gorm:"column:aggregate_id;type:varchar(128);uniqueIndex:uk_like_fact_stream"`
	FactVersion         *int64     `gorm:"column:fact_version;uniqueIndex:uk_like_fact_stream"`
	DeliveryEnvelope    *string    `gorm:"column:delivery_envelope;type:jsonb"`
	DeliveryStatus      int32      `gorm:"column:delivery_status;not null;default:0;index"`
	RetryCount          int32      `gorm:"column:retry_count;not null;default:0"`
	TechnicalReceiptID  string     `gorm:"column:technical_receipt_id;type:varchar(128);not null;default:''"`
	TechnicalInputHash  string     `gorm:"column:technical_input_hash;type:char(64);not null;default:''"`
	TechnicalOffset     int64      `gorm:"column:technical_offset;not null;default:0"`
	TechnicalReceivedAt *time.Time `gorm:"column:technical_received_at"`
	DeliveredAt         *time.Time `gorm:"column:delivered_at"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP"`
}

func (LikeDomainFactOutbox) TableName() string { return "like_domain_fact_outbox" }

// LikeFactStream serializes state-change facts for one user and target. The
// version is not a content revision or the client's operation ID.
type LikeFactStream struct {
	AggregateID string `gorm:"column:aggregate_id;primaryKey;type:varchar(128)"`
	Version     int64  `gorm:"column:version;not null"`
}

func (LikeFactStream) TableName() string { return "like_fact_stream" }

type likeDeliveryEnvelope struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	Producer         string          `json:"producer"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	OperationID      string          `json:"operation_id"`
	OccurredAt       string          `json:"occurred_at"`
	Payload          json.RawMessage `json:"payload"`
}

type likeFact struct {
	SchemaVersion    string    `json:"schema_version"`
	EventID          string    `json:"event_id"`
	EventType        string    `json:"event_type"`
	Producer         string    `json:"producer"`
	AggregateID      string    `json:"aggregate_id"`
	AggregateVersion *int64    `json:"aggregate_version"`
	OperationID      string    `json:"operation_id"`
	SubjectRef       string    `json:"subject_ref"`
	TargetType       string    `json:"target_type"`
	TargetID         string    `json:"target_id"`
	TargetRevision   *string   `json:"target_revision"`
	RevisionStatus   string    `json:"revision_status"`
	Operation        string    `json:"operation"`
	SourceRef        string    `json:"source_ref"`
	OldState         int32     `json:"old_state"`
	NewState         int32     `json:"new_state"`
	EventTime        time.Time `json:"event_time"`
	OccurredAt       time.Time `json:"occurred_at"`
	AvailableAt      time.Time `json:"available_at"`
}

func appendLikeFact(tx *gorm.DB, p *LikeProcessPayload, oldState, newState int32) error {
	if p.Record.UserID <= 0 || p.Record.TargetType == "" || p.Record.TargetID == "" || p.Inbox.MsgId == "" {
		return fmt.Errorf("incomplete like fact")
	}
	operation := map[int32]string{1: "like", 2: "unlike", 3: "dislike", 4: "undislike"}[p.Record.State]
	if operation == "" {
		return fmt.Errorf("invalid like operation: %d", p.Record.State)
	}
	eventID := "rtw.like/" + p.Inbox.MsgId
	eventTime := time.Unix(p.OccurredAt, 0).UTC()
	if p.OccurredAt <= 0 {
		eventTime = time.Now().UTC()
	}
	fact := likeFact{
		SchemaVersion: "rtw.community-fact.v1", EventID: eventID,
		EventType: "community.target.interaction", Producer: "rtw.like-mq",
		AggregateID: p.Record.TargetType + "/" + p.Record.TargetID,
		OperationID: p.Inbox.MsgId, SubjectRef: "rtw.identity/platform/" + strconv.FormatInt(p.Record.UserID, 10),
		TargetType: p.Record.TargetType, TargetID: p.Record.TargetID, RevisionStatus: "unknown",
		Operation: operation, SourceRef: eventID, OldState: oldState, NewState: newState,
		EventTime: eventTime, OccurredAt: eventTime, AvailableAt: time.Now().UTC(),
	}
	// A user-target is the durable state aggregate. The existing business
	// aggregate_id remains the target so H09 consumers keep their semantics.
	streamID := "like-state/" + strconv.FormatInt(p.Record.UserID, 10) + "/" + p.Record.TargetType + "/" + p.Record.TargetID
	var legacy int64
	if err := tx.Model(&LikeDomainFactOutbox{}).
		Where("payload->>'subject_ref' = ? AND payload->>'target_type' = ? AND payload->>'target_id' = ? AND delivery_envelope IS NULL",
			fact.SubjectRef, fact.TargetType, fact.TargetID).
		Count(&legacy).Error; err != nil {
		return err
	}
	if legacy > 0 {
		return fmt.Errorf("like aggregate %s has unversioned legacy facts", streamID)
	}
	var stream LikeFactStream
	if err := tx.Raw(`INSERT INTO like_fact_stream (aggregate_id, version) VALUES (?, 1)
		ON CONFLICT (aggregate_id) DO UPDATE SET version = like_fact_stream.version + 1
		RETURNING aggregate_id, version`, streamID).Scan(&stream).Error; err != nil {
		return err
	}
	if stream.Version < 1 || stream.Version > 9007199254740991 {
		return fmt.Errorf("like fact version out of DC range")
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(likeDeliveryEnvelope{
		EventID: fact.EventID, EventType: fact.EventType, SchemaVersion: 1,
		Producer: fact.Producer, AggregateID: streamID,
		AggregateVersion: stream.Version, OperationID: fact.OperationID,
		OccurredAt: fact.OccurredAt.Format(time.RFC3339Nano), Payload: payload,
	})
	if err != nil {
		return err
	}
	return tx.Create(&LikeDomainFactOutbox{
		EventID: eventID, EventType: fact.EventType, Payload: string(payload),
		AggregateID: &streamID, FactVersion: &stream.Version,
		DeliveryEnvelope: likeStringPtr(string(envelope)),
	}).Error
}

func likeStringPtr(value string) *string { return &value }
