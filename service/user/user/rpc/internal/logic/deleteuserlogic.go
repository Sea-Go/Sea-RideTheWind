package logic

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserLogic {
	return &DeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteUserLogic) DeleteUser(in *pb.DeleteUserReq) (*pb.DeleteUserResp, error) {

	tracer := otel.Tracer("user-rpc")
	spanCtx, span := tracer.Start(l.ctx, "Action-User-Delete")
	defer span.End()

	span.SetAttributes(attribute.Int64("biz-uid", in.Uid))

	_, err := l.svcCtx.UserModel.FindOneByUid(spanCtx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			logger.LogBusinessErr(spanCtx, errmsg.ErrorUserNotExist, err)
			return &pb.DeleteUserResp{
				Success: false,
			}, status.Error(codes.NotFound, "用户不存在")
		}
		span.RecordError(err)
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbSelect, err)
		return &pb.DeleteUserResp{
			Success: false,
		}, status.Error(codes.Internal, "DB查询失败")
	}
	err = l.svcCtx.UserModel.DeleteUserByUid(spanCtx, in.Uid)
	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbUpdate, err)
		return &pb.DeleteUserResp{
			Success: false,
		}, status.Error(codes.Internal, "DB更新失败")
	}
	logger.LogInfo(spanCtx, "delete success")
	return &pb.DeleteUserResp{
		Success: true,
	}, nil
}
