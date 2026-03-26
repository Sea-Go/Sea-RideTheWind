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

type FollowLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FollowLogic {
	return &FollowLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *FollowLogic) Follow(in *pb.RelationReq) (resp *pb.BaseResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("Follow", started, err)
		metrics.ObserveRelation("follow", err)
	}()

	if err = validateRelationReq(in); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if err = ensureUserExists(l.ctx, l.svcCtx, in.GetTargetId()); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	blocked, dbErr := l.svcCtx.FollowModel.ExistsAnyBlock(l.ctx, in.GetUserId(), in.GetTargetId())
	if dbErr != nil {
		metrics.ObserveDBError("follow", "exists_any_block")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}
	if blocked {
		err = followcommon.GRPCError(codes.PermissionDenied, followcommon.ErrorRelationBlocked)
		logger.LogBusinessErr(l.ctx, followcommon.ErrorRelationBlocked, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr = l.svcCtx.FollowModel.CreateFollow(l.ctx, in.GetUserId(), in.GetTargetId()); dbErr != nil {
		metrics.ObserveDBError("follow", "create_follow")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbUpdate)
	}

	logger.LogInfo(l.ctx, "follow relation updated", userLogOption(in.GetUserId()))
	return successBaseResp(), nil
}
