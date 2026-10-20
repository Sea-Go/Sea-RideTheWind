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

type UpdateUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserLogic {
	return &UpdateUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateUserLogic) UpdateUser(in *pb.UpdateUserReq) (*pb.UpdateUserResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-UpdateUser")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.OperatorId),
		attribute.Int64("audit.target_uid", in.Uid),
	)
	_, err := l.svcCtx.AdminModel.FindOneUserByUid(ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("update", "user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}
		metrics.AdminActionCount.WithLabelValues("update", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}
	toUpdate := &model.User{}
	if len(in.Username) > 0 {
		existUser, err := l.svcCtx.AdminModel.FindOneUserByUsername(ctx, in.Username)
		if err == nil && in.Uid != existUser.Uid {
			logger.LogBusinessErr(ctx, errmsg.ErrorUserExist, err)
			return nil, status.Error(codes.AlreadyExists, "用户名已存在")
		}
		if err != nil && err != model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("update", "db_error").Inc()
			span.RecordError(err)
			logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
			return nil, status.Error(codes.Internal, "DB查询失败")
		}
		toUpdate.Username = in.Username
	}
	if len(in.Password) > 0 {
		newPassword, e := cryptx.PasswordEncrypt(in.Password)
		if e != nil {

			metrics.AdminActionCount.WithLabelValues("update", "internal_error").Inc()
			span.RecordError(e)
			logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, e)
			return nil, status.Error(codes.Internal, "密码加密失败")
		}
		toUpdate.Password = newPassword
	}
	if len(in.Email) > 0 {
		toUpdate.Email = in.Email
	}
	if in.ExtraInfo != nil {
		toUpdate.ExtraInfo = in.ExtraInfo
	}
	err = l.svcCtx.AdminModel.UpdateOneUserByUid(ctx, in.Uid, toUpdate)
	if err != nil {
		metrics.AdminActionCount.WithLabelValues("update", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}
	var newUser *model.User
	newUser, err = l.svcCtx.AdminModel.FindOneUserByUid(ctx, in.Uid)
	if err != nil {
		metrics.AdminActionCount.WithLabelValues("update", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}

	metrics.AdminActionCount.WithLabelValues("update", "success").Inc()
	logger.LogInfo(ctx, fmt.Sprintf("update success,uid : %d", in.Uid))
	return &pb.UpdateUserResp{
		User: &pb.UserInfo{
			Uid:       newUser.Uid,
			Username:  newUser.Username,
			Email:     newUser.Email,
			Status:    uint64(newUser.Status),
			ExtraInfo: newUser.ExtraInfo,
		},
	}, nil
}
