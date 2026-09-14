// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"
	"errors"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/common/errmsg"
	"sea-try-go/service/user/user/api/internal/identity"
	"sea-try-go/service/user/user/api/internal/svc"
	"sea-try-go/service/user/user/api/internal/types"

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

	user, err := identity.ResolveUser(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		if errors.Is(err, identity.ErrInvalidClaim) {
			return nil, errmsg.ErrorTokenRuntime
		}
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, errmsg.ErrorUserNotExist
		}
		if errors.Is(err, identity.ErrIdentityMismatch) {
			return nil, errmsg.ErrorServerCommon
		}
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
			Uid:       user.Uid,
			Username:  user.Username,
			Email:     user.Email,
			Extrainfo: user.ExtraInfo,
		},
		Found: true,
	}, errmsg.Success
}
