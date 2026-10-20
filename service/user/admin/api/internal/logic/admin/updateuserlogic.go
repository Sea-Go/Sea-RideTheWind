// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"encoding/json"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

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
	var adminUid int64
	if uidNumber, ok := l.ctx.Value("uid").(json.Number); ok {
		adminUid, _ = uidNumber.Int64()
	}
	rpcReq := &pb.UpdateUserReq{
		Uid:        req.Uid,
		Username:   req.Username,
		Password:   req.Password,
		Email:      req.Email,
		ExtraInfo:  req.Extrainfo,
		OperatorId: adminUid,
	}

	rpcResp, err := l.svcCtx.AdminRpc.UpdateUser(l.ctx, rpcReq)
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/updateuser", "db_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.AlreadyExists:
			return nil, errmsg.ErrorUserExist
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	metrics.AdminApiInterceptCount.WithLabelValues("/admin/updateuser", "success").Inc()
	return &types.UpdateUserResp{
		Success: true,
		User: types.UserInfo{
			Uid:       rpcResp.User.Uid,
			Username:  rpcResp.User.Username,
			Email:     rpcResp.User.Email,
			Status:    int64(rpcResp.User.Status),
			Extrainfo: rpcResp.User.ExtraInfo,
		},
	}, errmsg.Success
}
