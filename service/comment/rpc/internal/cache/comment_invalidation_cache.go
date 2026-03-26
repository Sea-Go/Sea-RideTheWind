package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func (c *CommentCache) InvalidateTargetCaches(ctx context.Context, targetType, targetID string) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("comment cache is nil")
	}
	if targetType == "" || targetID == "" {
		return nil
	}

	keys := []string{SubjectKey(targetID)}
	pattern := fmt.Sprintf("comment:index:%s:%s:*", targetType, targetID)

	var cursor uint64
	for {
		matched, nextCursor, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan comment page cache failed: %w", err)
		}
		keys = append(keys, matched...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return c.deleteKeys(ctx, keys...)
}

func (c *CommentCache) DeleteCommentIndexCache(ctx context.Context, commentIDs ...int64) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("comment cache is nil")
	}
	if len(commentIDs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(commentIDs))
	for _, commentID := range commentIDs {
		if commentID <= 0 {
			continue
		}
		keys = append(keys, CommentIndexKey(commentID))
	}

	return c.deleteKeys(ctx, keys...)
}

func (c *CommentCache) deleteKeys(ctx context.Context, keys ...string) error {
	filtered := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, key)
	}
	if len(filtered) == 0 {
		return nil
	}

	for start := 0; start < len(filtered); start += 100 {
		end := start + 100
		if end > len(filtered) {
			end = len(filtered)
		}
		if err := c.rdb.Del(ctx, filtered[start:end]...).Err(); err != nil && err != redis.Nil {
			return fmt.Errorf("delete redis keys failed: %w", err)
		}
	}
	return nil
}
