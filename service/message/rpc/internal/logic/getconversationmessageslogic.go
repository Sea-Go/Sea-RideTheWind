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

type GetConversationMessagesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetConversationMessagesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConversationMessagesLogic {
	return &GetConversationMessagesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetConversationMessagesLogic) GetConversationMessages(in *pb.ConversationMessageListReq) (*pb.ConversationMessageListResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("GetConversationMessages", started, err)
	}()

	if in.OperatorId <= 0 || in.ConversationId <= 0 || !validRole(in.OperatorRole) {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	conversation, err := l.svcCtx.MessageModel.FindConversationByID(l.ctx, in.ConversationId)
	if err != nil {
		if errors.Is(err, model.ErrRecordNotFound) {
			err = errWithBizCode(codes.NotFound, messagecommon.ErrorConversationMiss)
			return nil, err
		}
		metrics.ObserveDBError("conversation", "find")
		return nil, err
	}

	if !isParticipant(conversation, in.OperatorRole, in.OperatorId) {
		err = errWithBizCode(codes.PermissionDenied, messagecommon.ErrorForbidden)
		return nil, err
	}

	limit := normalizeLimit(in.Limit, l.svcCtx.Config.List.DefaultLimit, l.svcCtx.Config.List.MaxLimit)
	items, err := l.svcCtx.MessageModel.ListConversationMessages(l.ctx, in.ConversationId, in.Offset, limit)
	if err != nil {
		metrics.ObserveDBError("conversation", "list_messages")
		return nil, err
	}

	respItems := make([]*pb.ChatMessageItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, toMessageItem(item))
	}

	return &pb.ConversationMessageListResp{
		Code:              int32(messagecommon.Success),
		Msg:               messagecommon.GetErrMsg(messagecommon.Success),
		Items:             respItems,
		Status:            pb.ConversationStatus(conversation.Status),
		CanSend:           canSend(conversation, in.OperatorRole, in.OperatorId),
		PendingSenderId:   conversation.PendingSenderID,
		PendingSenderRole: pb.SenderRole(conversation.PendingSenderRole),
	}, nil
}
