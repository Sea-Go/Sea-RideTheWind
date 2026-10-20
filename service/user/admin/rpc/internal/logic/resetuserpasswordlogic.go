package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/cryptx"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ResetUserPasswordLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewResetUserPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserPasswordLogic {
	return &ResetUserPasswordLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ResetUserPasswordLogic) ResetUserPassword(in *pb.ResetUserPasswordReq) (*pb.ResetUserPasswordResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-ResetUserPassword")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.OperatorId),
		attribute.Int64("audit.target_uid", in.Uid),
	)
	_, err := l.svcCtx.AdminModel.FindOneUserByUid(ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("resetpassword", "user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}
		metrics.AdminActionCount.WithLabelValues("resetpassword", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}
	var password string
	password, err = cryptx.PasswordEncrypt(l.svcCtx.Config.System.DefaultPassword)
	if err != nil {
		metrics.AdminActionCount.WithLabelValues("resetpassword", "internal_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, err)
		return nil, status.Error(codes.Internal, "密码加密失败")
	}
	err = l.svcCtx.AdminModel.UpdateUserPasswordByUid(ctx, in.Uid, password)
	if err != nil {
		metrics.AdminActionCount.WithLabelValues("resetpassword", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}
	metrics.AdminActionCount.WithLabelValues("resetpassword", "success").Inc()
	logger.LogInfo(ctx, fmt.Sprintf("reset password success,uid : %d", in.Uid))
	return &pb.ResetUserPasswordResp{
		Success: true,
	}, nil
}
