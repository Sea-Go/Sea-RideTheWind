package logic

import (
	"context"
	"fmt"
	"math"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetUserLikeListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserLikeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLikeListLogic {
	return &GetUserLikeListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserLikeListLogic) GetUserLikeList(in *pb.GetUserLikeListReq) (*pb.GetUserLikeListResp, error) {
	span := trace.SpanFromContext(l.ctx)
	span.SetAttributes(
		attribute.Int64("query.user_id", in.UserId),
		attribute.String("query.target_type", in.TargetType),
		attribute.Int64("query.cursor", in.Cursor),
		attribute.Int64("query.page_size", int64(in.PageSize)),
	)
	limit := int(in.PageSize)
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	fetchLimit := limit + 1
	redisKey := fmt.Sprintf("user_like_list:%d:%s", in.UserId, in.TargetType)
	var maxScore int64
	if in.Cursor > 0 {
		maxScore = in.Cursor - 1
	} else {
		maxScore = math.MaxInt64
	}
	res, err := l.svcCtx.BizRedis.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, redisKey, 0, maxScore, 0, fetchLimit)
	if err != nil {
		span.RecordError(err)
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
	}
	var list []*pb.LikeRecordItem
	var nextCursor int64
	isEnd := true
	if err == nil && len(res) > 0 {
		if len(res) > limit {
			isEnd = false
			res = res[:limit]
		}
		for _, pair := range res {
			score := pair.Score
			targetId := pair.Key
			realTimestamp := score >> 22
			list = append(list, &pb.LikeRecordItem{
				TargetId:   targetId,
				TargetType: in.TargetType,
				Timestamp:  realTimestamp,
			})
			nextCursor = score
		}
	}
	if len(list) > 0 {
		span.SetAttributes(attribute.String("cache.status", "hit"))
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "hit").Inc()
	}
	if len(list) == 0 {
		span.SetAttributes(attribute.String("cache.status", "miss"))
		metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "miss").Inc()
		l.Infof("触发DB回源,UserId:%v", in.UserId)
		dbResults, dbErr := l.svcCtx.LikeModel.GetUserLikeList(l.ctx, in.UserId, in.TargetType, in.Cursor, fetchLimit)
		if dbErr != nil {
			span.RecordError(dbErr)
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, dbErr)
			return nil, errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		}
		if len(dbResults) > limit {
			isEnd = false
		}
		for i, r := range dbResults {
			var timestamp int64 = r.CreateTime
			if timestamp == 0 {
				logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, fmt.Errorf("发现缺少时间戳的脏数据,记录ID:%v,作品类型:%v,作品ID:%v", r.Id, r.TargetType, r.TargetId))
				continue
			}
			score := timestamp << 22
			_, addErr := l.svcCtx.BizRedis.ZaddCtx(l.ctx, redisKey, score, r.TargetId)
			if addErr != nil {
				span.RecordError(addErr)
				metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Inc()
				logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, addErr)
			}
			if i < limit {
				list = append(list, &pb.LikeRecordItem{
					TargetId:   r.TargetId,
					TargetType: r.TargetType,
					Timestamp:  timestamp,
				})
				nextCursor = score
			}

		}
		if len(dbResults) > 0 {
			_ = l.svcCtx.BizRedis.ExpireCtx(l.ctx, redisKey, 3600*24*7)
		}

	}
	if isEnd {
		nextCursor = 0
	}
	logger.LogInfo(l.ctx, "get user like list success")
	return &pb.GetUserLikeListResp{
		List:       list,
		IsEnd:      isEnd,
		NextCursor: nextCursor,
	}, nil
}
