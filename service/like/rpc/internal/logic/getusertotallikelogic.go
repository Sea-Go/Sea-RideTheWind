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

type GetUserTotalLikeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserTotalLikeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserTotalLikeLogic {
	return &GetUserTotalLikeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserTotalLikeLogic) GetUserTotalLike(in *pb.GetUserTotalLikeReq) (*pb.GetUserTotalLikeResp, error) {
	span := trace.SpanFromContext(l.ctx)
	span.SetAttributes(
		attribute.Int64("query.user_id", in.UserId),
	)

	if in.UserId <= 0 {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorInputWrong, fmt.Errorf("非法的用户ID"))
		return nil, errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "非法的用户ID")
	}

	redisKey := fmt.Sprintf("user_total_like:%d", in.UserId)
	val, err := l.svcCtx.BizRedis.GetCtx(l.ctx, redisKey)

	if err != nil {
		span.RecordError(err)
		metrics.LikeQueryCacheCount.WithLabelValues("user_total", "redis_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	} else if val != "" {
		span.SetAttributes(attribute.String("cache.status", "hit"))
		metrics.LikeQueryCacheCount.WithLabelValues("user_total", "hit").Inc()
		count, _ := strconv.ParseInt(val, 10, 64)

		ttl := l.svcCtx.Config.Storage.Redis.CacheTTL
		_ = l.svcCtx.BizRedis.ExpireCtx(l.ctx, redisKey, int(ttl))

		logger.LogInfo(l.ctx, "get user total like count success (from cache)")
		return &pb.GetUserTotalLikeResp{
			TotalLikeCount: count,
		}, nil
	}
	span.SetAttributes(attribute.String("cache.status", "miss"))
	metrics.LikeQueryCacheCount.WithLabelValues("user_total", "miss").Inc()
	totalCount, err := l.svcCtx.LikeModel.GetTotalLikeCount(l.ctx, in.UserId)
	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
	}

	ttl := l.svcCtx.Config.Storage.Redis.CacheTTL
	err = l.svcCtx.BizRedis.SetexCtx(l.ctx, redisKey, strconv.FormatInt(totalCount, 10), ttl)

	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, fmt.Errorf("写入Redis出错,原因:%v", err))
	}

	logger.LogInfo(l.ctx, "get user total like count success (from db)")
	return &pb.GetUserTotalLikeResp{
		TotalLikeCount: totalCount,
	}, nil
}
