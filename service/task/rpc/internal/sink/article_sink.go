package sink

import (
	"context"
	"encoding/json"
	"sea-try-go/service/task/rpc/internal/reward"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	//测试专用ID
	articleTaskID       = 190
	articleTarget       = int64(3)
	articleDoneTTL      = 90*24*time.Hour + 30*24*time.Hour
	articleInitTTL      = 24 * time.Hour
	articleFlushEvery   = 1 * time.Second
	redisPipeMaxDefault = 5000
	pgBatchMaxDefault   = 2000

	reconZKeyPrefix = "task:recon:active:"

	ownerHKey = "article:owner"
)

type ArticleLikeTaskProgress struct {
	UserID int64 `gorm:"primary_key;column:user_id"`
	TaskID int64 `gorm:"primary_key;column:task_id"`
	//ArticleID int64 `gorm:"primary_key;column:article_id"`

	Status   string `gorm:"column:status"`
	Progress int64  `gorm:"column:progress"`
	Target   int64  `gorm:"column:target"`

	/*	DoneAt    *time.Time `gorm:"column:done_at"`
		CreatedAt time.Time  `gorm:"column:created_at"`
		UpdatedAt time.Time  `gorm:"column:updated_at"`*/
}

func (ArticleLikeTaskProgress) TableName() string {
	return "task_progress"
}

type ArticleLikeSinkConsumer struct {
	rdb *redis.Client
	gdb *gorm.DB

	mu    sync.Mutex
	delta map[int64]articleAgg

	flushEvery   time.Duration
	redisPipeMax int
	pgBatchMax   int

	rewardProduct *reward.Product
	flushCh       chan struct{}
}

type articleAgg struct {
	UserID int64
	total  int64
}

type articleVal struct {
	UserID string `json:"user_id"`
	Cur    int64  `json:"cur"`
}

func NewArticleLikeSinkConsumer(rdb *redis.Client, gdb *gorm.DB) *ArticleLikeSinkConsumer {
	return &ArticleLikeSinkConsumer{
		rdb:           rdb,
		gdb:           gdb,
		delta:         make(map[int64]articleAgg, 1<<16),
		flushEvery:    articleFlushEvery,
		redisPipeMax:  redisPipeMaxDefault,
		pgBatchMax:    pgBatchMaxDefault,
		flushCh:       make(chan struct{}, 1),
		rewardProduct: reward.NewProduct(rdb),
	}
}

func (c *ArticleLikeSinkConsumer) Start(ctx context.Context) {
	go c.loop(ctx)
}

func (c *ArticleLikeSinkConsumer) Consume(ctx context.Context, key string, value string) error {
	if key == "" {
		return nil
	}
	articleID, err := strconv.ParseInt(key, 10, 64)
	if err != nil || articleID == 0 {
		return err
	}
	userID, d, ok := parseVal(value)
	if !ok || userID == 0 || d == 0 {
		return nil
	}
	c.mu.Lock()
	cur, exits := c.delta[articleID]
	if exits == false {
		c.delta[articleID] = articleAgg{
			UserID: userID,
			total:  d,
		}
		c.mu.Unlock()
		return nil
	}
	if d > cur.total {
		cur.total = d
		cur.UserID = userID
		c.delta[articleID] = cur
	}
	c.mu.Unlock()
	return nil
}

func parseVal(val string) (int64, int64, bool) {
	if val == "" {
		return 0, 0, false
	}
	var v articleVal
	if err := json.Unmarshal([]byte(val), &v); err == nil {
		if v.Cur == 0 {
			v.Cur = 1
		}
		UserID, _ := strconv.ParseInt(v.UserID, 10, 64)
		return UserID, v.Cur, UserID != 0
	}
	return 0, 0, false
}

func (c *ArticleLikeSinkConsumer) loop(ctx context.Context) {
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

func (c *ArticleLikeSinkConsumer) flushOnce(ctx context.Context) error {
	batch := c.swap()
	if len(batch) == 0 {
		return nil
	}

	if err := c.lazyInitTaskIfNeeded(ctx, batch); err != nil {
		return err
	}

	/*	_ = c.markReconCandidates(ctx, batch) //对账器*/

	if err := c.FlushRedis(ctx, batch); err != nil {
		logx.WithContext(ctx).Errorf("flush redis failed: %v", err)
		return err
	}

	/*if err := c.FlushPostgres(ctx, batch); err != nil {
		return err
	}*/
	return nil
}

func (c *ArticleLikeSinkConsumer) swap() map[int64]articleAgg {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.delta) == 0 {
		return nil
	}
	b := c.delta
	c.delta = make(map[int64]articleAgg, 1<<16)
	return b
}

func (c *ArticleLikeSinkConsumer) markReconCandidates(ctx context.Context, batch map[int64]articleAgg) error {
	zkey := reconZKeyPrefix + strconv.FormatInt(articleTaskID, 10)
	now := float64(time.Now().Unix())

	pipe := c.rdb.Pipeline()
	n := 0
	exec := func() error {
		if n == 0 {
			return nil
		}
		_, err := pipe.Exec(ctx)
		pipe = c.rdb.Pipeline()
		n = 0
		return err
	}

	for articleID, ag := range batch {
		pipe.ZAdd(ctx, zkey, redis.Z{
			Score:  now,
			Member: strconv.FormatInt(articleID, 10),
		})
		pipe.HSet(ctx, ownerHKey,
			strconv.FormatInt(articleID, 10),
			strconv.FormatInt(ag.UserID, 10),
		)
		n++
		if n >= c.redisPipeMax {
			if err := exec(); err != nil {
				return err
			}
		}
	}
	return exec()
}

func (c *ArticleLikeSinkConsumer) lazyInitTaskIfNeeded(ctx context.Context, batch map[int64]articleAgg) error {
	pipe := c.rdb.Pipeline()

	type item struct {
		articleID int64
		userID    int64
		cmd       *redis.BoolCmd
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

	items := make([]item, 0, min(len(batch), c.redisPipeMax))
	for articleID, ag := range batch {
		if ag.UserID == 0 {
			continue
		}
		key := "task:inits:" +
			strconv.FormatInt(articleTaskID, 10) + ":" +
			strconv.FormatInt(ag.UserID, 10) + ":" +
			strconv.FormatInt(articleID, 10)

		size++
		items = append(items, item{
			articleID: articleID,
			userID:    ag.UserID,
			cmd:       pipe.SetNX(ctx, key, "1", articleInitTTL),
		})
		if size >= c.redisPipeMax {
			if err := exec(); err != nil {
				return err
			}
		}
	}
	if err := exec(); err != nil {
		return err
	}

	type pair struct {
		userID    int64
		articleID int64
	}
	newOnes := make([]pair, 0)
	for _, it := range items {
		ok, err := it.cmd.Result()
		if err == nil && ok {
			newOnes = append(newOnes, pair{userID: it.userID, articleID: it.articleID})
		}
	}
	if len(newOnes) == 0 {
		return nil
	}

	//初始化一个记录，方便redisslnsight观测
	pipe2 := c.rdb.Pipeline()
	now := time.Now().Unix()
	for _, p := range newOnes {
		pk := "task:progress:" +
			strconv.FormatInt(articleTaskID, 10) + ":" +
			strconv.FormatInt(p.userID, 10) + ":" +
			strconv.FormatInt(p.articleID, 10)

		pipe2.HSet(ctx, pk,
			"status", "doing",
			"progress", 0,
			"target", articleTarget,
			"createAt", now,
			"updateAt", now,
		)
	}
	if _, err := pipe2.Exec(ctx); err != nil {
		return err
	}

	records := make([]ArticleLikeTaskProgress, 0, len(newOnes))
	for _, p := range newOnes {
		records = append(records, ArticleLikeTaskProgress{
			UserID: p.userID,
			TaskID: articleTaskID,
			/*	ArticleID: p.articleID,*/
			Status:   "doing",
			Progress: 0,
			Target:   articleTarget,
		})
	}

	return c.gdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "task_id"}},
		DoNothing: true,
	}).Create(&records).Error
}

func (c *ArticleLikeSinkConsumer) FlushRedis(ctx context.Context, batch map[int64]articleAgg) error {
	pipe := c.rdb.Pipeline()
	n := 0

	type cmdItem struct {
		articleID int64
		userID    int64
		total     int64
	}
	items := make([]cmdItem, 0, min(len(batch), c.redisPipeMax+1))

	exec := func() error {
		if n == 0 {
			return nil
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			pipe = c.rdb.Pipeline()
			n = 0
			items = items[:0]
			return err
		}
		for _, it := range items {
			if it.total >= articleTarget {
				_ = c.completeArticleLike(ctx, it.userID, it.articleID, it.total)
				continue
			}
			_ = c.updateDoingTaskProgress(ctx, it.userID, it.total)
		}
		pipe = c.rdb.Pipeline()
		n = 0
		items = items[:0]
		return nil
	}
	for articleID, ag := range batch {
		if ag.UserID == 0 || ag.total <= 0 {
			continue
		}
		cntKey := "article:like:cnt:" + strconv.FormatInt(articleID, 10)
		pipe.Set(ctx, cntKey, ag.total, articleDoneTTL)
		items = append(items, cmdItem{
			articleID: articleID,
			userID:    ag.UserID,
			total:     ag.total,
		})
		n++
		if n >= c.redisPipeMax {
			if err := exec(); err != nil {
				return err
			}
		}
	}
	return exec()
}

func (c *ArticleLikeSinkConsumer) updateDoingTaskProgress(ctx context.Context, userID, progress int64) error {
	if progress < 0 {
		progress = 0
	}
	if progress > articleTarget {
		progress = articleTarget
	}

	rec := ArticleLikeTaskProgress{
		UserID:   userID,
		TaskID:   articleTaskID,
		Status:   "doing",
		Progress: progress,
		Target:   articleTarget,
	}

	if err := c.gdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "task_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN task_progress.status = 'done' THEN task_progress.status ELSE EXCLUDED.status END",
			),
			"progress":   gorm.Expr("GREATEST(task_progress.progress, EXCLUDED.progress)"),
			"target":     articleTarget,
			"updated_at": gorm.Expr("now()"),
		}),
	}).Create(&rec).Error; err != nil {
		return err
	}

	return c.invalidateTaskCache(ctx, userID)
}

func (c *ArticleLikeSinkConsumer) invalidateTaskCache(ctx context.Context, userID int64) error {
	return c.rdb.Del(ctx, buildTaskCacheKey(userID)).Err()
}

func (c *ArticleLikeSinkConsumer) completeArticleLike(ctx context.Context, userID int64, articleID int64, total int64) error {
	//每个人只能完成一次这个任务，所以选择userID,而不是articleID
	doneKey := "task:done:" + strconv.FormatInt(articleTaskID, 10) + ":" + strconv.FormatInt(userID, 10)
	ok, err := c.rdb.SetNX(ctx, doneKey, "1", articleDoneTTL).Result()
	if err != nil || !ok {
		return err
	}

	now := time.Now()
	nowUnix := now.Unix()

	pk := "task:progress:" +
		strconv.FormatInt(articleTaskID, 10) + ":" +
		strconv.FormatInt(userID, 10) + ":" +
		strconv.FormatInt(articleID, 10)
	_, _ = c.rdb.HSet(ctx, pk,
		"status", "done",
		"doneAt", nowUnix,
		"progress", articleTarget,
		"updateAt", nowUnix,
	).Result()

	ev := reward.NewEvent(strconv.Itoa(int(userID)), strconv.Itoa(articleTaskID), 5)
	if err := c.rewardProduct.Enqueue(ctx, ev); err != nil {
		//需要补充如果失败后的异常处理
		return err
	}
	if err := c.markTaskDoneInDB(userID, articleID, now, total); err != nil {
		return err
	}
	return c.invalidateTaskCache(ctx, userID)
}

/*func (c *ArticleLikeSinkConsumer) FlushPostgres(ctx context.Context, batch map[string]articleVal) error {

}*/

func (c *ArticleLikeSinkConsumer) markTaskDoneInDB(userID int64, articleID int64, doneAt time.Time, total int64) error {
	/*articleID, err := strconv.ParseInt(_articleID, 10, 64)
	if err != nil {
		return err
	}*/
	res := ArticleLikeTaskProgress{
		UserID: userID,
		TaskID: articleTaskID,
		//ArticleID: articleID,
		Status:   "done",
		Progress: articleTarget,
		Target:   articleTarget,
		//DoneAt:    &doneAt,
	}

	return c.gdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "task_id"},
			//{Name: "article_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":     "done",
			"progress":   articleTarget,
			"target":     articleTarget,
			"done_at":    gorm.Expr("COALESCE(task_progress.done_at, now())"),
			"updated_at": gorm.Expr("now()"),
		}),
	}).Create(&res).Error
}
