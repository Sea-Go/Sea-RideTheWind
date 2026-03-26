package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

type JSONMap map[string]string

func (m JSONMap) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}

	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(b, m)
}

type Notification struct {
	ID          int64      `gorm:"primaryKey;autoIncrement:false"`
	RecipientID int64      `gorm:"column:recipient_id;not null;index:idx_notification_recipient_time,priority:1"`
	SenderID    int64      `gorm:"column:sender_id;not null"`
	SenderRole  int32      `gorm:"column:sender_role;not null"`
	Kind        int32      `gorm:"column:kind;not null"`
	Title       string     `gorm:"column:title;type:varchar(255);not null"`
	Content     string     `gorm:"column:content;type:text;not null"`
	IsRead      bool       `gorm:"column:is_read;not null;default:false;index:idx_notification_recipient_read,priority:2"`
	ReadAt      *time.Time `gorm:"column:read_at"`
	Extra       JSONMap    `gorm:"column:extra;type:jsonb"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_notification_recipient_time,priority:2"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (Notification) TableName() string {
	return "message_notification"
}

type Conversation struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement:false"`
	Participant1Role   int32     `gorm:"column:participant1_role;not null;uniqueIndex:uk_conversation_pair,priority:1;index:idx_conversation_p1,priority:1"`
	Participant1ID     int64     `gorm:"column:participant1_id;not null;uniqueIndex:uk_conversation_pair,priority:2;index:idx_conversation_p1,priority:2"`
	Participant2Role   int32     `gorm:"column:participant2_role;not null;uniqueIndex:uk_conversation_pair,priority:3;index:idx_conversation_p2,priority:1"`
	Participant2ID     int64     `gorm:"column:participant2_id;not null;uniqueIndex:uk_conversation_pair,priority:4;index:idx_conversation_p2,priority:2"`
	Status             int32     `gorm:"column:status;not null;default:1"`
	PendingSenderRole  int32     `gorm:"column:pending_sender_role;not null;default:0"`
	PendingSenderID    int64     `gorm:"column:pending_sender_id;not null;default:0"`
	CreatedByRole      int32     `gorm:"column:created_by_role;not null"`
	CreatedByID        int64     `gorm:"column:created_by_id;not null"`
	LastMessageID      int64     `gorm:"column:last_message_id;not null;default:0"`
	LastMessagePreview string    `gorm:"column:last_message_preview;type:varchar(255);not null;default:''"`
	LastMessageAt      time.Time `gorm:"column:last_message_at;not null;index"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime;index"`
}

func (Conversation) TableName() string {
	return "message_conversation"
}

type ConversationMessage struct {
	ID             int64      `gorm:"primaryKey;autoIncrement:false"`
	ConversationID int64      `gorm:"column:conversation_id;not null;index:idx_message_conversation_time,priority:1"`
	SenderID       int64      `gorm:"column:sender_id;not null"`
	SenderRole     int32      `gorm:"column:sender_role;not null"`
	RecipientID    int64      `gorm:"column:recipient_id;not null;index:idx_message_recipient_read,priority:1"`
	RecipientRole  int32      `gorm:"column:recipient_role;not null;index:idx_message_recipient_read,priority:2"`
	Content        string     `gorm:"column:content;type:text;not null"`
	IsRead         bool       `gorm:"column:is_read;not null;default:false;index:idx_message_recipient_read,priority:3"`
	ReadAt         *time.Time `gorm:"column:read_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_message_conversation_time,priority:2"`
}

func (ConversationMessage) TableName() string {
	return "message_conversation_message"
}
