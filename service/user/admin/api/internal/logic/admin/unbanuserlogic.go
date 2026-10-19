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

type UnbanuserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnbanuserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbanuserLogic {
	return &UnbanuserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbanuserLogic) Unbanuser(req *types.UnBanUserReq) (resp *types.UnBanUserResp, code int) {
	var adminUid int64
	if uidNumber, ok := l.ctx.Value("uid").(json.Number); ok {
		adminUid, _ = uidNumber.Int64()
	}
	rpcReq := &pb.UnBanUserReq{
		Uid:        req.Uid,
		OperatorId: adminUid,
	}
	rpcResp, err := l.svcCtx.AdminRpc.UnBanUser(l.ctx, rpcReq)
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/unbanuser", "rpc_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorDbSelect
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	metrics.AdminApiInterceptCount.WithLabelValues("/admin/unbanuser", "success").Inc()
	return &types.UnBanUserResp{
		Success: rpcResp.Success,
	}, errmsg.Success
}
