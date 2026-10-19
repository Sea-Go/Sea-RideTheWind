package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UnBanUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnBanUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnBanUserLogic {
	return &UnBanUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UnBanUserLogic) UnBanUser(in *pb.UnBanUserReq) (*pb.UnBanUserResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-UnbanUser")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.OperatorId),
		attribute.Int64("audit.target_uid", in.Uid),
	)
	err := l.svcCtx.AdminModel.UpdateUserStatusByUid(ctx, in.Uid, 0)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("unban", "user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}

		metrics.AdminActionCount.WithLabelValues("unban", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}
	logger.LogInfo(ctx, fmt.Sprintf("unban user success,uid : %d", in.Uid))
	metrics.AdminActionCount.WithLabelValues("unban", "success").Inc()
	return &pb.UnBanUserResp{
		Success: true,
	}, nil
}
