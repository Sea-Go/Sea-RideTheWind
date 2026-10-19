package logic

import (
	"context"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/svc"
	pb "github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AddPointsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAddPointsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddPointsLogic {
	return &AddPointsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *AddPointsLogic) AddPoints(in *pb.AddPointsReq) (*pb.AddPointsResp, error) {
	start := time.Now()
	resultLabel := "ok"
	defer func() {
		metrics.PointsRequestCounterMetric.WithLabelValues("points_rpc", "AddPoints", resultLabel).Inc()
		metrics.PointsRequestSecondsCounterMetric.WithLabelValues("points_rpc", "AddPoints").Add(time.Since(start).Seconds())
	}()

	result, err := ProcessPoints(l.ctx, l.svcCtx, in.UserId, in.RequestId, in.AddPoints)
	if err != nil {
		resultLabel = "sys_fail"
		return nil, err
	}
	return &pb.AddPointsResp{Success: result.Success, Message: result.Message}, nil
}
