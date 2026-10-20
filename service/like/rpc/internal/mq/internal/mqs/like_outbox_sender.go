package mqs

import (
	"context"
	"fmt"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/mq/internal/svc"
)

type LikeOutboxSender struct {
	svcCtx *svc.ServiceContext
}

func NewLikeOutboxSender(svcCtx *svc.ServiceContext) *LikeOutboxSender {
	return &LikeOutboxSender{svcCtx: svcCtx}
}

func (l *LikeOutboxSender) SendPending(ctx context.Context, limit int) error {
	events, err := l.svcCtx.LikeOutboxEventModel.FetchPending(ctx, limit)
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, fmt.Errorf("捞取 Outbox 失败: %v", err))
		return err
	}

	for _, event := range events {
		err := l.svcCtx.HotEventPusher.Push(ctx, string(event.Payload))
		if err != nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorKafkaPush, fmt.Errorf("推送热点系统失败: %v", err))
			_ = l.svcCtx.LikeOutboxEventModel.MarkFailed(ctx, event.EventID)
			continue
		}
		_ = l.svcCtx.LikeOutboxEventModel.MarkSent(ctx, event.EventID)
	}

	return nil
}
