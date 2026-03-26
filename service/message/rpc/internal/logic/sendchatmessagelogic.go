package logic

import (
	"context"
	"errors"
	"time"

	"sea-try-go/service/common/snowflake"
	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type SendChatMessageLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendChatMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendChatMessageLogic {
	return &SendChatMessageLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendChatMessageLogic) SendChatMessage(in *pb.SendChatMessageReq) (*pb.SendChatMessageResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("SendChatMessage", started, err)
		metrics.ObserveChat("send", err)
	}()

	content := trimContent(in.Content)
	if in.SenderId <= 0 || content == "" || !validRole(in.SenderRole) {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	var conversation *model.Conversation
	recipientID := in.RecipientId
	recipientRole := in.RecipientRole

	if in.ConversationId > 0 {
		conversation, err = l.svcCtx.MessageModel.FindConversationByID(l.ctx, in.ConversationId)
		if err != nil {
			if errors.Is(err, model.ErrRecordNotFound) {
				err = errWithBizCode(codes.NotFound, messagecommon.ErrorConversationMiss)
				return nil, err
			}
			metrics.ObserveDBError("chat", "find_conversation")
			return nil, err
		}
		if !isParticipant(conversation, in.SenderRole, in.SenderId) {
			err = errWithBizCode(codes.PermissionDenied, messagecommon.ErrorForbidden)
			return nil, err
		}
		var ok bool
		recipientRole, recipientID, ok = peerOf(conversation, in.SenderRole, in.SenderId)
		if !ok {
			err = errWithBizCode(codes.PermissionDenied, messagecommon.ErrorForbidden)
			return nil, err
		}
	} else {
		if recipientID <= 0 || !validRole(recipientRole) {
			err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
			return nil, err
		}
		if recipientID == in.SenderId && recipientRole == in.SenderRole {
			err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorSelfChat)
			return nil, err
		}

		role1, id1, role2, id2 := canonicalParticipants(in.SenderRole, in.SenderId, recipientRole, recipientID)
		conversation, err = l.svcCtx.MessageModel.FindConversationByParticipants(l.ctx, int32(role1), id1, int32(role2), id2)
		if err != nil && !errors.Is(err, model.ErrRecordNotFound) {
			metrics.ObserveDBError("chat", "find_by_pair")
			return nil, err
		}
		if errors.Is(err, model.ErrRecordNotFound) {
			conversation = nil
			err = nil
		}
	}

	now := time.Now()
	messageID, idErr := snowflake.GetID()
	if idErr != nil {
		err = idErr
		return nil, err
	}

	messageItem := &model.ConversationMessage{
		ID:            messageID,
		SenderID:      in.SenderId,
		SenderRole:    int32(in.SenderRole),
		RecipientID:   recipientID,
		RecipientRole: int32(recipientRole),
		Content:       content,
		IsRead:        false,
		CreatedAt:     now,
	}

	if conversation == nil {
		if in.SenderRole == pb.SenderRole_ADMIN {
			if recipientRole != pb.SenderRole_USER {
				err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorUnsupportedRole)
				return nil, err
			}
			conversationID, convErr := snowflake.GetID()
			if convErr != nil {
				err = convErr
				return nil, err
			}
			role1, id1, role2, id2 := canonicalParticipants(in.SenderRole, in.SenderId, recipientRole, recipientID)
			conversation = &model.Conversation{
				ID:                 conversationID,
				Participant1Role:   int32(role1),
				Participant1ID:     id1,
				Participant2Role:   int32(role2),
				Participant2ID:     id2,
				Status:             int32(pb.ConversationStatus_OPEN),
				PendingSenderRole:  int32(pb.SenderRole_SENDER_ROLE_UNSPECIFIED),
				PendingSenderID:    0,
				CreatedByRole:      int32(in.SenderRole),
				CreatedByID:        in.SenderId,
				LastMessageID:      messageID,
				LastMessagePreview: l.svcCtx.MessageModel.TrimPreview(content),
				LastMessageAt:      now,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			if err = l.svcCtx.MessageModel.CreateConversationWithMessageTx(l.ctx, conversation, messageItem); err != nil {
				metrics.ObserveDBError("chat", "create_conversation")
				return nil, err
			}
			return &pb.SendChatMessageResp{
				Code:           int32(messagecommon.Success),
				Msg:            messagecommon.GetErrMsg(messagecommon.Success),
				ConversationId: conversation.ID,
				Message:        toMessageItem(*messageItem),
				Status:         pb.ConversationStatus_OPEN,
				CanSend:        true,
			}, nil
		}

		if in.SenderRole != pb.SenderRole_USER || recipientRole != pb.SenderRole_USER {
			err = errWithBizCode(codes.PermissionDenied, messagecommon.ErrorAdminOnlyInit)
			return nil, err
		}
		if runeLen(content) > 50 {
			err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorFirstMsgTooLong)
			return nil, err
		}

		conversationID, convErr := snowflake.GetID()
		if convErr != nil {
			err = convErr
			return nil, err
		}
		role1, id1, role2, id2 := canonicalParticipants(in.SenderRole, in.SenderId, recipientRole, recipientID)
		conversation = &model.Conversation{
			ID:                 conversationID,
			Participant1Role:   int32(role1),
			Participant1ID:     id1,
			Participant2Role:   int32(role2),
			Participant2ID:     id2,
			Status:             int32(pb.ConversationStatus_PENDING),
			PendingSenderRole:  int32(in.SenderRole),
			PendingSenderID:    in.SenderId,
			CreatedByRole:      int32(in.SenderRole),
			CreatedByID:        in.SenderId,
			LastMessageID:      messageID,
			LastMessagePreview: l.svcCtx.MessageModel.TrimPreview(content),
			LastMessageAt:      now,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err = l.svcCtx.MessageModel.CreateConversationWithMessageTx(l.ctx, conversation, messageItem); err != nil {
			metrics.ObserveDBError("chat", "create_pending_conversation")
			return nil, err
		}
		return &pb.SendChatMessageResp{
			Code:           int32(messagecommon.Success),
			Msg:            messagecommon.GetErrMsg(messagecommon.Success),
			ConversationId: conversation.ID,
			Message:        toMessageItem(*messageItem),
			Status:         pb.ConversationStatus_PENDING,
			CanSend:        false,
		}, nil
	}

	statusAfterSend := pb.ConversationStatus(conversation.Status)
	pendingSenderRole := pb.SenderRole(conversation.PendingSenderRole)
	pendingSenderID := conversation.PendingSenderID

	if statusAfterSend == pb.ConversationStatus_PENDING {
		if pendingSenderRole == in.SenderRole && pendingSenderID == in.SenderId {
			err = errWithBizCode(codes.FailedPrecondition, messagecommon.ErrorChatPending)
			return nil, err
		}
		statusAfterSend = pb.ConversationStatus_OPEN
		pendingSenderRole = pb.SenderRole_SENDER_ROLE_UNSPECIFIED
		pendingSenderID = 0
	}
	if statusAfterSend == pb.ConversationStatus_CONVERSATION_STATUS_UNSPECIFIED {
		statusAfterSend = pb.ConversationStatus_OPEN
	}

	if err = l.svcCtx.MessageModel.CreateOrSendMessageTx(
		l.ctx,
		conversation,
		messageItem,
		int32(statusAfterSend),
		int32(pendingSenderRole),
		pendingSenderID,
		l.svcCtx.MessageModel.TrimPreview(content),
	); err != nil {
		metrics.ObserveDBError("chat", "append_message")
		return nil, err
	}

	return &pb.SendChatMessageResp{
		Code:           int32(messagecommon.Success),
		Msg:            messagecommon.GetErrMsg(messagecommon.Success),
		ConversationId: conversation.ID,
		Message:        toMessageItem(*messageItem),
		Status:         statusAfterSend,
		CanSend:        statusAfterSend == pb.ConversationStatus_OPEN,
	}, nil
}
