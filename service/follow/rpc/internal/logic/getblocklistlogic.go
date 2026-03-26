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

type GetBlockListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetBlockListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBlockListLogic {
	return &GetBlockListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetBlockListLogic) GetBlockList(in *pb.ListReq) (resp *pb.UserListResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("GetBlockList", started, err)
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

	userIDs, dbErr := l.svcCtx.FollowModel.ListBlockTargets(l.ctx, userID, offset, limit)
	if dbErr != nil {
		metrics.ObserveDBError("get_block_list", "list_block_targets")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(userID))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}

	logger.LogInfo(l.ctx, "block list loaded", userLogOption(userID))
	return successUserListResp(userIDs), nil
}
