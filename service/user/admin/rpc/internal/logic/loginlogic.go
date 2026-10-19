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
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-Login")
	defer span.End()
	span.SetAttributes(
		attribute.String("audit.admin_username", in.Username),
	)
	admin, err := l.svcCtx.AdminModel.FindOneAdminByUsername(ctx, in.Username)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminLoginCount.WithLabelValues("user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}

		metrics.AdminLoginCount.WithLabelValues("db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}
	correct := cryptx.CheckPassword(admin.Password, in.Password)
	if !correct {
		metrics.AdminLoginCount.WithLabelValues("wrong_answer").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorLoginWrong, fmt.Errorf("password mismatched"))
		return nil, status.Error(codes.Unauthenticated, "密码错误")
	}
	metrics.AdminLoginCount.WithLabelValues("success").Inc()
	logger.LogInfo(ctx, fmt.Sprintf("login success,username:%s", in.Username))
	return &pb.LoginResp{
		Uid: admin.Uid,
	}, nil
}
