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

type DeleteuserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteuserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteuserLogic {
	return &DeleteuserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteuserLogic) Deleteuser(req *types.DeleteUserReq) (resp *types.DeleteUserResp, code int) {

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

	rpcReq := &pb.DeleteUserReq{
		Uid: uid,
	}

	rpcResp, err := l.svcCtx.UserRpc.DeleteUser(l.ctx, rpcReq)
	if err != nil || rpcResp.Success == false {
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

	return &types.DeleteUserResp{
		Success: true,
	}, errmsg.Success
}
