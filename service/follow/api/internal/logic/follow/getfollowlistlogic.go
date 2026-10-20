package follow

import (
	"context"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/types"
	followcommon "github.com/Sea-Go/Sea-RideTheWind/service/follow/common"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/rpc/followservice"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFollowListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFollowListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFollowListLogic {
	return &GetFollowListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetFollowListLogic) GetFollowList(req *types.FollowListReq) (resp *types.FollowUserListResp, err error) {
	const route = "/follow/v1/follow/list"
	started := time.Now()
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	currentUserID, err := extractUserID(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "user_id_missing")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorUnauthorized, err)
		return nil, followcommon.NewErrCode(followcommon.ErrorUnauthorized)
	}

	queryUserID := resolveUserID(currentUserID, req.UserId)
	rpcResp, rpcErr := l.svcCtx.FollowRpc.GetFollowList(l.ctx, &followservice.ListReq{
		UserId: queryUserID,
		Offset: req.Offset,
		Limit:  req.Limit,
	})
	if rpcErr != nil {
		code := codeFromRPCError(rpcErr)
		logger.LogBusinessErr(l.ctx, code, rpcErr, userLogOption(currentUserID))
		return nil, followcommon.NewErrCode(code)
	}
	if rpcResp == nil {
		logger.LogBusinessErr(l.ctx, followcommon.ErrorServerCommon, followcommon.NewErrCode(followcommon.ErrorServerCommon), userLogOption(currentUserID))
		return nil, followcommon.NewErrCode(followcommon.ErrorServerCommon)
	}
	if rpcResp.Code != int32(followcommon.Success) {
		return nil, followcommon.NewErrCodeMsg(int(rpcResp.Code), rpcResp.Msg)
	}

	return &types.FollowUserListResp{UserIds: rpcResp.UserIds}, nil
}
