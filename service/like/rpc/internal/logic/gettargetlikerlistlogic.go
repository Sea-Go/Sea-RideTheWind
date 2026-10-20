package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/paging"
	"math"
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

type GetTargetLikerListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTargetLikerListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTargetLikerListLogic {
	return &GetTargetLikerListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetTargetLikerListLogic) GetTargetLikerList(in *pb.GetTargetLikerListReq) (*pb.GetTargetLikerListResp, error) {
	span := trace.SpanFromContext(l.ctx)

	span.SetAttributes(
		attribute.String("query.target_type", in.TargetType),
		attribute.String("query.target_id", in.TargetId),
		attribute.Int64("query.cursor", in.Cursor),
		attribute.Int64("query.page_size", int64(in.PageSize)),
	)

	limit := paging.Normalize(int(in.PageSize), 10, 100)
	fetchLimit := limit + 1
	redisKey := fmt.Sprintf("target_liker_list:%s:%s", in.TargetType, in.TargetId)
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

	var list []*pb.LikeItem
	var nextCursor int64
	isEnd := true
	if err == nil && len(res) > 0 {
		if len(res) > limit {
			isEnd = false
			res = res[:limit]
		}
		for _, pair := range res {
			score := pair.Score
			userIdStr := pair.Key
			userId, _ := strconv.ParseInt(userIdStr, 10, 64)
			realTimestamp := score >> 22
			list = append(list, &pb.LikeItem{
				UserId:    userId,
				Timestamp: realTimestamp,
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
		l.Infof("触发DB回源, Target: %s: %s", in.TargetType, in.TargetId)
		dbResults, dbErr := l.svcCtx.LikeModel.GetTargetLikerList(l.ctx, in.TargetType, in.TargetId, in.Cursor, int64(fetchLimit))

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
				logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, fmt.Errorf("发现缺少时间戳的脏数据,记录ID:%v,用户ID:%v", r.Id, r.UserId))
				continue
			}
			score := timestamp << 22
			_, addErr := l.svcCtx.BizRedis.ZaddCtx(l.ctx, redisKey, score, strconv.FormatInt(r.UserId, 10))
			if addErr != nil {
				span.RecordError(addErr)
				metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Inc()
				logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, addErr)
			}
			if i < limit {
				list = append(list, &pb.LikeItem{
					UserId:    r.UserId,
					Timestamp: timestamp,
				})
				nextCursor = score
			}
		}
		if len(dbResults) > 0 {
			_ = l.svcCtx.BizRedis.ExpireCtx(l.ctx, redisKey, 86400*7)
		}
	}
	if isEnd {
		nextCursor = 0
	}
	logger.LogInfo(l.ctx, "get target liker list success")
	return &pb.GetTargetLikerListResp{
		List:       list,
		IsEnd:      isEnd,
		NextCursor: nextCursor,
	}, nil
}
