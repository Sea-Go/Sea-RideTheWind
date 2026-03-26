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

type ListNotificationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListNotificationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListNotificationsLogic {
	return &ListNotificationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListNotificationsLogic) ListNotifications(in *pb.NotificationListReq) (*pb.NotificationListResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("ListNotifications", started, err)
	}()

	if in.UserId <= 0 {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	limit := normalizeLimit(in.Limit, l.svcCtx.Config.List.DefaultLimit, l.svcCtx.Config.List.MaxLimit)
	items, total, unread, err := l.svcCtx.MessageModel.ListNotifications(l.ctx, in.UserId, in.Offset, limit, in.UnreadOnly)
	if err != nil {
		metrics.ObserveDBError("notification", "list")
		return nil, err
	}

	respItems := make([]*pb.NotificationItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, toNotificationItem(item))
	}

	return &pb.NotificationListResp{
		Code:       int32(messagecommon.Success),
		Msg:        messagecommon.GetErrMsg(messagecommon.Success),
		Items:      respItems,
		Total:      total,
		UnreadCount: unread,
	}, nil
}
