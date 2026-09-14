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
	EventID   string    `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	EventType string    `gorm:"column:event_type;type:varchar(64);not null"`
	Payload   string    `gorm:"column:payload;type:jsonb;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP"`
}

func (CommentDomainFactOutbox) TableName() string { return "comment_domain_fact_outbox" }

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
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	return tx.Create(&CommentDomainFactOutbox{
		EventID: fact.EventID, EventType: fact.EventType, Payload: string(payload),
	}).Error
}

func newCommentInteractionID() string { return "rtw.comment.interaction/" + uuid.NewString() }
