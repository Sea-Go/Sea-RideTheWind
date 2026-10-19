package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/metrics"
	model2 "github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/model"
	"time"
)

const defaultCommentIndexTTL = 10 * time.Minute

func (c *CommentCache) GetCommentIndexCache(ctx context.Context, ids []int64, conn *model2.CommentModel) ([]model2.CommentIndex, error) {
	if c == nil || c.rdb == nil {
		return nil, fmt.Errorf("comment cache is nil")
	}
	if conn == nil {
		return nil, fmt.Errorf("comment model conn is nil")
	}
	if len(ids) == 0 {
		return []model2.CommentIndex{}, nil
	}

	validIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return []model2.CommentIndex{}, nil
	}

	keys := make([]string, 0, len(validIDs))
	for _, id := range validIDs {
		keys = append(keys, CommentIndexKey(id))
	}

	// --- 1. Redis MGET
	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		metrics.CommentRedisErrorCounterMetric.
			WithLabelValues("comment_redis", "GetCommentIndexCache", "batch_get").
			Inc()
		return nil, fmt.Errorf("redis MGet comment index cache failed: %w", err)
	}

	indexMap := make(map[int64]model2.CommentIndex, len(validIDs))
	missIDs := make([]int64, 0)

	for i, v := range values {
		id := validIDs[i]

		if v == nil {
			missIDs = append(missIDs, id)
			continue
		}

		var raw []byte
		switch vv := v.(type) {
		case string:
			raw = []byte(vv)
		case []byte:
			raw = vv
		default:
			missIDs = append(missIDs, id)
			continue
		}

		var idx model2.CommentIndex
		if err := json.Unmarshal(raw, &idx); err != nil {
			missIDs = append(missIDs, id)
			continue
		}
		indexMap[id] = idx
	}

	// --- 2. miss 回源 DB
	if len(missIDs) > 0 {
		dbIndexes, err := conn.BatchGetReplyIndexByIDs(ctx, missIDs)
		if err != nil {
			metrics.CommentRedisErrorCounterMetric.
				WithLabelValues("comment_redis", "GetCommentIndexCache", "db_fallback").
				Inc()
			return nil, fmt.Errorf("db fallback BatchGetReplyIndexByIDs failed: %w", err)
		}

		for _, idx := range dbIndexes {
			indexMap[idx.Id] = idx
		}

		// --- 3. 回填 Redis（pipeline）
		if len(dbIndexes) > 0 {
			pipe := c.rdb.Pipeline()
			for _, idx := range dbIndexes {
				b, err := json.Marshal(idx)
				if err != nil {
					continue
				}
				pipe.Set(ctx, CommentIndexKey(idx.Id), b, defaultCommentIndexTTL)
			}
			_, _ = pipe.Exec(ctx) // 回填失败不阻断主流程
		}
	}

	// --- 4. 按输入 ids 顺序输出
	result := make([]model2.CommentIndex, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if idx, ok := indexMap[id]; ok {
			result = append(result, idx)
		}
	}

	return result, nil
}
