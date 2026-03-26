package follow

import (
	"context"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/follow/api/internal/metrics"
	"sea-try-go/service/follow/api/internal/svc"
	"sea-try-go/service/follow/api/internal/types"
	followcommon "sea-try-go/service/follow/common"
	"sea-try-go/service/follow/rpc/followservice"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnfollowLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnfollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnfollowLogic {
	return &UnfollowLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnfollowLogic) Unfollow(req *types.RelationActionReq) (resp *types.RelationActionResp, err error) {
	const route = "/follow/v1/unfollow"
	started := time.Now()
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	userID, err := extractUserID(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "user_id_missing")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorUnauthorized, err)
		return nil, followcommon.NewErrCode(followcommon.ErrorUnauthorized)
	}

	rpcResp, rpcErr := l.svcCtx.FollowRpc.Unfollow(l.ctx, &followservice.RelationReq{
		UserId:   userID,
		TargetId: req.TargetId,
	})
	if rpcErr != nil {
		code := codeFromRPCError(rpcErr)
		logger.LogBusinessErr(l.ctx, code, rpcErr, userLogOption(userID))
		return nil, followcommon.NewErrCode(code)
	}
	if rpcResp == nil {
		logger.LogBusinessErr(l.ctx, followcommon.ErrorServerCommon, followcommon.NewErrCode(followcommon.ErrorServerCommon), userLogOption(userID))
		return nil, followcommon.NewErrCode(followcommon.ErrorServerCommon)
	}
	if rpcResp.Code != int32(followcommon.Success) {
		return nil, followcommon.NewErrCodeMsg(int(rpcResp.Code), rpcResp.Msg)
	}

	return &types.RelationActionResp{Success: true}, nil
}
