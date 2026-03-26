package logic

import (
	"context"
	"errors"
	"time"

	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type MarkNotificationReadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMarkNotificationReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkNotificationReadLogic {
	return &MarkNotificationReadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *MarkNotificationReadLogic) MarkNotificationRead(in *pb.MarkNotificationReq) (*pb.BaseResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("MarkNotificationRead", started, err)
	}()

	if in.UserId <= 0 || in.NotificationId <= 0 {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	err = l.svcCtx.MessageModel.MarkNotificationRead(l.ctx, in.UserId, in.NotificationId)
	if err != nil {
		if errors.Is(err, model.ErrRecordNotFound) {
			err = errWithBizCode(codes.NotFound, messagecommon.ErrorNotificationMiss)
			return nil, err
		}
		metrics.ObserveDBError("notification", "mark_read")
		return nil, err
	}

	return successBaseResp(), nil
}
