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

type BlockLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBlockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BlockLogic {
	return &BlockLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BlockLogic) Block(req *types.RelationActionReq) (resp *types.RelationActionResp, err error) {
	const route = "/follow/v1/block"
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

	rpcResp, rpcErr := l.svcCtx.FollowRpc.Block(l.ctx, &followservice.RelationReq{
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
