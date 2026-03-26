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

type UnfollowLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnfollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnfollowLogic {
	return &UnfollowLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UnfollowLogic) Unfollow(in *pb.RelationReq) (resp *pb.BaseResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("Unfollow", started, err)
		metrics.ObserveRelation("unfollow", err)
	}()

	if err = validateRelationReq(in); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr := l.svcCtx.FollowModel.DeleteFollow(l.ctx, in.GetUserId(), in.GetTargetId()); dbErr != nil {
		metrics.ObserveDBError("unfollow", "delete_follow")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbUpdate)
	}

	logger.LogInfo(l.ctx, "follow relation removed", userLogOption(in.GetUserId()))
	return successBaseResp(), nil
}
