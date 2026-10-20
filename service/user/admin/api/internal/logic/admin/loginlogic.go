// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/jwt"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (resp *types.LoginResp, code int) {
	rpcReq := &pb.LoginReq{
		Password: req.Password,
		Username: req.Username,
	}

	rpcResp, err := l.svcCtx.AdminRpc.Login(l.ctx, rpcReq)
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/login", "rpc_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		case codes.Unauthenticated:
			return nil, errmsg.ErrorLoginWrong
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	now := time.Now().Unix()
	accessSecret := l.svcCtx.Config.AdminAuth.AccessSecret
	accessExpire := l.svcCtx.Config.AdminAuth.AccessExpire
	token, err := jwt.GetToken(accessSecret, now, accessExpire, int64(rpcResp.Uid))
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/login", "internal_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.ErrorServerCommon
	}
	metrics.AdminApiInterceptCount.WithLabelValues("/admin/login", "success").Inc()
	return &types.LoginResp{
		Token: token,
	}, errmsg.Success
}
