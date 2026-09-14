package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

// PublicArticle pairs a still-visible article ID with its approved, immutable
// revision. Revision is nil only for an old PUBLISHED article without a
// publication pointer; that compatibility path does not invent a revision.
type PublicArticle struct {
	Article  *Article
	Revision *ArticleRevision
}

const publicArticlePredicate = `(
	(article_publication.state = 'published' AND article_publication.current_revision <> ''
	 AND EXISTS (SELECT 1 FROM article_revision
	             WHERE article_revision.article_id = article.id
	               AND article_revision.revision_id = article_publication.current_revision))
	OR (article_publication.article_id IS NULL AND article.status = 2)
)`

func publicArticleQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&Article{}).
		Joins("LEFT JOIN article_publication ON article_publication.article_id = article.id").
		Where(publicArticlePredicate)
}

func (m *ArticleRepo) FindPublicOne(ctx context.Context, id string) (PublicArticle, error) {
	var article Article
	if err := publicArticleQuery(m.Db.WithContext(ctx)).
		Where("article.id = ?", id).Select("article.*").First(&article).Error; err != nil {
		return PublicArticle{}, err
	}
	result := PublicArticle{Article: &article}
	pointer, err := m.Publication(ctx, id)
	if err == gorm.ErrRecordNotFound && article.Status == 2 {
		return result, nil
	}
	if err != nil {
		return PublicArticle{}, err
	}
	if pointer.State != "published" || pointer.CurrentRevision == "" {
		return PublicArticle{}, gorm.ErrRecordNotFound
	}
	revision, err := m.RevisionByID(ctx, id, pointer.CurrentRevision)
	if err != nil {
		return PublicArticle{}, err
	}
	result.Revision = &revision
	return result, nil
}

// ListPublic applies publication eligibility before count and pagination. A
// repeatable-read snapshot keeps the count, page, and revision pointers
// consistent when an article is edited or retracted concurrently.
func (m *ArticleRepo) ListPublic(ctx context.Context, opt ListArticlesOption) ([]PublicArticle, int64, error) {
	var result []PublicArticle
	var total int64
	err := m.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := publicArticleQuery(tx)
		if opt.ManualTypeTag != "" {
			// The approved tag is the public value, even if the source is now
			// REVIEWING with a different draft tag.
			query = query.Where(`(article_publication.state = 'published' AND EXISTS (
				SELECT 1 FROM article_revision r WHERE r.article_id = article.id
				AND r.revision_id = article_publication.current_revision AND r.manual_type_tag = ?))
				OR (article_publication.article_id IS NULL AND article.manual_type_tag = ?)`,
				opt.ManualTypeTag, opt.ManualTypeTag)
		}
		if opt.SecondaryTag != "" {
			tag, err := json.Marshal([]string{opt.SecondaryTag})
			if err != nil {
				return err
			}
			query = query.Where(`(article_publication.state = 'published' AND EXISTS (
				SELECT 1 FROM article_revision r WHERE r.article_id = article.id
				AND r.revision_id = article_publication.current_revision AND r.secondary_tags @> ?::jsonb))
				OR (article_publication.article_id IS NULL AND article.secondary_tags @> ?::jsonb)`, string(tag), string(tag))
		}
		if opt.AuthorId != "" {
			query = query.Where("article.author_id = ?", opt.AuthorId)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		page, size := opt.Page, opt.PageSize
		if page < 1 {
			page = 1
		}
		if size < 1 {
			size = 20
		} else if size > 100 {
			size = 100
		}
		var articles []*Article
		if err := query.Select("article.*").Order(publicArticleOrder(opt)).
			Offset((page - 1) * size).Limit(size).Find(&articles).Error; err != nil {
			return err
		}
		if len(articles) == 0 {
			return nil
		}
		ids := make([]string, 0, len(articles))
		for _, article := range articles {
			ids = append(ids, article.ID)
		}
		var revisions []ArticleRevision
		if err := tx.Model(&ArticleRevision{}).
			Joins("JOIN article_publication ON article_publication.article_id = article_revision.article_id AND article_publication.current_revision = article_revision.revision_id").
			Where("article_revision.article_id IN ? AND article_publication.state = 'published'", ids).
			Select("article_revision.*").Find(&revisions).Error; err != nil {
			return err
		}
		byID := make(map[string]*ArticleRevision, len(revisions))
		for i := range revisions {
			byID[revisions[i].ArticleID] = &revisions[i]
		}
		for _, article := range articles {
			result = append(result, PublicArticle{Article: article, Revision: byID[article.ID]})
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, total, err
}

func publicArticleOrder(opt ListArticlesOption) string {
	field := "article.created_at"
	switch opt.SortBy {
	case "view_count":
		field = "article.view_count"
	case "like_count":
		field = "article.like_count"
	case "comment_count":
		field = "article.comment_count"
	}
	direction := "DESC"
	if !opt.Desc {
		direction = "ASC"
	}
	return fmt.Sprintf("%s %s, article.id %s", field, direction, direction)
}

// IncrPublicViewCount atomically checks current publication eligibility. A
// withdrawal that won the race cannot gain a view or return an old result.
func (m *ArticleRepo) IncrPublicViewCount(ctx context.Context, id string) (bool, error) {
	result := m.Db.WithContext(ctx).Model(&Article{}).Where("article.id = ?", id).
		Where(`((article.status = 2 AND NOT EXISTS (SELECT 1 FROM article_publication
			WHERE article_publication.article_id = article.id)) OR EXISTS (
			SELECT 1 FROM article_publication JOIN article_revision
			ON article_revision.article_id = article_publication.article_id
			AND article_revision.revision_id = article_publication.current_revision
			WHERE article_publication.article_id = article.id AND article_publication.state = 'published'))`).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))
	return result.RowsAffected == 1, result.Error
}
