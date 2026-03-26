package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/admin/api/internal/metrics"
	"sea-try-go/service/user/admin/api/internal/svc"
	"sea-try-go/service/user/admin/api/internal/types"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateinviteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateinviteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateinviteLogic {
	return &CreateinviteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateinviteLogic) Createinvite(req *types.CreateInviteReq) (resp *types.CreateInviteResp, code int) {
	userId, ok := l.ctx.Value("userId").(json.Number)
	if !ok {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/invite/create", "token_invalid").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("ctx userId is not json.Number"))
		return nil, errmsg.ErrorTokenRuntime
	}

	uid, err := userId.Int64()
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/invite/create", "token_invalid").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("parse uid failed: %v", err))
		return nil, errmsg.ErrorTokenRuntime
	}

	rpcResp, err := l.svcCtx.AdminRpc.CreateInviteCode(l.ctx, &pb.CreateInviteCodeReq{
		OperatorId: uid,
	})
	if err != nil {
		metrics.AdminApiInterceptCount.WithLabelValues("/admin/invite/create", "rpc_error").Inc()
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

	metrics.AdminApiInterceptCount.WithLabelValues("/admin/invite/create", "success").Inc()
	return &types.CreateInviteResp{
		InviteCode: rpcResp.InviteCode,
		ExpiresAt:  rpcResp.ExpiresAt,
	}, errmsg.Success
}
