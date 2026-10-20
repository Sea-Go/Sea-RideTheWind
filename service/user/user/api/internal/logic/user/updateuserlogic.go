// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateuserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateuserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateuserLogic {
	return &UpdateuserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateuserLogic) Updateuser(req *types.UpdateUserReq) (resp *types.UpdateUserResp, code int) {
	userId, ok := l.ctx.Value("userId").(json.Number)
	if !ok {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("ctx userId is not json.Number"))
		return nil, errmsg.ErrorTokenRuntime
	}
	uid, err := userId.Int64()

	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("parse userId to int64 failed: %v", err))
		return nil, errmsg.ErrorTokenRuntime
	}

	rpcReq := &pb.UpdateUserReq{
		Uid:       uid,
		Username:  req.Username,
		Password:  req.Password,
		Email:     req.Email,
		ExtraInfo: req.Extrainfo,
	}

	rpcResp, err := l.svcCtx.UserRpc.UpdateUser(l.ctx, rpcReq)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		case codes.AlreadyExists:
			return nil, errmsg.ErrorUserExist
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	return &types.UpdateUserResp{
		User: types.UserInfo{
			Uid:       rpcResp.User.Uid,
			Username:  rpcResp.User.Username,
			Email:     rpcResp.User.Email,
			Extrainfo: rpcResp.User.ExtraInfo,
		},
	}, errmsg.Success

}
