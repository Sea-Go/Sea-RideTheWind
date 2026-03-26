package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/like/common/errmsg"
	"sea-try-go/service/like/rpc/internal/metrics"
	"sea-try-go/service/like/rpc/internal/svc"
	"sea-try-go/service/like/rpc/internal/types"
	"sea-try-go/service/like/rpc/pb"
	messagepb "sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	//nodeId目前为1,如果部署在多个服务器上可以利用环境变量读取nodeId
	nodeId   int64 = 1
	sequence int64 = 0
	lastTime int64 = -1
	sfMutex  sync.Mutex
)

// 手写一个新的雪花算法ID
// 目的是和其余部分的逻辑对齐,因为Redis部分Zset的Score是ID,雪花算法的ID前41位就是时间戳
// 原有的snowflake的起始时间与本服务中不符,返回给前端可能点赞时间对不上
// 所以新写了一个
func generateCursorID() int64 {
	sfMutex.Lock()
	defer sfMutex.Unlock()
	now := time.Now().UnixMilli()
	if now == lastTime {
		sequence = (sequence + 1) % 4096
		if sequence == 0 {
			for now <= lastTime {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		sequence = 0
	}
	lastTime = now
	return (now << 22) | (nodeId << 12) | sequence
}

const LikeActionLua = `
local target_id = ARGV[1]
local user_id = ARGV[2]
local action_type = tonumber(ARGV[3])
local score = ARGV[4]

-- KEYS[1]=like_count, KEYS[2]=like_state, KEYS[3]=user_like_list, 
-- KEYS[4]=target_liker_list, KEYS[5]=dislike_count, KEYS[6]=user_total_like

local current_state = tonumber(redis.call("HGET", KEYS[2], user_id) or "0")
if current_state == action_type then
    return 0 
end

redis.call("HSET", KEYS[2], user_id, action_type)

if action_type == 1 then
    redis.call("HINCRBY", KEYS[1], target_id, 1)
    redis.call("INCR", KEYS[6])
    redis.call("ZADD", KEYS[3], score, target_id)
    redis.call("ZADD", KEYS[4], score, user_id)
    if current_state == 3 then
        local curr_dislike = tonumber(redis.call("HGET", KEYS[5], target_id) or "0")
        if curr_dislike > 0 then
            redis.call("HINCRBY", KEYS[5], target_id, -1)
        end
    end

elseif action_type == 2 then
    local curr_like = tonumber(redis.call("HGET", KEYS[1], target_id) or "0")
    if curr_like > 0 then
        redis.call("HINCRBY", KEYS[1], target_id, -1)
        local curr_total = tonumber(redis.call("GET", KEYS[6]) or "0")
        if curr_total > 0 then
            redis.call("DECR", KEYS[6])
        end
    end
    redis.call("ZREM", KEYS[3], target_id)
    redis.call("ZREM", KEYS[4], user_id)

elseif action_type == 3 then
    redis.call("HINCRBY", KEYS[5], target_id, 1)
    if current_state == 1 then
        local curr_like = tonumber(redis.call("HGET", KEYS[1], target_id) or "0")
        if curr_like > 0 then
            redis.call("HINCRBY", KEYS[1], target_id, -1)
            local curr_total = tonumber(redis.call("GET", KEYS[6]) or "0")
            if curr_total > 0 then
                redis.call("DECR", KEYS[6])
            end
        end
        redis.call("ZREM", KEYS[3], target_id)
        redis.call("ZREM", KEYS[4], user_id)
    end

elseif action_type == 4 then
    local curr_dislike = tonumber(redis.call("HGET", KEYS[5], target_id) or "0")
    if curr_dislike > 0 then
        redis.call("HINCRBY", KEYS[5], target_id, -1)
    end
end
if action_type == 1 and current_state == 0 then
	return 2
end

return 1
`

const (
	activeLikeState = 1
)

type LikeActionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLikeActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeActionLogic {
	return &LikeActionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func getActionString(action pb.ActionType) string {
	switch action {
	case pb.ActionType_ACTION_LIKE:
		return "like"
	case pb.ActionType_ACTION_UNLIKE:
		return "unlike"
	case pb.ActionType_ACTION_DISLIKE:
		return "dislike"
	case pb.ActionType_ACTION_UNDISLIKE:
		return "undislike"
	default:
		return "unknown"
	}
}

func (l *LikeActionLogic) LikeAction(in *pb.LikeActionReq) (*pb.LikeActionResp, error) {

	actionStr := getActionString(in.ActionType)

	span := trace.SpanFromContext(l.ctx)
	span.SetAttributes(
		attribute.Int64("like.user_id", in.UserId),
		attribute.String("like.target_type", in.TargetType),
		attribute.String("like.target_id", in.TargetId),
		attribute.String("like.action_type", actionStr),
	)

	if in.TargetId == "" || in.TargetType == "" || in.UserId <= 0 {
		metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "invalid_param").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorInputWrong, fmt.Errorf("输入参数异常"))
		return nil, errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "参数异常")
	}

	//Hash类型,表示某类作品的点赞表,Field是视频ID,Value是点赞数
	countKey := fmt.Sprintf("like_count:%s", in.TargetType)

	//Hash类型,表示某个作品的点赞情况,Field是用户ID,Value是用户对作品的点赞情况
	stateKey := fmt.Sprintf("like_state:%s:%s", in.TargetType, in.TargetId)

	//Zset类型,表示某个用户某类作品的点赞列表,Member是作品ID,Score是点赞的时间戳游标
	userListKey := fmt.Sprintf("user_like_list:%d:%s", in.UserId, in.TargetType)

	//Zset类型,表示某个作品的点赞列表,Member是点赞者ID,Score是点赞时间戳游标
	targetListKey := fmt.Sprintf("target_liker_list:%s:%s", in.TargetType, in.TargetId)

	dislikeCountKey := fmt.Sprintf("dislike_count:%s", in.TargetType)

	authorTotalKey := fmt.Sprintf("user_total_like:%d", in.AuthorId)

	score := generateCursorID()
	//[]string里面是Lua中KEYS数组对应的内容,后面的参数是ARGV数组对应的内容
	res, err := l.svcCtx.BizRedis.EvalCtx(l.ctx, LikeActionLua, []string{countKey, stateKey, userListKey, targetListKey, dislikeCountKey, authorTotalKey},
		in.TargetId, fmt.Sprintf("%d", in.UserId), fmt.Sprintf("%d", in.ActionType), fmt.Sprintf("%d", score))
	if err != nil {
		span.RecordError(err)
		metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "redis_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, err)
		return nil, errmsg.NewGrpcErr(errmsg.ErrorRedisUpdate, "Redis更新失败")
	}
	var resInt int64
	if res != nil {
		switch v := res.(type) {
		case int64:
			resInt = v
		case int:
			resInt = int64(v)
		}
	}
	if resInt == 0 {
		metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "idempotent").Inc()
	} else {
		isFirst := false
		if resInt == 2 {
			isFirst = true
		}
		msgId, err := snowflake.GetID()
		if err != nil {
			span.RecordError(err)
			metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "snowflake_error").Inc()
		}

		msg := &types.KafkaLikeMsg{
			MsgID:      strconv.FormatInt(msgId, 10),
			UserId:     in.UserId,
			TargetType: in.TargetType,
			TargetId:   in.TargetId,
			AuthorID:   in.AuthorId,
			State:      int32(in.ActionType),
			IsFirst:    isFirst,
			CreatedAt:  time.Now().Unix(),
		}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			span.RecordError(err)
			metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "json_error").Inc()
			logger.LogBusinessErr(l.ctx, errmsg.ErrorJsonMarshal, err)

		} else {
			err = l.svcCtx.KafkaPusher.Push(l.ctx, string(msgBytes))
			if err != nil {
				//在大厂，这里通常会把发失败的消息写到本地磁盘(兜底日志)，等 Kafka 活了再补发。
				//目前先个重度告警日志！
				span.RecordError(err)
				metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "kafka_error").Inc()
				logger.LogBusinessErr(l.ctx, errmsg.ErrorKafkaPush, fmt.Errorf("Kafka消息发送失败 : %v, 丢失消息 :%s", err, string(msgBytes)))
			} else {
				metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "success").Inc()
				logger.LogInfo(l.ctx, "Kafka推送异步点赞消息成功")
			}
		}
		logger.LogInfo(l.ctx, "like action success")
	}
	likecountStr, err := l.svcCtx.BizRedis.HgetCtx(l.ctx, countKey, in.TargetId)
	var finallikeCount int64
	if err == nil && likecountStr != "" {
		parsedCount, err := strconv.ParseInt(likecountStr, 10, 64)
		if err == nil {
			finallikeCount = parsedCount
		} else {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorTypeTransfer, err)
		}
	} else {
		span.RecordError(err)
		metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "redis_read_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	}

	dislikecountStr, err := l.svcCtx.BizRedis.HgetCtx(l.ctx, dislikeCountKey, in.TargetId)
	var finalDislikeCount int64
	if err == nil && dislikecountStr != "" {
		parsedCount, err := strconv.ParseInt(dislikecountStr, 10, 64)
		if err == nil {
			finalDislikeCount = parsedCount
		} else {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorTypeTransfer, err)
		}
	} else {
		span.RecordError(err)
		metrics.LikeActionCount.WithLabelValues(in.TargetType, actionStr, "redis_read_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	}

	if resInt != 0 &&
		in.ActionType == pb.ActionType_ACTION_LIKE &&
		strings.EqualFold(in.TargetType, "article") {
		finallikeCount = l.currentArticleLikeTotal(in.TargetType, in.TargetId, finallikeCount)
	}

	if resInt != 0 &&
		in.ActionType == pb.ActionType_ACTION_LIKE &&
		strings.EqualFold(in.TargetType, "article") {
		l.syncTaskProgress(in, userListKey, finallikeCount)
	}
	if resInt != 0 &&
		in.ActionType == pb.ActionType_ACTION_LIKE &&
		strings.EqualFold(in.TargetType, "article") &&
		in.AuthorId > 0 &&
		in.UserId != in.AuthorId {
		l.notifyArticleLiked(in)
	}

	return &pb.LikeActionResp{
		Success:      true,
		Message:      "action success",
		LikeCount:    finallikeCount,
		DislikeCount: finalDislikeCount,
	}, nil
}

func (l *LikeActionLogic) syncTaskProgress(in *pb.LikeActionReq, userListKey string, articleLikes int64) {
	if l.svcCtx.TaskUserPusher != nil {
		totalLikesGiven, err := l.currentUserLikeTotal(in.UserId, in.TargetType, userListKey)
		if err != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, fmt.Errorf("sync task user progress failed: %w", err))
		} else if totalLikesGiven > 0 {
			if err := l.svcCtx.TaskUserPusher.PushWithKey(
				l.ctx,
				strconv.FormatInt(in.UserId, 10),
				strconv.FormatInt(totalLikesGiven, 10),
			); err != nil {
				logger.LogBusinessErr(l.ctx, errmsg.ErrorKafkaPush, fmt.Errorf("push task user progress failed: %w", err))
			}
		}
	}

	if l.svcCtx.TaskArticlePusher == nil || in.AuthorId <= 0 || articleLikes <= 0 {
		return
	}

	payload, err := json.Marshal(types.TaskArticleProgressMsg{
		UserID: strconv.FormatInt(in.AuthorId, 10),
		Cur:    articleLikes,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorJsonMarshal, fmt.Errorf("marshal task article progress failed: %w", err))
		return
	}

	if err := l.svcCtx.TaskArticlePusher.PushWithKey(l.ctx, in.TargetId, string(payload)); err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorKafkaPush, fmt.Errorf("push task article progress failed: %w", err))
	}
}

func (l *LikeActionLogic) currentUserLikeTotal(userID int64, targetType string, userListKey string) (int64, error) {
	total, err := l.svcCtx.BizRedis.ZcardCtx(l.ctx, userListKey)
	if err != nil {
		total = 0
	}

	dbTotal, dbErr := l.svcCtx.LikeModel.GetUserActiveLikeCount(l.ctx, userID, targetType)
	if dbErr != nil {
		if err == nil {
			return int64(total), nil
		}
		return 0, dbErr
	}

	if dbTotal > int64(total) {
		return dbTotal, nil
	}

	return int64(total), nil
}

func (l *LikeActionLogic) currentArticleLikeTotal(targetType string, targetID string, redisTotal int64) int64 {
	dbTotal, err := l.svcCtx.LikeModel.GetTargetActiveLikeCount(l.ctx, targetType, targetID)
	if err != nil {
		return redisTotal
	}

	if dbTotal > redisTotal {
		return dbTotal
	}

	return redisTotal
}

func (l *LikeActionLogic) notifyArticleLiked(in *pb.LikeActionReq) {
	_, err := l.svcCtx.MessageRpc.SendNotification(l.ctx, &messagepb.SendNotificationReq{
		RecipientIds: []int64{in.AuthorId},
		Broadcast:    false,
		SenderId:     in.UserId,
		SenderRole:   messagepb.SenderRole_USER,
		Kind:         messagepb.NotificationKind_ARTICLE_LIKED,
		Title:        "文章收获新点赞",
		Content:      fmt.Sprintf("用户 %d 点赞了你的文章。", in.UserId),
		Extra: map[string]string{
			"target_type": in.TargetType,
			"target_id":   in.TargetId,
			"liker_id":    fmt.Sprintf("%d", in.UserId),
			"author_id":   fmt.Sprintf("%d", in.AuthorId),
		},
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, fmt.Errorf("send article liked notification failed: %w", err))
	}
}
