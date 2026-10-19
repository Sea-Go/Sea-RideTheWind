package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/metrics"
	model2 "github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/model"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultSubjectTTL = 5 * time.Minute

func (c *CommentCache) GetSubjectWithCache(ctx context.Context, subjectType, subjectID string, conn *model2.CommentModel) (model2.Subject, error) {
	if c == nil || c.rdb == nil {
		return model2.Subject{}, fmt.Errorf("comment cache is nil")
	}
	if conn == nil {
		return model2.Subject{}, fmt.Errorf("comment model conn is nil")
	}
	if subjectID == "" {
		return model2.Subject{}, fmt.Errorf("invalid subjectID: empty")
	}

	// --- 1. 尝试从缓存取
	if cached, err := c.GetSubjectCache(ctx, subjectID); err == nil && cached != nil {
		return *cached, nil
	} else if err != nil {
		// Redis异常埋点
		metrics.CommentRedisErrorCounterMetric.
			WithLabelValues("comment_redis", "GetSubjectWithCache", "query").
			Inc()
	}

	sfKey := fmt.Sprintf("subject:%s", subjectID)

	v, err, _ := c.sf.Do(sfKey, func() (interface{}, error) {
		// 双检 Redis
		if cached, err := c.GetSubjectCache(ctx, subjectID); err == nil && cached != nil {
			return *cached, nil
		} else if err != nil {
			metrics.CommentRedisErrorCounterMetric.
				WithLabelValues("comment_redis", "GetSubjectWithCache", "query").
				Inc()
		}

		// --- 2. 回源 DB
		subject, dbErr := conn.FindOneSubjectByTarget(ctx, subjectType, subjectID)
		if dbErr != nil {
			metrics.CommentRedisErrorCounterMetric.
				WithLabelValues("comment_redis", "GetSubjectWithCache", "db_fallback").
				Inc()
			return model2.Subject{}, dbErr
		}

		// --- 3. 回填 Redis
		_ = c.SetSubjectCache(ctx, subjectID, &subject, 5*time.Minute)

		return subject, nil
	})

	if err != nil {
		return model2.Subject{}, err
	}

	subject, ok := v.(model2.Subject)
	if !ok {
		return model2.Subject{}, fmt.Errorf("singleflight result type assert failed")
	}

	return subject, nil
}

func (c *CommentCache) GetSubjectCache(ctx context.Context, subjectID string) (*model2.Subject, error) {
	if c == nil || c.rdb == nil {
		return nil, fmt.Errorf("comment cache is nil")
	}
	if subjectID == "" {
		return nil, fmt.Errorf("invalid subjectID: empty")
	}

	val, err := c.rdb.Get(ctx, SubjectKey(subjectID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		metrics.CommentRedisErrorCounterMetric.
			WithLabelValues("comment_redis", "GetSubjectCache", "query").
			Inc()
		return nil, err
	}

	var s model2.Subject
	if err := json.Unmarshal([]byte(val), &s); err != nil {
		// JSON 解析失败可以视作缓存未命中，不记 metric
		return nil, err
	}
	return &s, nil
}

func (c *CommentCache) SetSubjectCache(ctx context.Context, subjectID string, subject *model2.Subject, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("comment cache is nil")
	}
	if subjectID == "" {
		return fmt.Errorf("invalid subjectID: empty")
	}
	if subject == nil {
		return fmt.Errorf("subject is nil")
	}
	if ttl <= 0 {
		ttl = defaultSubjectTTL
	}

	key := SubjectKey(subjectID)

	b, err := json.Marshal(subject)
	if err != nil {
		return fmt.Errorf("marshal subject cache failed, subjectID=%s: %w", subjectID, err)
	}

	if err := c.rdb.Set(ctx, key, b, ttl).Err(); err != nil {
		metrics.CommentRedisErrorCounterMetric.
			WithLabelValues("comment_redis", "SetSubjectCache", "set").
			Inc()
		return fmt.Errorf("redis set subject cache failed, key=%s: %w", key, err)
	}

	return nil
}
