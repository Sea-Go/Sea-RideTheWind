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

type BanUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBanUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BanUserLogic {
	return &BanUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *BanUserLogic) BanUser(in *pb.BanUserReq) (*pb.BanUserResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-BanUser")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.OperatorId),
		attribute.Int64("audit.target_uid", in.Uid),
	)
	err := l.svcCtx.AdminModel.UpdateUserStatusByUid(ctx, in.Uid, 1)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("ban", "user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}
		metrics.AdminActionCount.WithLabelValues("ban", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}

	metrics.AdminActionCount.WithLabelValues("ban", "success").Inc()
	logger.LogInfo(ctx, fmt.Sprintf("ban user success,uid : %d", in.Uid))
	return &pb.BanUserResp{
		Success: true,
	}, nil
}
