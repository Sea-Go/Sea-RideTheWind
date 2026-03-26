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

type GetRecommendationsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRecommendationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecommendationsLogic {
	return &GetRecommendationsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRecommendationsLogic) GetRecommendations(req *types.FollowListReq) (resp *types.RecommendListResp, err error) {
	const route = "/follow/v1/recommendations"
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
	if queryUserID != currentUserID {
		metrics.ObserveReject(route, "forbidden_scope")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorForbidden, followcommon.NewErrCode(followcommon.ErrorForbidden), userLogOption(currentUserID))
		return nil, followcommon.NewErrCode(followcommon.ErrorForbidden)
	}

	rpcResp, rpcErr := l.svcCtx.FollowRpc.GetRecommendations(l.ctx, &followservice.ListReq{
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

	users := make([]types.RecommendUser, 0, len(rpcResp.Users))
	for _, user := range rpcResp.Users {
		if user == nil {
			continue
		}
		users = append(users, types.RecommendUser{
			TargetId:    user.TargetId,
			MutualScore: user.MutualScore,
		})
	}

	return &types.RecommendListResp{Users: users}, nil
}
