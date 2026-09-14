package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CommentDomainFactOutbox is an append-only RTW fact source. Delivery to DC is
// deliberately separate from committing the comment's business transaction.
type CommentDomainFactOutbox struct {
	EventID          string    `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	EventType        string    `gorm:"column:event_type;type:varchar(64);not null"`
	Payload          string    `gorm:"column:payload;type:jsonb;not null"`
	AggregateID      *string   `gorm:"column:aggregate_id;type:varchar(128);uniqueIndex:uk_comment_fact_stream"`
	FactVersion      *int64    `gorm:"column:fact_version;uniqueIndex:uk_comment_fact_stream"`
	DeliveryEnvelope *string   `gorm:"column:delivery_envelope;type:jsonb"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP"`
}

func (CommentDomainFactOutbox) TableName() string { return "comment_domain_fact_outbox" }

// CommentFactStream is the transactionally allocated DC source version for a
// comment's fact stream. It is unrelated to a target article revision.
type CommentFactStream struct {
	AggregateID string `gorm:"column:aggregate_id;primaryKey;type:varchar(128)"`
	Version     int64  `gorm:"column:version;not null"`
}

func (CommentFactStream) TableName() string { return "comment_fact_stream" }

type commentDeliveryEnvelope struct {
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

type commentFact struct {
	SchemaVersion    string    `json:"schema_version"`
	EventID          string    `json:"event_id"`
	EventType        string    `json:"event_type"`
	Producer         string    `json:"producer"`
	AggregateID      string    `json:"aggregate_id"`
	AggregateVersion *int64    `json:"aggregate_version"`
	OperationID      string    `json:"operation_id"`
	SubjectRef       string    `json:"subject_ref"`
	OperatorRef      string    `json:"operator_ref,omitempty"`
	TargetType       string    `json:"target_type"`
	TargetID         string    `json:"target_id"`
	TargetRevision   *string   `json:"target_revision"`
	RevisionStatus   string    `json:"revision_status"`
	Operation        string    `json:"operation"`
	SourceRef        string    `json:"source_ref"`
	CommentID        string    `json:"comment_id"`
	ParentCommentID  string    `json:"parent_comment_id,omitempty"`
	OldState         *int32    `json:"old_state,omitempty"`
	NewState         *int32    `json:"new_state,omitempty"`
	VisibilityState  int32     `json:"visibility_state"`
	SearchEvidence   bool      `json:"search_evidence"`
	EventTime        time.Time `json:"event_time"`
	OccurredAt       time.Time `json:"occurred_at"`
	AvailableAt      time.Time `json:"available_at"`
}

func subjectRef(uid int64) (string, error) {
	if uid <= 0 {
		return "", fmt.Errorf("invalid comment subject uid: %d", uid)
	}
	return "rtw.identity/platform/" + strconv.FormatInt(uid, 10), nil
}

func appendCommentFact(tx *gorm.DB, fact commentFact) error {
	if fact.EventID == "" || fact.TargetType == "" || fact.TargetID == "" || fact.SubjectRef == "" {
		return fmt.Errorf("incomplete comment fact")
	}
	if fact.EventTime.IsZero() {
		fact.EventTime = time.Now().UTC()
	}
	fact.EventTime = fact.EventTime.UTC()
	fact.OccurredAt = fact.EventTime
	fact.AvailableAt = time.Now().UTC()
	fact.SchemaVersion = "rtw.community-fact.v1"
	fact.Producer = "rtw.comment-rpc"
	fact.RevisionStatus = "unknown"
	fact.SearchEvidence = false
	// A legacy row on this aggregate has no trusted fact-stream version. A
	// migration/reconciliation must resolve it before any newer fact can be
	// emitted; otherwise DC would see an apparently complete but false stream.
	var legacy int64
	if err := tx.Model(&CommentDomainFactOutbox{}).
		Where("payload->>'aggregate_id' = ? AND delivery_envelope IS NULL", fact.AggregateID).
		Count(&legacy).Error; err != nil {
		return err
	}
	if legacy > 0 {
		return fmt.Errorf("comment aggregate %s has unversioned legacy facts", fact.AggregateID)
	}
	var stream CommentFactStream
	if err := tx.Raw(`INSERT INTO comment_fact_stream (aggregate_id, version) VALUES (?, 1)
		ON CONFLICT (aggregate_id) DO UPDATE SET version = comment_fact_stream.version + 1
		RETURNING aggregate_id, version`, fact.AggregateID).Scan(&stream).Error; err != nil {
		return err
	}
	if stream.Version < 1 || stream.Version > 9007199254740991 {
		return fmt.Errorf("comment fact version out of DC range")
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(commentDeliveryEnvelope{
		EventID: fact.EventID, EventType: fact.EventType, SchemaVersion: 1,
		Producer: fact.Producer, AggregateID: fact.AggregateID,
		AggregateVersion: stream.Version, OperationID: fact.OperationID,
		OccurredAt: fact.OccurredAt.Format(time.RFC3339Nano), Payload: payload,
	})
	if err != nil {
		return err
	}
	return tx.Create(&CommentDomainFactOutbox{
		EventID: fact.EventID, EventType: fact.EventType, Payload: string(payload),
		AggregateID: &fact.AggregateID, FactVersion: &stream.Version,
		DeliveryEnvelope: stringPtr(string(envelope)),
	}).Error
}

func stringPtr(value string) *string { return &value }

func newCommentInteractionID() string { return "rtw.comment.interaction/" + uuid.NewString() }
