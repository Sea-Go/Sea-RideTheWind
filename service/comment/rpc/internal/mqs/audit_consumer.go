package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	kqtypes "sea-try-go/service/comment/rpc/common/types"
	"sea-try-go/service/comment/rpc/internal/svc"
	messagepb "sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuditConsumer struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAuditConsumer(ctx context.Context, svcCtx *svc.ServiceContext) *AuditConsumer {
	return &AuditConsumer{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *AuditConsumer) Consume(ctx context.Context, key, val string) error {
	var msg kqtypes.CommentKafkaMsg
	if err := json.Unmarshal([]byte(val), &msg); err != nil {
		l.Errorf("parse comment kafka message failed: %v", err)
		return nil
	}

	isSensitive, hitWord := l.svcCtx.SensitiveFilter.Match(msg.Content)
	status := 0
	if isSensitive {
		l.Infof("comment %d matched sensitive word %q", msg.CommentId, hitWord)
		status = 1
	}

	if err := l.svcCtx.CommentModel.InsertCommentTx(ctx, msg, status); err != nil {
		l.Errorf("persist comment %d failed: %v", msg.CommentId, err)
		return err
	}
	if err := l.svcCtx.CommentCache.InvalidateTargetCaches(ctx, msg.TargetType, msg.TargetId); err != nil {
		l.Errorf("invalidate comment target cache failed, comment %d: %v", msg.CommentId, err)
	}
	if msg.ParentId != 0 {
		if err := l.svcCtx.CommentCache.DeleteCommentIndexCache(ctx, msg.ParentId); err != nil {
			l.Errorf("invalidate parent comment cache failed, parent %d: %v", msg.ParentId, err)
		}
	}

	if status == 0 {
		l.notifyComment(ctx, msg)
	}

	l.Infof("comment pipeline finished, id=%d, status=%d", msg.CommentId, status)
	return nil
}

func (l *AuditConsumer) notifyComment(ctx context.Context, msg kqtypes.CommentKafkaMsg) {
	recipientID := msg.OwnerId
	kind := messagepb.NotificationKind_ARTICLE_COMMENT
	title := "文章收到新评论"
	content := fmt.Sprintf("你的文章收到了一条新评论：%s", previewComment(msg.Content))

	if msg.ParentId != 0 {
		parent, err := l.svcCtx.CommentModel.FindOneCommentById(ctx, msg.ParentId)
		if err != nil {
			l.Errorf("query parent comment %d for notification failed: %v", msg.ParentId, err)
			return
		}
		recipientID = parent.UserId
		kind = messagepb.NotificationKind_COMMENT_REPLY
		title = "评论收到新回复"
		content = fmt.Sprintf("你的评论收到了新的回复：%s", previewComment(msg.Content))
	}

	if recipientID <= 0 || recipientID == msg.UserId {
		return
	}

	_, err := l.svcCtx.MessageRpc.SendNotification(ctx, &messagepb.SendNotificationReq{
		RecipientIds: []int64{recipientID},
		Broadcast:    false,
		SenderId:     msg.UserId,
		SenderRole:   messagepb.SenderRole_USER,
		Kind:         kind,
		Title:        title,
		Content:      content,
		Extra: map[string]string{
			"target_type": msg.TargetType,
			"target_id":   msg.TargetId,
			"comment_id":  fmt.Sprintf("%d", msg.CommentId),
			"root_id":     fmt.Sprintf("%d", msg.RootId),
			"parent_id":   fmt.Sprintf("%d", msg.ParentId),
		},
	})
	if err != nil {
		l.Errorf("send comment notification failed, comment %d: %v", msg.CommentId, err)
	}
}

func previewComment(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "(no content)"
	}
	if utf8.RuneCountInString(trimmed) <= 40 {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:40]) + "..."
}
