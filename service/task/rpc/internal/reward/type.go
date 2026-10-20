package reward

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"time"
)

const (
	StreamKey = "stream:task:reward"
	GroupName = "task-reward-group"
)

type RedisEvent struct {
	RewardID int64 //幂等ID
	UID      int64
	TaskID   int64
	Ts       int64
	AddScore int64
}

// NewEvent 从字符串 ID 构造事件；解析失败返回错误而不是静默归零
// （原实现的 ignored parse 会让坏 ID 变成 uid=0 的事件进入发放链路）。
func NewEvent(uid, taskID string, addScore int64) (*RedisEvent, error) {
	u, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("reward: invalid uid %q: %w", uid, err)
	}
	t, err := strconv.ParseInt(taskID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("reward: invalid task id %q: %w", taskID, err)
	}
	return NewEventFromInts(u, t, addScore), nil
}

func NewEventFromInts(uid, taskID, addScore int64) *RedisEvent {
	return &RedisEvent{
		// 幂等键由 (uid, taskID) 的 FNV-64 哈希合成。
		// 旧实现用加法合成，(uid=1,task=9) 与 (9,1) 碰撞导致幂等失效；
		// 消费端 worker 当前为死代码，无在途去重依赖旧公式，可安全切换。
		RewardID: idempotencyKey(uid, taskID),
		UID:      uid,
		TaskID:   taskID,
		Ts:       time.Now().Unix(),
		AddScore: addScore,
	}
}

func idempotencyKey(uid, taskID int64) int64 {
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(uid, 10)))
	h.Write([]byte{':'})
	h.Write([]byte(strconv.FormatInt(taskID, 10)))
	return int64(h.Sum64())
}
