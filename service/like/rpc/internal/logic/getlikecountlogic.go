package logic

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetLikeCountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetLikeCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLikeCountLogic {
	return &GetLikeCountLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetLikeCountLogic) GetLikeCount(in *pb.GetLikeCountReq) (*pb.GetLikeCountResp, error) {
	resp := &pb.GetLikeCountResp{
		Counts: make(map[string]*pb.LikeCountItem),
	}
	span := trace.SpanFromContext(l.ctx)
	span.SetAttributes(
		attribute.String("query.target_type", in.TargetType),
		attribute.Int("query.batch_size", len(in.TargetIds)),
	)
	if len(in.TargetIds) == 0 {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorInputWrong, fmt.Errorf("查询ID为空"))
		return nil, errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "输入需要查询的ID为空")
	}

	likeCountKey := fmt.Sprintf("like_count:%s", in.TargetType)
	dislikeCountKey := fmt.Sprintf("dislike_count:%s", in.TargetType)

	likeVals, err := l.svcCtx.BizRedis.HmgetCtx(l.ctx, likeCountKey, in.TargetIds...)
	if err != nil {
		span.RecordError(err)
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Add(float64(len(in.TargetIds)))
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	}
	dislikeVals, err := l.svcCtx.BizRedis.HmgetCtx(l.ctx, dislikeCountKey, in.TargetIds...)
	if err != nil {
		span.RecordError(err)
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Add(float64(len(in.TargetIds)))
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	}

	var missingIds []string
	for i, id := range in.TargetIds {
		var likeStr, dislikeStr string
		if len(likeVals) > i {
			likeStr = likeVals[i]
		}
		if len(dislikeVals) > i {
			dislikeStr = dislikeVals[i]
		}
		if likeStr == "" {
			missingIds = append(missingIds, id)
			continue
		}
		if dislikeStr == "" {
			dislikeStr = "0"
		}
		likeCount, _ := strconv.ParseInt(likeStr, 10, 64)
		dislikeCount, _ := strconv.ParseInt(dislikeStr, 10, 64)
		resp.Counts[id] = &pb.LikeCountItem{
			LikeCount:    likeCount,
			DislikeCount: dislikeCount,
		}
	}

	hits := len(in.TargetIds) - len(missingIds)
	misses := len(missingIds)

	span.SetAttributes(
		attribute.Int("cache.hits", hits),
		attribute.Int("cache.misses", misses),
	)

	if hits > 0 {
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "hit").Add(float64(hits))
	}

	if misses > 0 {
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "miss").Add(float64(misses))
		l.Infof("触发DB消息回源,缺失数量:%d", misses)
		dbResMap, dbErr := l.svcCtx.LikeModel.GetBatchLikeCount(l.ctx, in.TargetType, missingIds)
		if dbErr != nil {
			span.RecordError(dbErr)
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, dbErr)
			return nil, errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		}

		for _, id := range missingIds {
			var likeCount, dislikeCount int64
			if counts, ok := dbResMap[id]; ok {
				likeCount = counts[1]
				dislikeCount = counts[2]
			} else {
				likeCount = 0
				dislikeCount = 0
			}
			resp.Counts[id] = &pb.LikeCountItem{
				LikeCount:    likeCount,
				DislikeCount: dislikeCount,
			}

			//trconv.FormatInt(likeCount, 10)中的10表示十进制
			if err := l.svcCtx.BizRedis.HsetCtx(l.ctx, likeCountKey, id, strconv.FormatInt(likeCount, 10)); err != nil {
				span.RecordError(err)
				logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, fmt.Errorf("写入Redis出错,错误原因:%v", err))
			}
			if err := l.svcCtx.BizRedis.HsetCtx(l.ctx, dislikeCountKey, id, strconv.FormatInt(dislikeCount, 10)); err != nil {
				span.RecordError(err)
				logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, fmt.Errorf("写入Redis出错,错误原因:%v", err))
			}
		}
	}

	logger.LogInfo(l.ctx, "get like count success")
	return resp, nil
}

/*
这意味着程序会继续往下走，vals 是空的，likeStr 也是空的，所有的 ID 都会被丢进 missingIds，然后自动降级去查询 DB！
这是一种极其优秀的“缓存容灾降级”策略，保证了 Redis 宕机时，业务依然能（勉强）运作！
*/
