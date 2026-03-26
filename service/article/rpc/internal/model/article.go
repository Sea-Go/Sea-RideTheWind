package model

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Article GORM Model
type Article struct {
	ID            string         `gorm:"primaryKey;type:varchar(32)"`
	Title         string         `gorm:"type:varchar(255);not null"`
	Brief         string         `gorm:"type:varchar(512)"`
	Content       string         `gorm:"type:text"` // 对应 markdown_content
	CoverImageURL string         `gorm:"type:varchar(255)"`
	ManualTypeTag string         `gorm:"type:varchar(64);index"`
	SecondaryTags StringArray    `gorm:"type:jsonb"` // 使用 jsonb 存储标签数组
	AuthorID      string         `gorm:"type:varchar(32);index"`
	Status        int32          `gorm:"type:smallint;default:0"`
	ViewCount     int32          `gorm:"default:0"`
	LikeCount     int32          `gorm:"default:0"`
	CommentCount  int32          `gorm:"default:0"`
	ShareCount    int32          `gorm:"default:0"`
	ExtInfo       JSONMap        `gorm:"type:jsonb"` // 使用 jsonb 存储扩展信息
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// --- Custom Types for Postgres JSONB ---

// StringArray handles []string <-> jsonb
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(b, a)
}

// JSONMap handles map[string]string <-> jsonb
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

// --- ArticleRepo Methods ---

func (m *ArticleRepo) Insert(ctx context.Context, article *Article) error {
	return m.Db.WithContext(ctx).Create(article).Error
}

func (m *ArticleRepo) FindOne(ctx context.Context, id string) (*Article, error) {
	var article Article
	err := m.Db.WithContext(ctx).Where("id = ?", id).First(&article).Error
	if err != nil {
		return nil, err
	}
	return &article, nil
}

func (m *ArticleRepo) FindOneUnscoped(ctx context.Context, id string) (*Article, error) {
	var article Article
	err := m.Db.WithContext(ctx).Unscoped().Where("id = ?", id).First(&article).Error
	if err != nil {
		return nil, err
	}
	return &article, nil
}

func (m *ArticleRepo) Update(ctx context.Context, article *Article) error {
	return m.Db.WithContext(ctx).Save(article).Error
}

func (m *ArticleRepo) UpdateTx(ctx context.Context, tx *gorm.DB, article *Article) error {
	return tx.WithContext(ctx).Save(article).Error
}

func (m *ArticleRepo) Delete(ctx context.Context, id string) error {
	return m.Db.WithContext(ctx).Delete(&Article{}, "id = ?", id).Error
}

func (m *ArticleRepo) DeleteTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Delete(&Article{}, "id = ?", id).Error
}

func (m *ArticleRepo) RunInTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

func (m *ArticleRepo) IncrViewCount(ctx context.Context, id string) error {
	return m.Db.WithContext(ctx).Model(&Article{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + ?", 1)).Error
}

type ListArticlesOption struct {
	Page          int
	PageSize      int
	SortBy        string
	Desc          bool
	ManualTypeTag string
	SecondaryTag  string
	AuthorId      string
}

func (m *ArticleRepo) List(ctx context.Context, opt ListArticlesOption) ([]*Article, int64, error) {
	var articles []*Article
	var total int64

	db := m.Db.WithContext(ctx).Model(&Article{})

	if opt.ManualTypeTag != "" {
		db = db.Where("manual_type_tag = ?", opt.ManualTypeTag)
	}
	if opt.SecondaryTag != "" {
		db = db.Where("secondary_tags @> ?", fmt.Sprintf(`["%s"]`, opt.SecondaryTag))
	}
	if opt.AuthorId != "" {
		db = db.Where("author_id = ?", opt.AuthorId)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (opt.Page - 1) * opt.PageSize
	if offset < 0 {
		offset = 0
	}

	order := "created_at desc"
	if opt.SortBy != "" {
		switch opt.SortBy {
		case "create_time":
			order = "created_at"
		case "view_count":
			order = "view_count"
		case "like_count":
			order = "like_count"
		}
		if opt.Desc {
			order += " desc"
		} else {
			order += " asc"
		}
	}

	if err := db.Order(order).Offset(offset).Limit(opt.PageSize).Find(&articles).Error; err != nil {
		return nil, 0, err
	}

	return articles, total, nil
}
