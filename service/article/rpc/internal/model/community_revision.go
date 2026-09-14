package model

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ArticleRevision is an immutable approved community article body. Its ID is
// separate from the mutable article row and from the knowledge-module release.
type ArticleRevision struct {
	ArticleID     string      `gorm:"primaryKey;type:varchar(32)"`
	Revision      int64       `gorm:"primaryKey"`
	RevisionID    string      `gorm:"type:varchar(96);not null;uniqueIndex"`
	AuthorID      string      `gorm:"type:varchar(32);not null"`
	SourceObject  string      `gorm:"type:text;not null"`
	ContentSHA256 string      `gorm:"type:char(64);not null"`
	Title         string      `gorm:"type:varchar(255);not null"`
	Brief         string      `gorm:"type:varchar(512)"`
	CoverImageURL string      `gorm:"type:varchar(255)"`
	ManualTypeTag string      `gorm:"type:varchar(64)"`
	SecondaryTags StringArray `gorm:"type:jsonb"`
	Markdown      string      `gorm:"type:text;not null"`
	PublishedAt   time.Time   `gorm:"not null"`
	SyncEventID   string      `gorm:"type:varchar(64);not null;uniqueIndex"`
}

func (ArticleRevision) TableName() string { return "article_revision" }

// ArticlePublication is the source-domain pointer. A new review never changes
// it until its own result is accepted; retract advances pointer version.
type ArticlePublication struct {
	ArticleID       string    `gorm:"primaryKey;type:varchar(32)"`
	CurrentRevision string    `gorm:"type:varchar(96)"`
	PointerVersion  int64     `gorm:"not null"`
	State           string    `gorm:"type:varchar(16);not null"`
	LastEventID     string    `gorm:"type:varchar(128);not null"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime;not null"`
}

func (ArticlePublication) TableName() string { return "article_publication" }

type ArticleDomainOutbox struct {
	EventID          string    `gorm:"primaryKey;type:varchar(128)"`
	AggregateID      string    `gorm:"type:varchar(32);not null;uniqueIndex:uk_article_domain_version"`
	AggregateVersion int64     `gorm:"not null;uniqueIndex:uk_article_domain_version"`
	EventType        string    `gorm:"type:varchar(64);not null"`
	Payload          string    `gorm:"type:jsonb;not null"`
	Status           int32     `gorm:"type:smallint;not null;default:0;index"`
	RetryCount       int32     `gorm:"not null;default:0"`
	CreatedAt        time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime;not null"`
}

func (ArticleDomainOutbox) TableName() string { return "article_domain_outbox" }

func (m *ArticleRepo) RevisionByID(ctx context.Context, articleID, revisionID string) (ArticleRevision, error) {
	var revision ArticleRevision
	err := m.Db.WithContext(ctx).Where("article_id = ? AND revision_id = ?", articleID, revisionID).First(&revision).Error
	return revision, err
}

func (m *ArticleRepo) Publication(ctx context.Context, articleID string) (ArticlePublication, error) {
	var pointer ArticlePublication
	err := m.Db.WithContext(ctx).Where("article_id = ?", articleID).First(&pointer).Error
	return pointer, err
}

func (m *ArticleRepo) DomainOutboxPending(ctx context.Context, limit int) ([]ArticleDomainOutbox, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	var rows []ArticleDomainOutbox
	err := m.Db.WithContext(ctx).Where("status IN ?", []int32{0, 2}).
		Order("created_at,event_id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (m *ArticleRepo) WithArticleTx(ctx context.Context, articleID string, fn func(*gorm.DB, *Article) error) error {
	return m.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var article Article
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", articleID).First(&article).Error; err != nil {
			return err
		}
		return fn(tx, &article)
	})
}
