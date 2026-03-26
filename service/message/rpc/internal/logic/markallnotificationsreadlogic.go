package logic

import (
	"context"
	"time"

	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type MarkAllNotificationsReadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMarkAllNotificationsReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkAllNotificationsReadLogic {
	return &MarkAllNotificationsReadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *MarkAllNotificationsReadLogic) MarkAllNotificationsRead(in *pb.MarkAllNotificationsReq) (*pb.BaseResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("MarkAllNotificationsRead", started, err)
	}()

	if in.UserId <= 0 {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	if err = l.svcCtx.MessageModel.MarkAllNotificationsRead(l.ctx, in.UserId); err != nil {
		metrics.ObserveDBError("notification", "mark_all_read")
		return nil, err
	}

	return successBaseResp(), nil
}
