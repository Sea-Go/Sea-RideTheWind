package logic

import (
	"context"
	"time"

	"sea-try-go/service/common/logger"
	followcommon "sea-try-go/service/follow/common"
	"sea-try-go/service/follow/rpc/internal/metrics"
	"sea-try-go/service/follow/rpc/internal/svc"
	"sea-try-go/service/follow/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type GetFollowListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFollowListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFollowListLogic {
	return &GetFollowListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetFollowListLogic) GetFollowList(in *pb.ListReq) (resp *pb.UserListResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("GetFollowList", started, err)
	}()

	userID, offset, limit, err := normalizeListReq(l.svcCtx.Config.List.DefaultLimit, l.svcCtx.Config.List.MaxLimit, in)
	if err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err)
		return nil, err
	}

	if err = ensureUserExists(l.ctx, l.svcCtx, userID); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(userID))
		return nil, err
	}

	userIDs, dbErr := l.svcCtx.FollowModel.ListFollowTargets(l.ctx, userID, offset, limit)
	if dbErr != nil {
		metrics.ObserveDBError("get_follow_list", "list_follow_targets")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(userID))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}

	logger.LogInfo(l.ctx, "follow list loaded", userLogOption(userID))
	return successUserListResp(userIDs), nil
}
