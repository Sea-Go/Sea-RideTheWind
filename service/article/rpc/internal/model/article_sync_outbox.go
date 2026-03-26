package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const (
	ArticleSyncOutboxStatusPending = 0
	ArticleSyncOutboxStatusSent    = 1
	ArticleSyncOutboxStatusFailed  = 2
)

type ArticleSyncOutboxEvent struct {
	EventID     string    `gorm:"primaryKey;type:varchar(64)"`
	EventKey    string    `gorm:"type:varchar(128);not null;uniqueIndex:uk_article_sync_outbox_event_key"`
	EventType   string    `gorm:"type:varchar(64);not null;index"`
	AggregateID string    `gorm:"type:varchar(64);not null;index"`
	Payload     string    `gorm:"type:jsonb;not null"`
	Status      int32     `gorm:"type:smallint;not null;default:0;index"`
	RetryCount  int32     `gorm:"type:int;not null;default:0"`
	LastError   string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;not null"`
}

func (ArticleSyncOutboxEvent) TableName() string {
	return "article_sync_outbox"
}

type ArticleSyncOutboxModel interface {
	CreateTx(ctx context.Context, tx *gorm.DB, data *ArticleSyncOutboxEvent) error
	FetchPending(ctx context.Context, limit int, maxRetry int) ([]*ArticleSyncOutboxEvent, error)
	MarkSent(ctx context.Context, eventID string) error
	MarkFailed(ctx context.Context, eventID string, lastError string) error
}

type defaultArticleSyncOutboxModel struct {
	db *gorm.DB
}

func NewArticleSyncOutboxModel(db *gorm.DB) ArticleSyncOutboxModel {
	return &defaultArticleSyncOutboxModel{db: db}
}

func (m *defaultArticleSyncOutboxModel) CreateTx(ctx context.Context, tx *gorm.DB, data *ArticleSyncOutboxEvent) error {
	return tx.WithContext(ctx).Create(data).Error
}

func (m *defaultArticleSyncOutboxModel) FetchPending(ctx context.Context, limit int, maxRetry int) ([]*ArticleSyncOutboxEvent, error) {
	var res []*ArticleSyncOutboxEvent
	query := m.db.WithContext(ctx).
		Where("status IN (?, ?)", ArticleSyncOutboxStatusPending, ArticleSyncOutboxStatusFailed)
	if maxRetry > 0 {
		query = query.Where("retry_count < ?", maxRetry)
	}
	err := query.Order("created_at asc").Limit(limit).Find(&res).Error
	return res, err
}

func (m *defaultArticleSyncOutboxModel) MarkSent(ctx context.Context, eventID string) error {
	return m.db.WithContext(ctx).
		Model(&ArticleSyncOutboxEvent{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":     ArticleSyncOutboxStatusSent,
			"last_error": "",
		}).Error
}

func (m *defaultArticleSyncOutboxModel) MarkFailed(ctx context.Context, eventID string, lastError string) error {
	return m.db.WithContext(ctx).
		Model(&ArticleSyncOutboxEvent{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":      ArticleSyncOutboxStatusFailed,
			"retry_count": gorm.Expr("retry_count + 1"),
			"last_error":  lastError,
		}).Error
}
