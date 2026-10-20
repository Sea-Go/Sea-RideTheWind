package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/snowflake"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/cryptx"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"

	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RegisterLogic) Register(in *pb.CreateUserReq) (*pb.CreateUserResp, error) {

	tracer := otel.Tracer("user-rpc")
	spanCtx, span := tracer.Start(l.ctx, "Action-User-Register")
	defer span.End()
	span.SetAttributes(attribute.String("biz.username", in.Username))

	_, err := l.svcCtx.UserModel.FindOneByUserName(spanCtx, in.Username)
	if err == nil {
		metrics.UserRegisterCount.WithLabelValues("fail_exists").Inc()
		logger.LogBusinessErr(spanCtx, errmsg.ErrorUserExist, fmt.Errorf("username has existed"))
		return nil, status.Error(codes.AlreadyExists, "用户名已存在")
	}
	if err != model.ErrorNotFound {
		metrics.UserRegisterCount.WithLabelValues("db_error").Inc()
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbSelect, err)
		span.RecordError(err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}
	truePassword, err := cryptx.PasswordEncrypt(in.Password)
	if err != nil {
		metrics.UserRegisterCount.WithLabelValues("internal_error").Inc()
		logger.LogBusinessErr(spanCtx, errmsg.ErrorServerCommon, err)
		span.RecordError(err)
		return nil, status.Error(codes.Internal, "密码生成失败")
	}

	var uid int64
	uid, err = snowflake.GetID()
	if err != nil {
		metrics.UserRegisterCount.WithLabelValues("internal_error").Inc()
		logger.LogBusinessErr(spanCtx, errmsg.ErrorServerCommon, err)
		span.RecordError(err)
		return nil, status.Error(codes.Internal, "uid生成失败")
	}
	newUser := model.User{
		Uid:       uid,
		Username:  in.Username,
		Password:  truePassword,
		Email:     in.Email,
		Score:     0,
		Status:    0,
		ExtraInfo: in.ExtraInfo,
	}
	err = l.svcCtx.UserModel.Insert(spanCtx, &newUser)
	if err != nil {
		metrics.UserRegisterCount.WithLabelValues("db_error").Inc()
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbUpdate, err)
		span.RecordError(err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}
	metrics.UserRegisterCount.WithLabelValues("success").Inc()
	logger.LogInfo(spanCtx, fmt.Sprintf("register success,uid : %d", uid))
	return &pb.CreateUserResp{
		Uid: newUser.Uid,
	}, nil
}
