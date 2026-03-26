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

type BlockLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBlockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BlockLogic {
	return &BlockLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *BlockLogic) Block(in *pb.RelationReq) (resp *pb.BaseResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("Block", started, err)
		metrics.ObserveRelation("block", err)
	}()

	if err = validateRelationReq(in); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if err = ensureUserExists(l.ctx, l.svcCtx, in.GetTargetId()); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr := l.svcCtx.FollowModel.CreateBlockAndCleanup(l.ctx, in.GetUserId(), in.GetTargetId()); dbErr != nil {
		metrics.ObserveDBError("block", "create_block_and_cleanup")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbUpdate)
	}

	logger.LogInfo(l.ctx, "block relation updated", userLogOption(in.GetUserId()))
	return successBaseResp(), nil
}
