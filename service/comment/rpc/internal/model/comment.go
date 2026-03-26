package model

import (
	"context"
	"encoding/json"
	"fmt"
	kqtypes "sea-try-go/service/comment/rpc/common/types"
	"sea-try-go/service/comment/rpc/internal/metrics"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CommentModel struct {
	conn *gorm.DB
}

func NewCommentModel(db *gorm.DB) *CommentModel {
	return &CommentModel{
		conn: db,
	}
}

func (m *CommentModel) InsertCommentTx(ctx context.Context, msg kqtypes.CommentKafkaMsg, status int) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		// 1. 检查重复
		var existCount int64
		if err := tx.Model(&CommentIndex{}).Where("id = ?", msg.CommentId).Count(&existCount).Error; err != nil {
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "InsertCommentTx", "query").
				Inc()
			return err
		}
		if existCount > 0 {
			return nil
		}

		createTime := time.Unix(msg.CreateTime, 0)
		meta := normalizeCommentMeta(msg.Meta)

		// 2. 插入 CommentContent
		content := &CommentContent{
			CommentId: msg.CommentId,
			Content:   msg.Content,
			Meta:      meta,
			CreatedAt: createTime,
		}
		if err := tx.Create(content).Error; err != nil {
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "InsertCommentTx", "insert").
				Inc()
			return err
		}

		// 3. 插入 CommentIndex
		index := &CommentIndex{
			Id:         msg.CommentId,
			TargetType: msg.TargetType,
			TargetId:   msg.TargetId,
			UserId:     msg.UserId,
			RootId:     msg.RootId,
			ParentId:   msg.ParentId,
			State:      int32(status),
			Attribute:  msg.Attribute,
			CreatedAt:  createTime,
		}
		if err := tx.Create(index).Error; err != nil {
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "InsertCommentTx", "insert").
				Inc()
			return err
		}

		// 4. 更新 Subject
		newSubject := &Subject{
			TargetType: msg.TargetType,
			TargetId:   msg.TargetId,
			TotalCount: 1,
			RootCount:  0,
			State:      0,
			Attribute:  0,
			OwnerId:    msg.OwnerId,
		}
		if msg.RootId == 0 {
			newSubject.RootCount = 1
		}
		updateCols := map[string]interface{}{"total_count": gorm.Expr(`"subject"."total_count" + 1`)}
		if msg.RootId == 0 {
			updateCols["root_count"] = gorm.Expr(`"subject"."root_count" + 1`)
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "target_type"}, {Name: "target_id"}},
			DoUpdates: clause.Assignments(updateCols),
		}).Create(&newSubject).Error; err != nil {
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "InsertCommentTx", "upsert").
				Inc()
			return err
		}

		// 5. 更新父评论回复数
		if msg.ParentId != 0 {
			updateCols := map[string]interface{}{"reply_count": gorm.Expr("reply_count + 1")}
			if msg.UserId == msg.OwnerId {
				updateCols["attribute"] = gorm.Expr("attribute | ?", (1 << 1))
			}
			db := tx.Model(&CommentIndex{}).Where("id = ?", msg.ParentId).Updates(updateCols)
			if db.Error != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "InsertCommentTx", "update").
					Inc()
				return db.Error
			}
			if db.RowsAffected == 0 {
				return ErrorCommentNotFound
			}
		}

		return nil
	})
}

func (m *CommentModel) ManageCommentAttribute(ctx context.Context, commentId int64, bitOffset uint, isSet bool) error {
	val := (1 << bitOffset)
	var expr clause.Expr
	if isSet {
		expr = gorm.Expr("attribute | ?", val)
	} else {
		expr = gorm.Expr("attribute & ~?", val)
	}

	db := m.conn.WithContext(ctx).Model(&CommentIndex{}).
		Where("id = ?", commentId).
		Update("attribute", expr)

	if db.Error != nil {
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "ManageCommentAttribute", "update").
			Inc()
		return db.Error
	}

	if db.RowsAffected == 0 {
		return ErrorCommentNotFound
	}

	return nil
}

func (m *CommentModel) UpdateSubjectState(ctx context.Context, targetType, targetId string, state int32) error {
	db := m.conn.WithContext(ctx).Model(&Subject{}).
		Where("target_type = ? AND target_id = ?", targetType, targetId).
		Update("state", state)

	if db.Error != nil {
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "UpdateSubjectState", "update").
			Inc()
		return db.Error
	}

	if db.RowsAffected == 0 {
		return ErrorSubjectNotFound
	}

	return nil
}

func (m *CommentModel) GetOwnerId(ctx context.Context, targetType, targetId string) (ownerId int, err error) {
	db := m.conn.WithContext(ctx).Model(&Subject{}).
		Where("target_type = ? AND target_id = ?", targetType, targetId).
		Select("owner_id").Scan(&ownerId)

	if db.Error != nil {
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "GetOwnerId", "query").
			Inc()
		return 0, db.Error
	}

	if db.RowsAffected == 0 {
		return 0, ErrorSubjectNotFound
	}

	return ownerId, nil
}

func normalizeCommentMeta(meta string) string {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return "{}"
	}

	var payload any
	if err := json.Unmarshal([]byte(meta), &payload); err == nil {
		return meta
	}

	fallback, err := json.Marshal(map[string]string{
		"raw": meta,
	})
	if err != nil {
		return "{}"
	}
	return string(fallback)
}

func (m *CommentModel) InsertReport(ctx context.Context, report *ReportRecord) error {
	err := m.conn.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(report).Error
	if err != nil {
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "InsertReport", "insert").
			Inc()
	}
	return err
}

func (m *CommentModel) DeleteCommentTx(ctx context.Context, commentId, userId int64, targetType, targetId string) (int64, error) {
	var remainCount int64
	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		// 查评论
		var comment CommentIndex
		if err := tx.Where("id = ?", commentId).First(&comment).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorCommentNotFound
			}
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "DeleteCommentTx", "query").
				Inc()
			return err
		}

		// 查 Subject
		var sub Subject
		if err := tx.Where("target_type = ? AND target_id = ?", targetType, targetId).First(&sub).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorSubjectNotFound
			}
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "DeleteCommentTx", "query").
				Inc()
			return err
		}

		// 删除逻辑
		if comment.State != 2 {
			if err := tx.Model(&CommentIndex{}).Where("id = ?", commentId).Update("state", 2).Error; err != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "DeleteCommentTx", "update").
					Inc()
				return err
			}

			updateSub := map[string]interface{}{"total_count": gorm.Expr("total_count - 1")}
			if comment.RootId == 0 {
				updateSub["root_count"] = gorm.Expr("root_count - 1")
			}
			if err := tx.Model(&Subject{}).Where("target_type = ? AND target_id = ?", targetType, targetId).Updates(updateSub).Error; err != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "DeleteCommentTx", "update").
					Inc()
				return err
			}
		}

		// 更新父评论回复数
		if comment.ParentId != 0 {
			db := tx.Model(&CommentIndex{}).Where("id = ?", comment.ParentId).Update("reply_count", gorm.Expr("reply_count - 1"))
			if db.Error != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "DeleteCommentTx", "update").
					Inc()
				return db.Error
			}
			if db.RowsAffected == 0 {
				return ErrorCommentNotFound
			}
		}

		// 返回剩余总数
		tx.Where("target_type = ? AND target_id = ?", targetType, targetId).First(&sub)
		remainCount = sub.TotalCount

		return nil
	})

	return remainCount, err
}

func (m *CommentModel) GetReplyIDsByPage(ctx context.Context, req GetReplyIDsPageReq) ([]int64, error) {
	if req.TargetType == "" {
		return nil, fmt.Errorf("invalid TargetType: empty")
	}
	if req.TargetId == "" {
		return nil, fmt.Errorf("invalid TargetId: empty")
	}
	if req.RootId < 0 {
		return nil, fmt.Errorf("invalid RootId: %d", req.RootId)
	}
	if req.Offset < 0 {
		return nil, fmt.Errorf("invalid Offset: %d", req.Offset)
	}
	if req.Limit <= 0 {
		return nil, fmt.Errorf("invalid Limit: %d", req.Limit)
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	sort := req.Sort
	if sort == "" {
		sort = ReplySortTime
	}

	db := m.conn.WithContext(ctx).
		Model(&CommentIndex{}).
		Select("id").
		Where("target_type = ? AND target_id = ?", req.TargetType, req.TargetId).
		Where("root_id = ?", req.RootId)

	// 只查正常状态（0=正常）
	if req.OnlyNormal {
		db = db.Where("state = ?", 0)
	}

	// 排序：
	switch sort {
	case ReplySortHot:
		db = db.Order("like_count DESC").Order("id DESC")
	case ReplySortTime:
		fallthrough
	default:
		db = db.Order("created_at DESC").Order("id DESC")
	}

	db = db.Offset(req.Offset).Limit(req.Limit)

	var rows []struct {
		Id int64 `gorm:"column:id"`
	}
	if err := db.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("GetReplyIDsByPage query failed: %w", err)
	}

	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Id)
	}

	return ids, nil
}

/*func (m *CommentModel) GetReplyContent(ctx context.Context, commentId int64, bitOffset uint, isSet bool) (CommentContent, error) {

}*/

func (m *CommentModel) BatchGetReplyContent(ctx context.Context, commentIDs []int64) ([]CommentContent, error) {
	if len(commentIDs) == 0 {
		return []CommentContent{}, nil
	}

	//去掉非法ID
	uniq := make(map[int64]struct{}, len(commentIDs))
	filteredIDs := make([]int64, 0, len(commentIDs))
	for _, id := range commentIDs {
		if id <= 0 {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		filteredIDs = append(filteredIDs, id)
	}

	if len(filteredIDs) == 0 {
		return []CommentContent{}, nil
	}

	var rows []CommentContent
	err := m.conn.WithContext(ctx).
		Model(&CommentContent{}).
		Where("comment_id IN ?", filteredIDs).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("BatchGetReplyContentByCommentIDs query failed: %w", err)
	}

	//便于按输入顺序重排
	contentMap := make(map[int64]CommentContent, len(rows))
	for _, row := range rows {
		contentMap[row.CommentId] = row
	}

	result := make([]CommentContent, 0, len(commentIDs))
	for _, id := range commentIDs {
		if id <= 0 {
			continue
		}
		if c, ok := contentMap[id]; ok {
			result = append(result, c)
		}
	}
	return result, nil
}

func (m *CommentModel) LikeCommentTx(ctx context.Context, userId, commentId int64, targetType, targetId string, actionType int32, ownerId int64) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var likeRecord CommentLike
		var needInsert bool

		// 悲观锁查询
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND comment_id = ?", userId, commentId).First(&likeRecord).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				needInsert = true
				likeRecord = CommentLike{
					UserId:     userId,
					CommentId:  commentId,
					TargetType: targetType,
					TargetId:   targetId,
					State:      0,
				}
			} else {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "LikeCommentTx", "lock_query").
					Inc()
				return err
			}
		}

		// 查评论
		var comment CommentIndex
		if err := tx.Model(&CommentIndex{}).Where("id = ?", commentId).First(&comment).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorCommentNotFound
			}
			metrics.CommentPostgresErrorCounterMetric.
				WithLabelValues("comment_postgres", "LikeCommentTx", "query").
				Inc()
			return err
		}

		// 计算状态差值
		oldState := likeRecord.State
		var newState int32
		var likeDiff, dislikeDiff int64
		switch actionType {
		case 1:
			if oldState == 1 {
				return nil
			}
			newState = 1
			likeDiff = 1
			if oldState == 2 {
				dislikeDiff = -1
			}
		case 2:
			if oldState != 1 {
				return nil
			}
			newState = 0
			likeDiff = -1
		case 3:
			if oldState == 2 {
				return nil
			}
			newState = 2
			dislikeDiff = 1
			if oldState == 1 {
				likeDiff = -1
			}
		case 4:
			if oldState != 2 {
				return nil
			}
			newState = 0
			dislikeDiff = -1
		default:
			return fmt.Errorf("未知的操作类型")
		}

		// 更新 likeRecord
		likeRecord.State = newState
		if needInsert {
			if err := tx.Create(&likeRecord).Error; err != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "LikeCommentTx", "insert").
					Inc()
				return err
			}
		} else {
			if err := tx.Model(&likeRecord).Update("state", newState).Error; err != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "LikeCommentTx", "update").
					Inc()
				return err
			}
		}

		// 更新 CommentIndex
		updateCols := make(map[string]interface{})
		if likeDiff != 0 {
			updateCols["like_count"] = gorm.Expr("like_count + ?", likeDiff)
		}
		if dislikeDiff != 0 {
			updateCols["dislike_count"] = gorm.Expr("dislike_count + ?", dislikeDiff)
		}
		if userId == ownerId {
			if newState == 1 {
				updateCols["attribute"] = gorm.Expr("attribute | ?", 1)
			} else {
				updateCols["attribute"] = gorm.Expr("attribute & ~?", 1)
			}
		}

		if len(updateCols) > 0 {
			if err := tx.Model(&CommentIndex{}).Where("id = ?", commentId).Updates(updateCols).Error; err != nil {
				metrics.CommentPostgresErrorCounterMetric.
					WithLabelValues("comment_postgres", "LikeCommentTx", "update").
					Inc()
				return err
			}
		}

		return nil
	})
}

func (m *CommentModel) BatchGetReplyIndexByIDs(ctx context.Context, ids []int64) ([]CommentIndex, error) {
	if len(ids) == 0 {
		return []CommentIndex{}, nil
	}

	uniq := make(map[int64]struct{}, len(ids))
	filtered := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		filtered = append(filtered, id)
	}
	if len(filtered) == 0 {
		return []CommentIndex{}, nil
	}

	var list []CommentIndex
	err := m.conn.WithContext(ctx).
		Model(&CommentIndex{}).
		Where("id IN ?", filtered).
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("BatchGetReplyIndexByIDs query failed: %w", err)
	}

	return list, nil
}

func (m *CommentModel) FindOneCommentById(ctx context.Context, commentId int64) (CommentIndex, error) {
	var comment CommentIndex
	err := m.conn.WithContext(ctx).Model(&CommentIndex{}).
		Where("id = ?", commentId).
		First(&comment).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return CommentIndex{}, ErrorCommentNotFound
		}
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "FindOneCommentById", "query").
			Inc()
		return CommentIndex{}, err
	}

	return comment, nil
}

func (m *CommentModel) FindOneSubjectByTarget(ctx context.Context, targetType, targetId string) (Subject, error) {
	var subject Subject
	err := m.conn.WithContext(ctx).Model(&Subject{}).
		Where("target_type = ? AND target_id = ?", targetType, targetId).
		First(&subject).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return Subject{}, ErrorSubjectNotFound
		}
		metrics.CommentPostgresErrorCounterMetric.
			WithLabelValues("comment_postgres", "FindOneSubjectByTarget", "query").
			Inc()
		return Subject{}, err
	}

	return subject, nil
}
