// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RegisterLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegisterLogic) Register(req *types.CreateUserReq) (resp *types.CreateUserResp, code int) {

	rpcReq := &pb.CreateUserReq{
		Username:  req.Username,
		Password:  req.Password,
		Email:     req.Email,
		ExtraInfo: req.Extrainfo,
	}

	rpcResp, err := l.svcCtx.UserRpc.Register(l.ctx, rpcReq)

	if err != nil {
		metrics.UserApiInterceptCount.WithLabelValues("/user/register", "rpc_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		case codes.AlreadyExists:
			return nil, errmsg.ErrorUserExist
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	metrics.UserApiInterceptCount.WithLabelValues("/user/register", "success").Inc()
	return &types.CreateUserResp{
		Uid: rpcResp.Uid,
	}, errmsg.Success
}
