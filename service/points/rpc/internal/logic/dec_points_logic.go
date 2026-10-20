package logic

import (
	"context"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/svc"
	pb "github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type DecPointsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDecPointsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DecPointsLogic {
	return &DecPointsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DecPointsLogic) DecPoints(in *pb.DecPointsReq) (*pb.DecPointsResp, error) {
	start := time.Now()
	resultLabel := "ok"
	defer func() {
		metrics.PointsRequestCounterMetric.WithLabelValues("points_rpc", "DecPoints", resultLabel).Inc()
		metrics.PointsRequestSecondsCounterMetric.WithLabelValues("points_rpc", "DecPoints").Add(time.Since(start).Seconds())
	}()

	result, err := ProcessPoints(l.ctx, l.svcCtx, in.UserId, in.RequestId, -in.DecPoints)
	if err != nil {
		resultLabel = "sys_fail"
		return nil, err
	}
	return &pb.DecPointsResp{Success: result.Success, Message: result.Message}, nil
}
