package model

import (
	"context"
	"strings"

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
	BatchUpsert(ctx context.Context, data []*LikeRecord) error
	BatchUpsertTx(ctx context.Context, tx *gorm.DB, data []*LikeRecord) error
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
	Inbox  *LikeConsumeInbox
	Record *LikeRecord
	Outbox *LikeOutboxEvent
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

func (m *defaultLikeRecordModel) BatchUpsert(ctx context.Context, data []*LikeRecord) error {
	if len(data) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "target_type"},
			{Name: "target_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"state"}),
	}).Create(&data).Error
}

func (m *defaultLikeRecordModel) BatchUpsertTx(ctx context.Context, tx *gorm.DB, data []*LikeRecord) error {
	if len(data) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "target_type"},
			{Name: "target_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"state"}),
	}).Create(&data).Error
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "duplicated")
}

func (m *defaultLikeRecordModel) ProcessLikeMessageBatch(ctx context.Context, payloads []*LikeProcessPayload) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var recordsToUpsert []*LikeRecord
		var outboxesToInsert []*LikeOutboxEvent
		var msgIDsToMarkDone []string

		for _, p := range payloads {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(p.Inbox)
			if res.Error != nil {
				return res.Error
			}

			if res.RowsAffected == 0 {
				continue
			}

			recordsToUpsert = append(recordsToUpsert, p.Record)
			if p.Outbox != nil {
				outboxesToInsert = append(outboxesToInsert, p.Outbox)
			}
			msgIDsToMarkDone = append(msgIDsToMarkDone, p.Inbox.MsgId)
		}

		if len(recordsToUpsert) == 0 {
			return nil
		}

		if err := m.BatchUpsertTx(ctx, tx, recordsToUpsert); err != nil {
			return err
		}

		if len(outboxesToInsert) > 0 {
			if err := tx.Create(&outboxesToInsert).Error; err != nil {
				return err
			}
		}

		if len(msgIDsToMarkDone) > 0 {
			if err := tx.Model(&LikeConsumeInbox{}).
				Where("msg_id IN ?", msgIDsToMarkDone).
				Update("status", 1).Error; err != nil {
				return err
			}
		}

		return nil
	})
}
