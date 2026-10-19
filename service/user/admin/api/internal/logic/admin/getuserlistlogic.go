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

type GetuserlistLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetuserlistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetuserlistLogic {
	return &GetuserlistLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetuserlistLogic) Getuserlist(req *types.GetUserListReq) (resp *types.GetUserListResp, code int) {
	rpcReq := &pb.GetUserListReq{
		Page:     req.Page,
		PageSize: req.PageSize,
		Keyword:  req.Keyword,
	}

	rpcResp, err := l.svcCtx.AdminRpc.GetUserList(l.ctx, rpcReq)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	var list []types.UserInfo
	if rpcResp.List != nil {
		for _, v := range rpcResp.List {
			list = append(list, types.UserInfo{
				Uid:       v.Uid,
				Username:  v.Username,
				Email:     v.Email,
				Status:    int64(v.Status),
				Extrainfo: v.ExtraInfo,
			})
		}
	}
	return &types.GetUserListResp{
		List:  list,
		Total: rpcResp.Total,
	}, errmsg.Success
}
