package model

import (
	"time"

	"gorm.io/gorm"
)

type LikeRecord struct {
	ID              int64  `gorm:"primaryKey;autoIncrement;comment:主键ID"`
	UserID          int64  `gorm:"type:bigint;not null;uniqueIndex:uk_user_target,priority:1;index:idx_user_list,priority:1;comment:点赞者ID"`
	TargetType      string `gorm:"type:varchar(32);not null;uniqueIndex:uk_user_target,priority:2;index:idx_target_list,priority:1;comment:目标类型"`
	TargetID        string `gorm:"type:varchar(64);not null;uniqueIndex:uk_user_target,priority:3;index:idx_target_list,priority:2;comment:目标ID"`
	AuthorID        int64  `gorm:"type:bigint;not null;index:idx_author;comment:被点赞内容作者ID"`
	State           int32  `gorm:"type:smallint;not null;default:0;comment:状态(0表示无,1表示已点赞,2表示已点踩)"`
	LastOperationID int64  `gorm:"column:last_operation_id;type:bigint;not null;default:0;comment:最后接纳的雪花消息ID"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null;comment:首次操作时间"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null;index:idx_user_list,priority:2;comment:最后操作时间"`
	DeleteAt  gorm.DeletedAt `gorm:"index;comment:GORM软删除时间"`
}

func (LikeRecord) TableName() string {
	return "like_record"
}

//uniqueIndex是唯一索引,目的是防止重名,在这里的顺序是(UserID,TargetType,TargetID),防止重复

type LikeConsumeInbox struct {
	MsgId       string    `gorm:"primaryKey;type:varchar(64);comment:消息唯一ID"`
	PayloadHash string    `gorm:"column:payload_hash;type:varchar(64);not null;default:'';comment:消息语义哈希"`
	Topic       string    `gorm:"type:varchar(64);not null;index:idx_topic_consumer;comment:topic"`
	Consumer    string    `gorm:"type:varchar(64);not null;index:idx_topic_consumer;comment:消费者名称"`
	Status      int32     `gorm:"type:smallint;not null;default:0;comment:0处理 1已完成"`
	CreatedAt   time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;not null"`
}

func (LikeConsumeInbox) TableName() string {
	return "like_consume_inbox"
}

type LikeOutboxEvent struct {
	EventID     string    `gorm:"primaryKey;type:varchar(64);comment:事件ID"`
	EventKey    string    `gorm:"type:varchar(128);not null;uniqueIndex:uk_event_key;comment:业务幂等键"`
	EventType   string    `gorm:"type:varchar(64);not null;index;comment:事件类型"`
	AggregateID string    `gorm:"type:varchar(64);not null;index;comment:聚合ID,比如文章ID"`
	Payload     string    `gorm:"type:json;not null;comment:事件载荷"`
	Status      int32     `gorm:"type:smallint;not null;default:0;index;comment:0待发送 1已发送 2发送失败"`
	RetryCount  int32     `gorm:"type:int;not null;default:0;comment:重试次数"`
	CreatedAt   time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;not null"`
}

func (LikeOutboxEvent) TableName() string {
	return "like_outbox_event"
}
