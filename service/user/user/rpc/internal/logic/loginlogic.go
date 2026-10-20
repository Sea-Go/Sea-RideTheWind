package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
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

type LoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LoginLogic) Login(in *pb.LoginReq) (*pb.LoginResp, error) {

	tracer := otel.Tracer("user-rpc")
	spanCtx, span := tracer.Start(l.ctx, "Action-User-Login")
	defer span.End()
	span.SetAttributes(attribute.String("biz.username", in.Username))

	user, err := l.svcCtx.UserModel.FindOneByUserName(spanCtx, in.Username)

	if err != nil {
		if err == model.ErrorNotFound {
			metrics.UserLoginCount.WithLabelValues("user_not_found").Inc()
			logger.LogInfo(spanCtx, fmt.Sprintf("login failed : user not found,username : %s", in.Username))
			return &pb.LoginResp{
				Status: 1,
			}, nil
		}
		metrics.UserLoginCount.WithLabelValues("db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}

	correct := cryptx.CheckPassword(user.Password, in.Password)
	if !correct {
		metrics.UserLoginCount.WithLabelValues("internal_error").Inc()
		logger.LogInfo(spanCtx, fmt.Sprintf("login failed: username or password incorrect,username : %s", in.Username))
		return &pb.LoginResp{
			Status: 1,
		}, nil
	}
	if user.Status == 1 {
		logger.LogInfo(spanCtx, fmt.Sprintf("login failed: user banned, username :  %s", in.Username))
		return &pb.LoginResp{
			Status: 2,
		}, nil
	}
	metrics.UserLoginCount.WithLabelValues("success").Inc()
	logger.LogInfo(spanCtx, fmt.Sprintf("login success,username : %s", in.Username))
	return &pb.LoginResp{
		Uid:    user.Uid,
		Status: 0,
	}, nil
}
