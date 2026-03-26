// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/admin/api/internal/svc"
	"sea-try-go/service/user/admin/api/internal/types"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateLogic {
	return &CreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateLogic) Create(req *types.CreateAdminReq) (resp *types.CreateAdminResp, code int) {
	rpcReq := &pb.CreateAdminReq{
		Username:   req.Username,
		Password:   req.Password,
		Email:      req.Email,
		InviteCode: req.InviteCode,
		ExtraInfo:  req.Extrainfo,
	}

	rpcResp, err := l.svcCtx.AdminRpc.CreateAdmin(l.ctx, rpcReq)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.AlreadyExists:
			return nil, errmsg.ErrorUserExist
		case codes.PermissionDenied:
			return nil, errmsg.ErrorAdminInviteCodeWrong
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	return &types.CreateAdminResp{
		Uid: rpcResp.Uid,
	}, errmsg.Success
}
