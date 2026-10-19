// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
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

type LogoutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LogoutLogic) Logout(req *types.LogoutReq) (resp *types.LogoutResp, code int) {
	token := l.ctx.Value("jwt_token")
	if token == nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenNotExist, fmt.Errorf("Token不存在"))
		return nil, errmsg.ErrorTokenNotExist
	}
	tokenString := token.(string)
	_, err := l.svcCtx.AdminRpc.Logout(l.ctx, &pb.LogoutReq{
		Token: tokenString,
	})
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
	return &types.LogoutResp{
		Success: true,
	}, errmsg.Success
}
