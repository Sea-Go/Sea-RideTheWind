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
	EventID   string    `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	EventType string    `gorm:"column:event_type;type:varchar(64);not null"`
	Payload   string    `gorm:"column:payload;type:jsonb;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP"`
}

func (LikeDomainFactOutbox) TableName() string { return "like_domain_fact_outbox" }

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
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	return tx.Create(&LikeDomainFactOutbox{EventID: eventID, EventType: fact.EventType, Payload: string(payload)}).Error
}
