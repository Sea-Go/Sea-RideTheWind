package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sea-try-go/service/common/snowflake"
	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"
	adminpb "sea-try-go/service/user/admin/rpc/pb"
	userpb "sea-try-go/service/user/user/rpc/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func successBaseResp() *pb.BaseResp {
	return &pb.BaseResp{
		Code: int32(messagecommon.Success),
		Msg:  messagecommon.GetErrMsg(messagecommon.Success),
	}
}

func validRole(role pb.SenderRole) bool {
	return role == pb.SenderRole_USER || role == pb.SenderRole_ADMIN
}

func normalizeLimit(limit, defaultLimit, maxLimit int32) int32 {
	if limit <= 0 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func trimContent(content string) string {
	return strings.TrimSpace(content)
}

func runeLen(content string) int {
	return len([]rune(content))
}

func canonicalParticipants(role1 pb.SenderRole, id1 int64, role2 pb.SenderRole, id2 int64) (pb.SenderRole, int64, pb.SenderRole, int64) {
	if role1 < role2 {
		return role1, id1, role2, id2
	}
	if role1 > role2 {
		return role2, id2, role1, id1
	}
	if id1 <= id2 {
		return role1, id1, role2, id2
	}
	return role2, id2, role1, id1
}

func isParticipant(conv *model.Conversation, role pb.SenderRole, id int64) bool {
	return (conv.Participant1Role == int32(role) && conv.Participant1ID == id) ||
		(conv.Participant2Role == int32(role) && conv.Participant2ID == id)
}

func peerOf(conv *model.Conversation, role pb.SenderRole, id int64) (pb.SenderRole, int64, bool) {
	if conv.Participant1Role == int32(role) && conv.Participant1ID == id {
		return pb.SenderRole(conv.Participant2Role), conv.Participant2ID, true
	}
	if conv.Participant2Role == int32(role) && conv.Participant2ID == id {
		return pb.SenderRole(conv.Participant1Role), conv.Participant1ID, true
	}
	return pb.SenderRole_SENDER_ROLE_UNSPECIFIED, 0, false
}

func canSend(conv *model.Conversation, role pb.SenderRole, id int64) bool {
	if conv == nil {
		return false
	}
	if pb.ConversationStatus(conv.Status) == pb.ConversationStatus_OPEN {
		return true
	}
	if pb.ConversationStatus(conv.Status) != pb.ConversationStatus_PENDING {
		return false
	}
	return !(conv.PendingSenderRole == int32(role) && conv.PendingSenderID == id)
}

func notificationKindLabel(kind pb.NotificationKind) string {
	return strings.ToLower(kind.String())
}

func errWithBizCode(grpcCode codes.Code, bizCode int) error {
	return messagecommon.GRPCError(grpcCode, bizCode)
}

func toNotificationItem(item model.Notification) *pb.NotificationItem {
	return &pb.NotificationItem{
		Id:          item.ID,
		RecipientId: item.RecipientID,
		SenderId:    item.SenderID,
		SenderRole:  pb.SenderRole(item.SenderRole),
		Kind:        pb.NotificationKind(item.Kind),
		Title:       item.Title,
		Content:     item.Content,
		IsRead:      item.IsRead,
		CreatedAt:   item.CreatedAt.Unix(),
		Extra:       mapString(item.Extra),
	}
}

func toMessageItem(item model.ConversationMessage) *pb.ChatMessageItem {
	return &pb.ChatMessageItem{
		Id:             item.ID,
		ConversationId: item.ConversationID,
		SenderId:       item.SenderID,
		SenderRole:     pb.SenderRole(item.SenderRole),
		RecipientId:    item.RecipientID,
		RecipientRole:  pb.SenderRole(item.RecipientRole),
		Content:        item.Content,
		IsRead:         item.IsRead,
		CreatedAt:      item.CreatedAt.Unix(),
	}
}

func mapString(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func loadProfile(ctx context.Context, svcCtx *svc.ServiceContext, role pb.SenderRole, id int64) (string, string) {
	switch role {
	case pb.SenderRole_USER:
		resp, err := svcCtx.UserRpc.GetUser(ctx, &userpb.GetUserReq{Uid: id})
		if err == nil && resp != nil && resp.Found && resp.User != nil {
			return resp.User.Username, resp.User.Email
		}
		return fmt.Sprintf("User %d", id), ""
	case pb.SenderRole_ADMIN:
		resp, err := svcCtx.AdminRpc.GetSelf(ctx, &adminpb.GetSelfReq{Uid: id})
		if err == nil && resp != nil && resp.Admin != nil {
			return resp.Admin.Username, resp.Admin.Email
		}
		return fmt.Sprintf("Admin %d", id), ""
	default:
		return fmt.Sprintf("Unknown %d", id), ""
	}
}

func buildConversationItem(ctx context.Context, svcCtx *svc.ServiceContext, conv model.Conversation, operatorRole pb.SenderRole, operatorID int64) (*pb.ConversationItem, error) {
	peerRole, peerID, ok := peerOf(&conv, operatorRole, operatorID)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, messagecommon.GetErrMsg(messagecommon.ErrorForbidden))
	}

	unread, err := svcCtx.MessageModel.CountUnreadMessagesByConversation(ctx, conv.ID, int32(operatorRole), operatorID)
	if err != nil {
		metrics.ObserveDBError("conversation", "count_unread")
		return nil, err
	}

	name, email := loadProfile(ctx, svcCtx, peerRole, peerID)
	return &pb.ConversationItem{
		ConversationId: conv.ID,
		PeerId:         peerID,
		PeerRole:       peerRole,
		PeerName:       name,
		PeerEmail:      email,
		Status:         pb.ConversationStatus(conv.Status),
		UnreadCount:    unread,
		LatestMessage:  conv.LastMessagePreview,
		LatestMessageAt: conv.LastMessageAt.Unix(),
		UpdatedAt:      conv.UpdatedAt.Unix(),
		CanSend:        canSend(&conv, operatorRole, operatorID),
	}, nil
}

func collectBroadcastRecipients(ctx context.Context, svcCtx *svc.ServiceContext) ([]int64, error) {
	pageSize := svcCtx.Config.Broadcast.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}

	recipients := make([]int64, 0, pageSize)
	page := int64(1)
	seen := make(map[int64]struct{})
	for {
		resp, err := svcCtx.AdminRpc.GetUserList(ctx, &adminpb.GetUserListReq{
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.List) == 0 {
			break
		}
		for _, item := range resp.List {
			if item == nil || item.Uid <= 0 {
				continue
			}
			if _, ok := seen[item.Uid]; ok {
				continue
			}
			seen[item.Uid] = struct{}{}
			recipients = append(recipients, item.Uid)
		}
		if int64(len(resp.List)) < pageSize {
			break
		}
		page++
	}

	return recipients, nil
}

func dedupeRecipients(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func newNotificationRecord(recipientID int64, senderID int64, senderRole pb.SenderRole, kind pb.NotificationKind, title, content string, extra map[string]string, now time.Time) (*model.Notification, error) {
	id, err := snowflake.GetID()
	if err != nil {
		return nil, err
	}
	return &model.Notification{
		ID:          id,
		RecipientID: recipientID,
		SenderID:    senderID,
		SenderRole:  int32(senderRole),
		Kind:        int32(kind),
		Title:       title,
		Content:     content,
		IsRead:      false,
		Extra:       model.JSONMap(mapString(extra)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func safeStatusBiz(err error, notFoundCode int) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, model.ErrRecordNotFound) {
		return errWithBizCode(codes.NotFound, notFoundCode)
	}
	return err
}
