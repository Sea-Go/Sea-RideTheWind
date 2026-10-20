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

type GetLikeStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetLikeStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLikeStateLogic {
	return &GetLikeStateLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetLikeStateLogic) GetLikeState(in *pb.GetLikeStateReq) (*pb.GetLikeStateResp, error) {
	resp := &pb.GetLikeStateResp{
		States: make(map[string]int32),
	}
	span := trace.SpanFromContext(l.ctx)
	span.SetAttributes(
		attribute.Int64("query.user_id", in.UserId),
		attribute.String("query.target_type", in.TargetType),
		attribute.Int("query.batch_size", len(in.TargetIds)),
	)
	if len(in.TargetIds) == 0 {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorInputWrong, fmt.Errorf("查询ID为空"))
		return nil, errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "输入需要查询的ID为空")
	}

	var missingIds []string
	for _, id := range in.TargetIds {
		stateKey := fmt.Sprintf("like_state:%s:%s", in.TargetType, id)
		filed := fmt.Sprintf("%d", in.UserId)
		stateStr, err := l.svcCtx.BizRedis.HgetCtx(l.ctx, stateKey, filed)
		if err != nil {
			span.RecordError(err)
			metrics.LikeQueryCacheCount.WithLabelValues(in.TargetType, "redis_error").Add(1)
			logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisSelect, err)
		}
		if stateStr == "" {
			missingIds = append(missingIds, id)
			continue
		}
		state, parseErr := strconv.ParseInt(stateStr, 10, 32)
		if parseErr != nil {
			// 缓存值损坏按未命中回源修复，不展示伪造的 0 状态。
			missingIds = append(missingIds, id)
			continue
		}
		resp.States[id] = int32(state)
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
		l.Infof("触发DB回源,缺失的数量为:%v", len(missingIds))
		dbRespMap, dbErr := l.svcCtx.LikeModel.GetUserBatchLikeState(l.ctx, in.UserId, in.TargetType, missingIds)
		if dbErr != nil {
			span.RecordError(dbErr)
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, dbErr)
			return nil, errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		}
		//防止内存穿透
		for _, id := range missingIds {
			//Go中如果dbRespMap没有找到id对应的值就会返回默认的零值,然后将零值存入Redis中达成防止内存穿透的目的
			//否则如果多次连续发Redis中不存在的数据,一直调用SQL,会导致很大的问题
			state := dbRespMap[id]
			resp.States[id] = state
			stateKey := fmt.Sprintf("like_state:%s:%s", in.TargetType, id)
			field := fmt.Sprintf("%d", in.UserId)
			if err := l.svcCtx.BizRedis.HsetCtx(l.ctx, stateKey, field, strconv.Itoa(int(state))); err != nil {
				span.RecordError(err)
				logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, fmt.Errorf("写入Redis失败,原因:%v", err))
			}
		}
	}

	logger.LogInfo(l.ctx, "get like state success")
	return resp, nil
}
