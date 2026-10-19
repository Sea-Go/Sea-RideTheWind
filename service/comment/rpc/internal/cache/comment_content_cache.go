package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/metrics"
	model2 "github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/model"
	"time"
)

const defaultReplyContentTTL = 24 * time.Hour

func (cache *CommentCache) BatchGetContentCache(ctx context.Context, ids []int64, conn *model2.CommentModel) ([]model2.CommentContent, error) {
	if cache == nil || cache.rdb == nil {
		return nil, fmt.Errorf("comment cache is nil")
	}
	if conn == nil {
		return nil, fmt.Errorf("comment model conn is nil")
	}
	if len(ids) == 0 {
		return []model2.CommentContent{}, nil
	}

	// 过滤ID
	validIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return []model2.CommentContent{}, nil
	}

	keys := make([]string, 0, len(validIDs))
	for _, id := range validIDs {
		keys = append(keys, ReplyContentKey(id))
	}

	values, err := cache.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		// Redis 错误埋点
		metrics.CommentRedisErrorCounterMetric.
			WithLabelValues("comment_redis", "BatchGetContentCache", "batch_get").
			Inc()
		return nil, fmt.Errorf("redis MGet content cache failed: %w", err)
	}

	contentMap := make(map[int64]model2.CommentContent, len(validIDs))
	missIDs := make([]int64, 0)

	// 解析 MGET 结果
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

		var c model2.CommentContent
		if err := json.Unmarshal(raw, &c); err != nil {
			missIDs = append(missIDs, id)
			continue
		}
		contentMap[id] = c
	}

	// miss 回源 DB
	if len(missIDs) > 0 {
		dbContents, err := conn.BatchGetReplyContent(ctx, missIDs)
		if err != nil {
			// DB 回源失败也可以算作 Redis fallback 错误
			metrics.CommentRedisErrorCounterMetric.
				WithLabelValues("comment_redis", "BatchGetContentCache", "db_fallback").
				Inc()
			return nil, fmt.Errorf("db fallback BatchGetReplyContentByCommentIDs failed: %w", err)
		}

		for _, c := range dbContents {
			contentMap[c.CommentId] = c
		}

		// 回填 Redis
		if len(dbContents) > 0 {
			pipe := cache.rdb.Pipeline()
			for _, c := range dbContents {
				b, err := json.Marshal(c)
				if err != nil {
					continue
				}
				pipe.Set(ctx, ReplyContentKey(c.CommentId), b, defaultReplyContentTTL)
			}
			_, _ = pipe.Exec(ctx)
		}
	}

	// 按输入顺序重排输出
	result := make([]model2.CommentContent, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if c, ok := contentMap[id]; ok {
			result = append(result, c)
		}
	}

	return result, nil
}
