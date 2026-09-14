package model

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LikeRecordModel interface {
	GetTotalLikeCount(ctx context.Context, authorId int64) (int64, error)
	GetUserActiveLikeCount(ctx context.Context, userId int64, targetType string) (int64, error)
	GetTargetActiveLikeCount(ctx context.Context, targetType string, targetId string) (int64, error)
	GetBatchLikeCount(ctx context.Context, targetType string, targetIds []string) (map[string]map[int32]int64, error)
	GetUserBatchLikeState(ctx context.Context, userId int64, targetType string, targetIds []string) (map[string]int32, error)
	GetUserLikeList(ctx context.Context, userId int64, targetType string, cursor int64, limit int) ([]UserLikeListResult, error)
	GetTargetLikerList(ctx context.Context, targetType string, targetId string, cursor int64, limit int64) ([]TargetLikerListResult, error)
	ProcessLikeMessageBatch(ctx context.Context, payloads []*LikeProcessPayload) error
}

type TargetLikerListResult struct {
	Id         int64
	UserId     int64
	CreateTime int64
}

type defaultLikeRecordModel struct {
	db *gorm.DB
}

func NewLikeRecordModel(db *gorm.DB) LikeRecordModel {
	return &defaultLikeRecordModel{db: db}
}

type LikeProcessPayload struct {
	Inbox      *LikeConsumeInbox
	Record     *LikeRecord
	Outbox     *LikeOutboxEvent
	OccurredAt int64
	IsFirst    bool
}

func (m *defaultLikeRecordModel) GetTotalLikeCount(ctx context.Context, authorId int64) (int64, error) {
	var count int64
	err := m.db.WithContext(ctx).Model(&LikeRecord{}).Where("author_id = ? AND state = ?", authorId, 1).Count(&count).Error
	return count, err
}

func (m *defaultLikeRecordModel) GetUserActiveLikeCount(ctx context.Context, userId int64, targetType string) (int64, error) {
	var count int64
	err := m.db.WithContext(ctx).
		Model(&LikeRecord{}).
		Where("user_id = ? AND target_type = ? AND state = ?", userId, targetType, 1).
		Count(&count).Error
	return count, err
}

func (m *defaultLikeRecordModel) GetTargetActiveLikeCount(ctx context.Context, targetType string, targetId string) (int64, error) {
	var count int64
	err := m.db.WithContext(ctx).
		Model(&LikeRecord{}).
		Where("target_type = ? AND target_id = ? AND state = ?", targetType, targetId, 1).
		Count(&count).Error
	return count, err
}

func (m *defaultLikeRecordModel) GetBatchLikeCount(ctx context.Context, targetType string, targetIds []string) (map[string]map[int32]int64, error) {
	type Result struct {
		TargetID string
		State    int32
		Count    int64
	}
	var results []Result
	err := m.db.WithContext(ctx).Model(&LikeRecord{}).
		Select("target_id,state,count(1) as count").
		Where("target_type = ? AND target_id IN (?) AND state IN (1,2)", targetType, targetIds).
		Group("target_id,state").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}
	resMap := make(map[string]map[int32]int64)
	for _, r := range results {
		if resMap[r.TargetID] == nil {
			resMap[r.TargetID] = make(map[int32]int64)
		}
		resMap[r.TargetID][r.State] = r.Count
	}
	return resMap, nil
}

func (m *defaultLikeRecordModel) GetUserBatchLikeState(ctx context.Context, userId int64, targetType string, targetIds []string) (map[string]int32, error) {
	type Result struct {
		TargetID string
		State    int32
	}
	var results []Result
	err := m.db.WithContext(ctx).Model(&LikeRecord{}).
		Select("target_id, state").
		Where("user_id = ? AND target_type = ? AND target_id IN (?)", userId, targetType, targetIds).
		Scan(&results).Error
	if err != nil {
		return nil, err
	}
	resMap := make(map[string]int32)
	for _, r := range results {
		resMap[r.TargetID] = r.State
	}
	return resMap, nil
}

type UserLikeListResult struct {
	Id         int64
	TargetType string
	TargetId   string
	CreateTime int64
}

func (m *defaultLikeRecordModel) GetUserLikeList(ctx context.Context, userId int64, targetType string, cursor int64, limit int) ([]UserLikeListResult, error) {
	var results []UserLikeListResult
	//ID越大表示时间越新,只需要 id < cursor ,然后结合Limit就能取出最接近cursor的limit条数据
	query := m.db.WithContext(ctx).Model(&LikeRecord{}).Where("user_id = ? AND target_type = ? AND state = 1", userId, targetType)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	err := query.Order("id DESC").
		Limit(limit).
		Select("id, target_type,target_id, created_at").
		Scan(&results).Error
	return results, err
}

func (m *defaultLikeRecordModel) GetTargetLikerList(ctx context.Context, targetType string, targetId string, cursor int64, limit int64) ([]TargetLikerListResult, error) {
	var results []TargetLikerListResult
	query := m.db.WithContext(ctx).Model(&LikeRecord{}).Where("target_type = ? AND target_id = ? AND state = 1", targetType, targetId)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	err := query.Order("id DESC").
		Limit(int(limit)).
		Select("id, user_id, created_at").
		Scan(&results).Error
	return results, err
}

func (m *defaultLikeRecordModel) ProcessLikeMessageBatch(ctx context.Context, payloads []*LikeProcessPayload) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, p := range payloads {
			if p == nil || p.Inbox == nil || p.Record == nil || p.Record.UserID <= 0 ||
				p.Record.TargetType == "" || p.Record.TargetID == "" || p.Record.State < 1 || p.Record.State > 4 {
				return fmt.Errorf("invalid like message payload")
			}
			operationID, err := strconv.ParseInt(p.Inbox.MsgId, 10, 64)
			if err != nil || operationID <= 0 {
				return fmt.Errorf("invalid like operation id: %q", p.Inbox.MsgId)
			}
			payloadHash, err := likeMessageHash(p)
			if err != nil {
				return err
			}
			p.Inbox.PayloadHash = payloadHash
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(p.Inbox)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				var prior LikeConsumeInbox
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("msg_id = ?", p.Inbox.MsgId).First(&prior).Error; err != nil {
					return err
				}
				if prior.PayloadHash != "" && prior.PayloadHash != payloadHash {
					return fmt.Errorf("like message %s reused with conflicting payload", p.Inbox.MsgId)
				}
				if prior.Status == 1 {
					continue
				}
			}
			if err := m.applyLikeMessage(tx, p, operationID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *defaultLikeRecordModel) applyLikeMessage(tx *gorm.DB, p *LikeProcessPayload, operationID int64) error {
	seed := LikeRecord{UserID: p.Record.UserID, TargetType: p.Record.TargetType,
		TargetID: p.Record.TargetID, AuthorID: p.Record.AuthorID, State: 0}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return err
	}
	var current LikeRecord
	if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND target_type = ? AND target_id = ?", p.Record.UserID, p.Record.TargetType, p.Record.TargetID).
		First(&current).Error; err != nil {
		return err
	}
	if current.DeleteAt.Valid {
		return fmt.Errorf("like record is soft deleted")
	}
	if current.State < 0 || current.State > 2 || (current.State == 2 && current.LastOperationID == 0) {
		return fmt.Errorf("legacy like state %d requires reconciliation before fact emission", current.State)
	}
	if operationID <= current.LastOperationID {
		return markLikeMessageDone(tx, p.Inbox.MsgId)
	}
	oldState := current.State
	newState := oldState
	switch p.Record.State {
	case 1: // like
		newState = 1
	case 2: // unlike
		if oldState == 1 {
			newState = 0
		}
	case 3: // dislike
		newState = 2
	case 4: // undislike
		if oldState == 2 {
			newState = 0
		}
	}
	if err := tx.Model(&current).Updates(map[string]any{
		"state": newState, "last_operation_id": operationID, "author_id": p.Record.AuthorID,
	}).Error; err != nil {
		return err
	}
	if newState != oldState {
		if err := appendLikeFact(tx, p, oldState, newState); err != nil {
			return err
		}
		if p.Outbox != nil && oldState == 0 && newState == 1 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(p.Outbox).Error; err != nil {
				return err
			}
		}
	}
	return markLikeMessageDone(tx, p.Inbox.MsgId)
}

func markLikeMessageDone(tx *gorm.DB, msgID string) error {
	return tx.Model(&LikeConsumeInbox{}).Where("msg_id = ?", msgID).Update("status", 1).Error
}
