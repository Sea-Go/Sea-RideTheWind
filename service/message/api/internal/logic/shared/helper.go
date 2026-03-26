package shared

import (
	"context"
	"encoding/json"
	"fmt"

	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/api/internal/types"
	"sea-try-go/service/message/rpc/pb"

	"google.golang.org/grpc/status"
)

func UserIDFromContext(ctx context.Context) (int64, error) {
	userID, ok := ctx.Value("userId").(json.Number)
	if !ok {
		return 0, messagecommon.NewErrCode(messagecommon.ErrorUnauthorized)
	}
	uid, err := userID.Int64()
	if err != nil {
		return 0, messagecommon.NewErrCode(messagecommon.ErrorUnauthorized)
	}
	return uid, nil
}

func RPCError(err error) error {
	if err == nil {
		return nil
	}
	code := messagecommon.BizCodeFromError(err)
	if st, ok := status.FromError(err); ok && st.Message() != "" {
		return messagecommon.NewErrCodeMsg(code, st.Message())
	}
	return messagecommon.NewErrCode(code)
}

func ToNotificationListResp(in *pb.NotificationListResp) *types.NotificationListResp {
	items := make([]types.NotificationItem, 0, len(in.Items))
	for _, item := range in.Items {
		if item == nil {
			continue
		}
		items = append(items, types.NotificationItem{
			Id:          item.Id,
			RecipientId: item.RecipientId,
			SenderId:    item.SenderId,
			SenderRole:  int32(item.SenderRole),
			Kind:        int32(item.Kind),
			Title:       item.Title,
			Content:     item.Content,
			IsRead:      item.IsRead,
			CreatedAt:   item.CreatedAt,
			Extra:       item.Extra,
		})
	}
	return &types.NotificationListResp{
		Items:       items,
		Total:       in.Total,
		UnreadCount: in.UnreadCount,
	}
}

func ToUnreadSummaryResp(in *pb.UnreadSummaryResp) *types.UnreadSummaryResp {
	return &types.UnreadSummaryResp{
		NotificationUnread: in.NotificationUnread,
		ConversationUnread: in.ConversationUnread,
		TotalUnread:        in.TotalUnread,
	}
}

func ToConversationListResp(in *pb.ConversationListResp) *types.ConversationListResp {
	items := make([]types.ConversationItem, 0, len(in.Items))
	for _, item := range in.Items {
		if item == nil {
			continue
		}
		items = append(items, types.ConversationItem{
			ConversationId:  item.ConversationId,
			PeerId:          item.PeerId,
			PeerRole:        int32(item.PeerRole),
			PeerName:        item.PeerName,
			PeerEmail:       item.PeerEmail,
			Status:          int32(item.Status),
			UnreadCount:     item.UnreadCount,
			LatestMessage:   item.LatestMessage,
			LatestMessageAt: item.LatestMessageAt,
			UpdatedAt:       item.UpdatedAt,
			CanSend:         item.CanSend,
		})
	}
	return &types.ConversationListResp{
		Items:       items,
		Total:       in.Total,
		UnreadCount: in.UnreadCount,
	}
}

func ToConversationMessagesResp(in *pb.ConversationMessageListResp) *types.ConversationMessagesResp {
	items := make([]types.ChatMessageItem, 0, len(in.Items))
	for _, item := range in.Items {
		if item == nil {
			continue
		}
		items = append(items, types.ChatMessageItem{
			Id:             item.Id,
			ConversationId: item.ConversationId,
			SenderId:       item.SenderId,
			SenderRole:     int32(item.SenderRole),
			RecipientId:    item.RecipientId,
			RecipientRole:  int32(item.RecipientRole),
			Content:        item.Content,
			IsRead:         item.IsRead,
			CreatedAt:      item.CreatedAt,
		})
	}
	return &types.ConversationMessagesResp{
		Items:             items,
		Status:            int32(in.Status),
		CanSend:           in.CanSend,
		PendingSenderId:   in.PendingSenderId,
		PendingSenderRole: int32(in.PendingSenderRole),
	}
}

func ToSendChatResp(in *pb.SendChatMessageResp) *types.SendChatResp {
	message := types.ChatMessageItem{}
	if in.Message != nil {
		message = types.ChatMessageItem{
			Id:             in.Message.Id,
			ConversationId: in.Message.ConversationId,
			SenderId:       in.Message.SenderId,
			SenderRole:     int32(in.Message.SenderRole),
			RecipientId:    in.Message.RecipientId,
			RecipientRole:  int32(in.Message.RecipientRole),
			Content:        in.Message.Content,
			IsRead:         in.Message.IsRead,
			CreatedAt:      in.Message.CreatedAt,
		}
	}
	return &types.SendChatResp{
		ConversationId: in.ConversationId,
		Message:        message,
		Status:         int32(in.Status),
		CanSend:        in.CanSend,
	}
}

func AdminNotificationKind(broadcast bool, raw int32) pb.NotificationKind {
	if raw > 0 {
		return pb.NotificationKind(raw)
	}
	if broadcast {
		return pb.NotificationKind_ADMIN_BROADCAST
	}
	return pb.NotificationKind_ADMIN_NOTICE
}

func InvalidContextError(label string) error {
	return fmt.Errorf("%s not found in context", label)
}
