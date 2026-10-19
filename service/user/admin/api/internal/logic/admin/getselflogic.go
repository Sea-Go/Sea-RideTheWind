// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetselfLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetselfLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetselfLogic {
	return &GetselfLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetselfLogic) Getself(req *types.GetSelfReq) (resp *types.GetSelfResp, code int) {
	userId, ok := l.ctx.Value("userId").(json.Number)
	if !ok {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("ctx userId is not json.Number"))
		return nil, errmsg.ErrorTokenRuntime
	}
	uid, err := userId.Int64()
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("parse uid failed: %v", err))
		return nil, errmsg.ErrorTokenRuntime
	}
	rpcReq := &pb.GetSelfReq{
		Uid: uid,
	}
	rpcResp, err := l.svcCtx.AdminRpc.GetSelf(l.ctx, rpcReq)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	return &types.GetSelfResp{
		Admin: types.AdminInfo{
			Uid:       rpcResp.Admin.Uid,
			Username:  rpcResp.Admin.Username,
			Email:     rpcResp.Admin.Email,
			Extrainfo: rpcResp.Admin.ExtraInfo,
		},
	}, errmsg.Success
}
