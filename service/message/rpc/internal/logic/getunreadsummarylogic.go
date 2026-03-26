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

type GetUnreadSummaryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUnreadSummaryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUnreadSummaryLogic {
	return &GetUnreadSummaryLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUnreadSummaryLogic) GetUnreadSummary(in *pb.UnreadSummaryReq) (*pb.UnreadSummaryResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("GetUnreadSummary", started, err)
	}()

	if in.UserId <= 0 {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	notificationUnread, err := l.svcCtx.MessageModel.CountNotifications(l.ctx, in.UserId, true)
	if err != nil {
		metrics.ObserveDBError("unread", "notification")
		return nil, err
	}

	conversationUnread, err := l.svcCtx.MessageModel.CountUnreadConversationMessages(l.ctx, int32(pb.SenderRole_USER), in.UserId)
	if err != nil {
		metrics.ObserveDBError("unread", "conversation")
		return nil, err
	}

	return &pb.UnreadSummaryResp{
		Code:               int32(messagecommon.Success),
		Msg:                messagecommon.GetErrMsg(messagecommon.Success),
		NotificationUnread: notificationUnread,
		ConversationUnread: conversationUnread,
		TotalUnread:        notificationUnread + conversationUnread,
	}, nil
}
