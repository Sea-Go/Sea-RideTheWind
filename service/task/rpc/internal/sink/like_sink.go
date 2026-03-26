package sink

import (
	"context"
	"sea-try-go/service/task/rpc/internal/reward"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserLikeCount struct {
	UserID    string    `gorm:"primary_key;column:user_id"`
	LikeCount int64     `gorm:"column:like_count"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserLikeCount) TableName() string {
	return "user_like_count"
}

func (UserTaskProgress) TableName() string {
	return "task_progress"
}

const (
	taskID = int64(1902)
	target = 5
)

type UserTaskProgress struct {
	UserID   int64  `gorm:"primary_key;column:user_id"`
	TaskID   int64  `gorm:"primary_key;column:task_id"`
	Status   string `gorm:"column:status"`
	Progress int64  `gorm:"column:progress"`
	Target   int64  `gorm:"column:target"`
}

type LikeSinkConsumer struct {
	rdb *redis.Client
	gdb *gorm.DB

	mu    sync.Mutex
	delta map[int64]int64

	flushEvery   time.Duration
	redisPipeMax int
	pgBatchMax   int

	rewardProduct *reward.Product
	flushCh       chan struct{}
}

type row struct {
	uid   int64
	total int64
}

func NewUserLikeSinkConsumer(rdb *redis.Client, gdb *gorm.DB) *LikeSinkConsumer {
	return &LikeSinkConsumer{
		rdb:           rdb,
		gdb:           gdb,
		delta:         make(map[int64]int64, 1<<16),
		flushEvery:    1 * time.Second,
		redisPipeMax:  5000,
		pgBatchMax:    2000,
		flushCh:       make(chan struct{}, 1),
		rewardProduct: reward.NewProduct(rdb),
	}
}

func (c *LikeSinkConsumer) Start(ctx context.Context) {
	go c.loop(ctx)
}

func (c *LikeSinkConsumer) Consume(ctx context.Context, key string, val string) error {
	userID, err := strconv.ParseInt(key, 10, 63)
	if err != nil {
		return err
	}
	if userID <= 0 {
		return nil
	}

	total := int64(1)
	if val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			total = n
		}
	}
	if total <= 0 {
		return nil
	}

	c.mu.Lock()
	if current, ok := c.delta[userID]; !ok || total > current {
		c.delta[userID] = total
	}
	c.mu.Unlock()
	return nil
}

func (c *LikeSinkConsumer) loop(ctx context.Context) {

	ticker := time.NewTicker(c.flushEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = c.flushOnce(context.Background())
			return
		case <-ticker.C:
			_ = c.flushOnce(ctx)
		case <-c.flushCh:
			_ = c.flushOnce(ctx)
		}
	}
}

func (c *LikeSinkConsumer) flushOnce(ctx context.Context) error {
	batch := c.swap()
	if len(batch) == 0 {
		return nil
	}
	if err := c.lazyInitTaskIfNeeded(ctx, batch); err != nil {
		return err
	}
	if err := c.flushRedis(ctx, batch); err != nil {
		return err
	}
	if err := c.flushPostgres(ctx, batch); err != nil {
		return err
	}
	return nil
}

func (c *LikeSinkConsumer) lazyInitTaskIfNeeded(ctx context.Context, batch map[int64]int64) error {

	pipe := c.rdb.Pipeline()
	type item struct {
		uid int64
		cmd *redis.BoolCmd
	}
	size := 0
	exec := func() error {
		if size == 0 {
			return nil
		}
		_, err := pipe.Exec(ctx)
		pipe = c.rdb.Pipeline()
		size = 0
		return err
	}

	items := make([]item, 0, len(batch))
	for _uid := range batch {
		uid := strconv.FormatInt(_uid, 10)
		k := "task:init:" + uid + ":" + strconv.FormatInt(taskID, 10)
		size++
		items = append(items, item{uid: _uid, cmd: pipe.SetNX(ctx, k, "1", 90*24*time.Hour)})
		if size >= c.redisPipeMax {
			if err := exec(); err != nil {
				return err
			}
		}
	}
	if err := exec(); err != nil {
		return err
	}

	newUsers := make([]int64, 0)
	for _, it := range items {
		ok, err := it.cmd.Result()
		if err == nil && ok {
			newUsers = append(newUsers, it.uid)
		}
	}
	if len(newUsers) == 0 {
		return nil
	}

	pipe2 := c.rdb.Pipeline()
	now := time.Now().Unix()
	for _, _uid := range newUsers {
		uid := strconv.FormatInt(_uid, 10)
		pk := "task:progress:" + uid + ":" + strconv.FormatInt(taskID, 10)
		pipe2.HSet(ctx, pk,
			"status", "doing",
			"progress", 0,
			"target", target,
			"createAt", now,
			"updateAt", now)
	}

	if _, err := pipe2.Exec(ctx); err != nil {
		return err
	}

	records := make([]UserTaskProgress, 0, len(newUsers))
	for _, uid := range newUsers {
		records = append(records, UserTaskProgress{
			UserID:   uid,
			TaskID:   taskID,
			Status:   "doing",
			Progress: 0,
			Target:   target,
		})
	}
	return c.gdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "task_id"}},
		DoNothing: true,
	}).Create(&records).Error
}

func (c *LikeSinkConsumer) swap() map[int64]int64 {

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.delta) == 0 {
		return nil
	}
	b := c.delta
	c.delta = make(map[int64]int64, 1<<16)
	return b
}

func (c *LikeSinkConsumer) flushRedis(ctx context.Context, batch map[int64]int64) error {
	pipe := c.rdb.Pipeline()
	n := 0

	exec := func() error {
		if n == 0 {
			return nil
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			pipe = c.rdb.Pipeline()
			n = 0
			return err
		}
		for uid, total := range batch {
			if total >= target {
				_ = c.completeLikeGT5(ctx, uid, total)
				continue
			}
			_ = c.updateDoingTaskProgress(ctx, uid, total)
		}

		pipe = c.rdb.Pipeline()
		n = 0
		return nil
	}

	for uid, total := range batch {
		pipe.Set(ctx, "like:total:"+strconv.FormatInt(uid, 10), total, 90*24*time.Hour)
		n++
		if n > c.redisPipeMax {
			if err := exec(); err != nil {
				return err
			}
		}
	}
	return exec()
}

func (c *LikeSinkConsumer) updateDoingTaskProgress(ctx context.Context, uid, progress int64) error {
	if progress < 0 {
		progress = 0
	}
	if progress > target {
		progress = target
	}

	rec := UserTaskProgress{
		UserID:   uid,
		TaskID:   taskID,
		Status:   "doing",
		Progress: progress,
		Target:   target,
	}

	if err := c.gdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN task_progress.status = 'done' THEN task_progress.status ELSE EXCLUDED.status END",
			),
			"progress":   gorm.Expr("GREATEST(task_progress.progress, EXCLUDED.progress)"),
			"target":     target,
			"updated_at": gorm.Expr("now()"),
		}),
	}).Create(&rec).Error; err != nil {
		return err
	}

	return c.invalidateTaskCache(ctx, uid)
}

func (c *LikeSinkConsumer) invalidateTaskCache(ctx context.Context, uid int64) error {
	return c.rdb.Del(ctx, buildTaskCacheKey(uid)).Err()
}

func buildTaskCacheKey(uid int64) string {
	return "task:progress:" + strconv.FormatInt(uid, 10)
}

func (c *LikeSinkConsumer) completeLikeGT5(ctx context.Context, _uid, total int64) error {
	uid := strconv.FormatInt(_uid, 10)
	doneKey := "task:done:" + uid + ":" + strconv.FormatInt(taskID, 10)
	ok, err := c.rdb.SetNX(ctx, doneKey, "1", 90*24*time.Hour).Result()
	if err != nil || !ok {
		return err
	}

	now := time.Now().Unix()
	pk := "task:progress:" + uid + ":" + strconv.FormatInt(taskID, 10)
	_, err = c.rdb.HSet(ctx, pk,
		"status", "done",
		"doneAt", now,
		"progress", target, // 你也可以写成 5 或 likeTotal，看你语义
		"updateAt", now,
	).Result()
	if err != nil {
		return err
	}
	ev := reward.NewEvent(uid, strconv.FormatInt(taskID, 10), 5)
	if err := c.rewardProduct.Enqueue(ctx, ev); err != nil {
		return err
	}
	if err := c.markTaskDoneInDB(_uid, total); err != nil {
		// 这里建议记录日志即可，不要让整个 sink 挂掉（看你容错策略）
		return err
	}
	return c.invalidateTaskCache(ctx, _uid)
}

func (c *LikeSinkConsumer) markTaskDoneInDB(uid, progress int64) error {
	rec := UserTaskProgress{
		UserID:   uid,
		TaskID:   taskID,
		Status:   "done",
		Progress: target,
		Target:   target,
	}

	return c.gdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"status":     "done",
			"progress":   target,
			"target":     target,
			"done_at":    gorm.Expr("COALESCE(task_progress.done_at, now())"),
			"updated_at": gorm.Expr("now()"),
		}),
	}).Create(&rec).Error
}

func (c *LikeSinkConsumer) flushPostgres(ctx context.Context, batch map[int64]int64) error {

	rows := make([]row, 0, len(batch))
	for uid, total := range batch {
		rows = append(rows, row{uid, total})
	}

	for i := 0; i < len(rows); i++ {
		end := i + c.pgBatchMax
		if end > len(rows) {
			end = len(rows)
		}
		if err := c.upsertChunk(rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (c *LikeSinkConsumer) upsertChunk(rows []row) error {
	records := make([]UserLikeCount, 0, len(rows))
	for _, row := range rows {
		records = append(records, UserLikeCount{
			UserID:    strconv.FormatInt(row.uid, 10),
			LikeCount: row.total,
		})
	}

	return c.gdb.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"like_count": gorm.Expr("GREATEST(user_like_count.like_count, EXCLUDED.like_count)"),
				"updated_at": gorm.Expr("now()"),
			}),
		}).Create(&records).Error
}
