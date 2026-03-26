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

type UnblockLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnblockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnblockLogic {
	return &UnblockLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UnblockLogic) Unblock(in *pb.RelationReq) (resp *pb.BaseResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("Unblock", started, err)
		metrics.ObserveRelation("unblock", err)
	}()

	if err = validateRelationReq(in); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr := l.svcCtx.FollowModel.DeleteBlock(l.ctx, in.GetUserId(), in.GetTargetId()); dbErr != nil {
		metrics.ObserveDBError("unblock", "delete_block")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbUpdate)
	}

	logger.LogInfo(l.ctx, "block relation removed", userLogOption(in.GetUserId()))
	return successBaseResp(), nil
}
