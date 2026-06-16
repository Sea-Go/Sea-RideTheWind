package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"sea-try-go/service/common/observability"
	"sea-try-go/service/hot/heavykeeper"
	"sea-try-go/service/hot/rpc/internal/svc"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"go.opentelemetry.io/otel/attribute"
)

const (
	mergeLockKey  = "hot:merge:lock"
	lockTTL       = 10 * time.Second
	renewInterval = 3 * time.Second
)

var (
	eventConsumeTotal = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "article_hot",
		Subsystem: "kafka",
		Name:      "consume_total",
		Help:      "Total number of consumed hot events",
		Labels:    []string{"type"},
	})
	redisSyncTotal = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "article_hot",
		Subsystem: "redis",
		Name:      "sync_total",
		Help:      "Total number of redis sync operations",
		Labels:    []string{"status"},
	})
)

type HotHandler struct {
	svcCtx     *svc.ServiceContext
	instanceID string
	topic      string
	hkMap      map[int]*heavykeeper.HeavyKeeper
	hkMu       sync.RWMutex
	counter    atomic.Int64
	syncEvery  int64
	hotTTL     time.Duration
	weights    map[string]int32
	lockCancel context.CancelFunc
}

func NewHotHandler(svcCtx *svc.ServiceContext, syncEvery int64, hotTTL time.Duration, weights map[string]int32, topic string) *HotHandler {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	h := &HotHandler{
		svcCtx:     svcCtx,
		instanceID: fmt.Sprintf("%s-%d", hostname, os.Getpid()),
		topic:      topic,
		hkMap:      make(map[int]*heavykeeper.HeavyKeeper),
		syncEvery:  syncEvery,
		hotTTL:     hotTTL,
		weights:    weights,
	}

	go h.periodicSync(context.Background())
	go h.periodicMerge(context.Background())

	return h
}

// sarama.ConsumerGroupHandler 接口实现
func (h *HotHandler) Setup(sess sarama.ConsumerGroupSession) error {
	for topic, partitions := range sess.Claims() {
		if topic == h.topic {
			h.OnPartitionsAssigned(partitions)
		}
	}
	return nil
}

func (h *HotHandler) Cleanup(sess sarama.ConsumerGroupSession) error {
	for topic, partitions := range sess.Claims() {
		if topic == h.topic {
			h.OnPartitionsRevoked(partitions)
		}
	}
	return nil
}

func (h *HotHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		partitionID := int(msg.Partition)
		key := string(msg.Key)
		_ = observability.TraceConsumer(sess.Context(), "hot-rpc", "HotHandler.Consume", hotMessageAttrs(h.topic, partitionID, key), func(ctx context.Context) error {
			return h.consumeMessage(ctx, partitionID, key, string(msg.Value))
		})
		sess.MarkMessage(msg, "")
	}
	return nil
}

func (h *HotHandler) OnPartitionsAssigned(partitions []int32) {
	h.hkMu.Lock()
	defer h.hkMu.Unlock()
	for _, p := range partitions {
		pid := int(p)
		if _, ok := h.hkMap[pid]; ok {
			continue
		}
		hk := heavykeeper.New(h.svcCtx.Config.HeavyKeeper)
		h.hkMap[pid] = hk
		h.loadFromRedis(pid, hk)
		logx.Infof("[Hot] %s assigned partition %d", h.instanceID, pid)
	}
}

func (h *HotHandler) OnPartitionsRevoked(partitions []int32) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	h.hkMu.Lock()
	defer h.hkMu.Unlock()
	for _, p := range partitions {
		pid := int(p)
		h.syncToRedis(ctx, pid)
		delete(h.hkMap, pid)
		logx.Infof("[Hot] %s revoked partition %d", h.instanceID, pid)
	}
}

func (h *HotHandler) consumeMessage(ctx context.Context, partitionID int, key, value string) error {
	start := time.Now()
	defer func() {
		cost := time.Since(start)
		if cost > 100*time.Millisecond {
			logx.Slowf("[HotHandler] slow consume, cost: %v, value: %s", cost, value)
		}
	}()

	var event ArticleHotEvent
	if err := json.Unmarshal([]byte(value), &event); err != nil {
		logx.Errorf("[HotHandler] unmarshal event failed: %v, value: %s", err, value)
		return err
	}

	if event.ArticleID == "" {
		logx.Errorf("[HotHandler] invalid event: %+v", event)
		return fmt.Errorf("invalid hot event: missing article id")
	}

	weight, ok := h.weights[event.Type]
	if !ok || weight <= 0 {
		logx.Errorf("[HotHandler] unknown or zero-weight type: %s, event: %+v", event.Type, event)
		return fmt.Errorf("unknown or zero-weight hot event type: %s", event.Type)
	}

	eventConsumeTotal.Inc(event.Type)

	h.hkMu.RLock()
	hk, exists := h.hkMap[partitionID]
	h.hkMu.RUnlock()
	if !exists {
		return nil
	}

	hk.Add(event.ArticleID, uint32(weight))

	if h.counter.Add(1)%h.syncEvery == 0 {
		go h.syncToRedis(context.Background(), partitionID)
	}
	return nil
}

func hotMessageAttrs(topic string, partitionID int, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", topic),
		attribute.String("messaging.operation", "consume"),
		attribute.Int("messaging.kafka.partition", partitionID),
		attribute.String("messaging.message.key", key),
	}
}

func (h *HotHandler) syncToRedis(ctx context.Context, partitionID int) {
	h.hkMu.RLock()
	hk, exists := h.hkMap[partitionID]
	h.hkMu.RUnlock()
	if !exists {
		return
	}

	items := hk.TopK()
	if len(items) == 0 {
		return
	}

	partitionKey := fmt.Sprintf("hot:articles:p%d", partitionID)
	shadowKey := fmt.Sprintf("hot:articles:p%d:shadow", partitionID)

	zMembers := make([]redis.Z, len(items))
	for i, item := range items {
		zMembers[i] = redis.Z{Score: float64(item.Count), Member: item.Key}
	}

	pipe := h.svcCtx.RedisClient.Pipeline()
	pipe.Del(ctx, shadowKey)
	pipe.ZAdd(ctx, shadowKey, zMembers...)
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		logx.Errorf("[Sync] partition %d pipeline failed: %v, cmds=%d", partitionID, err, len(cmds))
		redisSyncTotal.Inc("fail")
		return
	}
	logx.Infof("[Sync] partition %d pipeline success: items=%d", partitionID, len(items))

	renameScript := redis.NewScript(`
		if redis.call('EXISTS', KEYS[1]) == 0 then
			return 0
		end
		redis.call('RENAME', KEYS[1], KEYS[2])
		redis.call('EXPIRE', KEYS[2], 300)
		return 1
	`)
	result, err := renameScript.Run(ctx, h.svcCtx.RedisClient, []string{shadowKey, partitionKey}).Int()
	if err != nil || result == 0 {
		logx.Errorf("[Sync] partition %d rename failed: err=%v, result=%d", partitionID, err, result)
		redisSyncTotal.Inc("fail")
	} else {
		redisSyncTotal.Inc("success")
	}
}

func (h *HotHandler) periodicSync(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.hkMu.RLock()
			pids := make([]int, 0, len(h.hkMap))
			for pid := range h.hkMap {
				pids = append(pids, pid)
			}
			h.hkMu.RUnlock()
			for _, pid := range pids {
				go h.syncToRedis(ctx, pid)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *HotHandler) loadFromRedis(partitionID int, hk *heavykeeper.HeavyKeeper) {
	ctx := context.Background()
	partitionKey := fmt.Sprintf("hot:articles:p%d", partitionID)
	items, err := h.svcCtx.RedisClient.ZRevRangeWithScores(ctx, partitionKey, 0, -1).Result()
	if err != nil {
		logx.Errorf("[Hot] partition %d load failed: %v", partitionID, err)
		return
	}

	for _, item := range items {
		articleID, ok := item.Member.(string)
		if !ok {
			continue
		}
		hk.Add(articleID, uint32(item.Score))
	}

	logx.Infof("[Hot] loaded %d articles for partition %d", len(items), partitionID)
}

func (h *HotHandler) acquireLockWithWatchDog(ctx context.Context) bool {
	ok, err := h.svcCtx.RedisClient.SetNX(ctx, mergeLockKey, h.instanceID, lockTTL).Result()
	if err != nil || !ok {
		return false
	}

	watchCtx, cancel := context.WithCancel(ctx)
	h.lockCancel = cancel
	go h.watchDog(watchCtx)
	return true
}

func (h *HotHandler) watchDog(ctx context.Context) {
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	renewScript := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		end
		return 0
	`)

	for {
		select {
		case <-ticker.C:
			result, _ := renewScript.Run(ctx, h.svcCtx.RedisClient,
				[]string{mergeLockKey}, h.instanceID, int64(lockTTL.Seconds())).Int()
			if result == 0 {
				logx.Errorf("[Lock] %s lost lock", h.instanceID)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *HotHandler) releaseLock(ctx context.Context) {
	if h.lockCancel != nil {
		h.lockCancel()
		h.lockCancel = nil
	}

	releaseLockScript := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		end
		return 0
	`)
	releaseLockScript.Run(ctx, h.svcCtx.RedisClient, []string{mergeLockKey}, h.instanceID)
}

func (h *HotHandler) periodicMerge(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !h.acquireLockWithWatchDog(ctx) {
				continue
			}
			h.mergePartitions(ctx)
			h.releaseLock(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (h *HotHandler) mergePartitions(ctx context.Context) {
	partitionCount := h.svcCtx.Config.Interaction.PartitionCount

	pipe := h.svcCtx.RedisClient.Pipeline()
	cmds := make([]*redis.ZSliceCmd, partitionCount)
	for i := 0; i < partitionCount; i++ {
		cmds[i] = pipe.ZRevRangeWithScores(ctx, fmt.Sprintf("hot:articles:p%d", i), 0, 999)
	}
	pipe.Exec(ctx)

	scoreMap := make(map[string]float64, 1000)
	for _, cmd := range cmds {
		items, _ := cmd.Result()
		for _, z := range items {
			scoreMap[z.Member.(string)] += z.Score
		}
	}

	type pair struct {
		id    string
		score float64
	}
	merged := make([]pair, 0, len(scoreMap))
	for id, score := range scoreMap {
		merged = append(merged, pair{id, score})
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].score > merged[j].score })
	if len(merged) > 100 {
		merged = merged[:100]
	}
	if len(merged) == 0 {
		return
	}

	shadowKey := "hot:articles:shadow"
	zMembers := make([]redis.Z, len(merged))
	for i, p := range merged {
		zMembers[i] = redis.Z{Score: p.score, Member: p.id}
	}
	pipe2 := h.svcCtx.RedisClient.Pipeline()
	pipe2.Del(ctx, shadowKey)
	pipe2.ZAdd(ctx, shadowKey, zMembers...)
	pipe2.Exec(ctx)

	renameScript := redis.NewScript(`
		if redis.call("GET", "hot:merge:lock") ~= ARGV[1] then
			return 0
		end
		redis.call('RENAME', KEYS[1], KEYS[2])
		redis.call('EXPIRE', KEYS[2], ARGV[2])
		return 1
	`)
	result, _ := renameScript.Run(ctx, h.svcCtx.RedisClient,
		[]string{shadowKey, "hot:articles"},
		h.instanceID, int64(h.hotTTL.Seconds())).Int()

	if result == 0 {
		logx.Errorf("[Merge] lock lost, abort")
	}
}
