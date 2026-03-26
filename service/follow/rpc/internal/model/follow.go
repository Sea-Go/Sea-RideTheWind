package model

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FollowModel struct {
	conn *gorm.DB
}

func NewFollowModel(db *gorm.DB) *FollowModel {
	return &FollowModel{conn: db}
}

func (m *FollowModel) CreateFollow(ctx context.Context, userID, targetID int64) error {
	return m.conn.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "target_id"}},
			DoNothing: true,
		}).
		Create(&FollowRelation{UserId: userID, TargetId: targetID}).Error
}

func (m *FollowModel) DeleteFollow(ctx context.Context, userID, targetID int64) error {
	return m.conn.WithContext(ctx).
		Where("user_id = ? AND target_id = ?", userID, targetID).
		Delete(&FollowRelation{}).Error
}

func (m *FollowModel) CreateBlockAndCleanup(ctx context.Context, userID, targetID int64) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "target_id"}},
				DoNothing: true,
			}).
			Create(&BlockRelation{UserId: userID, TargetId: targetID}).Error; err != nil {
			return err
		}

		return tx.
			Where(
				"(user_id = ? AND target_id = ?) OR (user_id = ? AND target_id = ?)",
				userID,
				targetID,
				targetID,
				userID,
			).
			Delete(&FollowRelation{}).Error
	})
}

func (m *FollowModel) DeleteBlock(ctx context.Context, userID, targetID int64) error {
	return m.conn.WithContext(ctx).
		Where("user_id = ? AND target_id = ?", userID, targetID).
		Delete(&BlockRelation{}).Error
}

func (m *FollowModel) ExistsAnyBlock(ctx context.Context, userID, targetID int64) (bool, error) {
	var count int64
	err := m.conn.WithContext(ctx).
		Model(&BlockRelation{}).
		Where(
			"(user_id = ? AND target_id = ?) OR (user_id = ? AND target_id = ?)",
			userID,
			targetID,
			targetID,
			userID,
		).
		Count(&count).Error
	return count > 0, err
}

func (m *FollowModel) ListFollowTargets(ctx context.Context, userID int64, offset, limit int32) ([]int64, error) {
	var relations []FollowRelation
	query := m.conn.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time desc")
	if offset > 0 {
		query = query.Offset(int(offset))
	}
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if err := query.Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.TargetId)
	}
	return userIDs, nil
}

func (m *FollowModel) ListFollowerUsers(ctx context.Context, userID int64, offset, limit int32) ([]int64, error) {
	var relations []FollowRelation
	query := m.conn.WithContext(ctx).
		Where("target_id = ?", userID).
		Order("create_time desc")
	if offset > 0 {
		query = query.Offset(int(offset))
	}
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if err := query.Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.UserId)
	}
	return userIDs, nil
}

func (m *FollowModel) ListBlockTargets(ctx context.Context, userID int64, offset, limit int32) ([]int64, error) {
	var relations []BlockRelation
	query := m.conn.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time desc")
	if offset > 0 {
		query = query.Offset(int(offset))
	}
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if err := query.Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.TargetId)
	}
	return userIDs, nil
}

func (m *FollowModel) ListAllFollowTargets(ctx context.Context, userID int64, limit int) ([]int64, error) {
	var relations []FollowRelation
	query := m.conn.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time desc")
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.TargetId)
	}
	return userIDs, nil
}

func (m *FollowModel) ListAllBlockTargets(ctx context.Context, userID int64) ([]int64, error) {
	var relations []BlockRelation
	if err := m.conn.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time desc").
		Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.TargetId)
	}
	return userIDs, nil
}

func (m *FollowModel) ListBlockerUsers(ctx context.Context, userID int64) ([]int64, error) {
	var relations []BlockRelation
	if err := m.conn.WithContext(ctx).
		Where("target_id = ?", userID).
		Order("create_time desc").
		Find(&relations).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		userIDs = append(userIDs, relation.UserId)
	}
	return userIDs, nil
}
