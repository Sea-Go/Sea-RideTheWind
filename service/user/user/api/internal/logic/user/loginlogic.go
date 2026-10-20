// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/jwt"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"
	"time"

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
		Username: req.Username,
		Password: req.Password,
	}

	rpcResp, err := l.svcCtx.UserRpc.Login(l.ctx, rpcReq)

	if err != nil {
		metrics.UserApiInterceptCount.WithLabelValues("/user/login", "rpc_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	if rpcResp.Status == 1 {
		metrics.UserApiInterceptCount.WithLabelValues("/user/login", "wrong_password").Inc()
		logger.LogInfo(l.ctx, fmt.Sprintf("login failed : wrong password,username : %s", req.Username))
		return nil, errmsg.ErrorLoginWrong
	}

	if rpcResp.Status == 2 {
		metrics.UserApiInterceptCount.WithLabelValues("/user/login", "user_banned").Inc()
		logger.LogInfo(l.ctx, fmt.Sprintf("login failed : user banned,username : %s", req.Username))
		return nil, errmsg.ErrorUserBanned
	}

	now := time.Now().Unix()
	accessSecret := l.svcCtx.Config.UserAuth.AccessSecret
	accessExpire := l.svcCtx.Config.UserAuth.AccessExpire

	token, err := jwt.GetToken(accessSecret, now, accessExpire, int64(rpcResp.Uid))

	if err != nil {
		metrics.UserApiInterceptCount.WithLabelValues("/user/login", "token_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("generate token failed : %v", err))
		return nil, errmsg.ErrorTokenRuntime
	}

	metrics.UserApiInterceptCount.WithLabelValues("/user/login", "success").Inc()
	logger.LogInfo(l.ctx, fmt.Sprintf("login success,username : %s", req.Username))
	return &types.LoginResp{
		Token: token,
	}, errmsg.Success
}
