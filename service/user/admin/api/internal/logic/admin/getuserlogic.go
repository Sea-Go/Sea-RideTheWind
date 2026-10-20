// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetuserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetuserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetuserLogic {
	return &GetuserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetuserLogic) Getuser(req *types.GetUserReq) (resp *types.GetUserResp, code int) {

	rpcReq := &pb.GetUserReq{
		Uid: req.Uid,
	}
	rpcResp, err := l.svcCtx.AdminRpc.GetUser(l.ctx, rpcReq)
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
	return &types.GetUserResp{
		User: types.UserInfo{
			Uid:       rpcResp.User.Uid,
			Username:  rpcResp.User.Username,
			Email:     rpcResp.User.Email,
			Status:    int64(rpcResp.User.Status),
			Extrainfo: rpcResp.User.ExtraInfo,
		},
		Found: true,
	}, errmsg.Success
}
