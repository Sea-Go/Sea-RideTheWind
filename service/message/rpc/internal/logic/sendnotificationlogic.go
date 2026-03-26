package logic

import (
	"context"
	"time"

	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type SendNotificationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendNotificationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendNotificationLogic {
	return &SendNotificationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendNotificationLogic) SendNotification(in *pb.SendNotificationReq) (*pb.SendNotificationResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("SendNotification", started, err)
		metrics.ObserveNotification(notificationKindLabel(in.Kind), err)
	}()

	title := trimContent(in.Title)
	content := trimContent(in.Content)
	if title == "" || content == "" {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	recipients := dedupeRecipients(in.RecipientIds)
	if in.Broadcast {
		recipients, err = collectBroadcastRecipients(l.ctx, l.svcCtx)
		if err != nil {
			metrics.ObserveDBError("notification", "broadcast_user_list")
			return nil, err
		}
	}
	if len(recipients) == 0 {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	now := time.Now()
	items := make([]*model.Notification, 0, len(recipients))
	for _, recipientID := range recipients {
		item, buildErr := newNotificationRecord(recipientID, in.SenderId, in.SenderRole, in.Kind, title, content, in.Extra, now)
		if buildErr != nil {
			err = buildErr
			return nil, err
		}
		items = append(items, item)
	}

	if err = l.svcCtx.MessageModel.CreateNotifications(l.ctx, items); err != nil {
		metrics.ObserveDBError("notification", "insert")
		return nil, err
	}

	return &pb.SendNotificationResp{
		Code:     int32(messagecommon.Success),
		Msg:      messagecommon.GetErrMsg(messagecommon.Success),
		Affected: int64(len(items)),
	}, nil
}
