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

type ListConversationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListConversationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListConversationsLogic {
	return &ListConversationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListConversationsLogic) ListConversations(in *pb.ConversationListReq) (*pb.ConversationListResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("ListConversations", started, err)
	}()

	if in.OperatorId <= 0 || !validRole(in.OperatorRole) {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	limit := normalizeLimit(in.Limit, l.svcCtx.Config.List.DefaultLimit, l.svcCtx.Config.List.MaxLimit)
	items, total, err := l.svcCtx.MessageModel.ListConversations(l.ctx, int32(in.OperatorRole), in.OperatorId, in.Offset, limit)
	if err != nil {
		metrics.ObserveDBError("conversation", "list")
		return nil, err
	}

	unreadTotal, err := l.svcCtx.MessageModel.CountUnreadConversationMessages(l.ctx, int32(in.OperatorRole), in.OperatorId)
	if err != nil {
		metrics.ObserveDBError("conversation", "count_unread")
		return nil, err
	}

	respItems := make([]*pb.ConversationItem, 0, len(items))
	for _, item := range items {
		respItem, buildErr := buildConversationItem(l.ctx, l.svcCtx, item, in.OperatorRole, in.OperatorId)
		if buildErr != nil {
			err = buildErr
			return nil, err
		}
		respItems = append(respItems, respItem)
	}

	return &pb.ConversationListResp{
		Code:       int32(messagecommon.Success),
		Msg:        messagecommon.GetErrMsg(messagecommon.Success),
		Items:      respItems,
		Total:      total,
		UnreadCount: unreadTotal,
	}, nil
}
