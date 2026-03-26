package mqs

import (
	"context"
	"fmt"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/metrics"
	"sea-try-go/service/common/logger"
)

type ArticleSyncOutboxSender struct {
	svcCtx *svc.ServiceContext
}

func NewArticleSyncOutboxSender(svcCtx *svc.ServiceContext) *ArticleSyncOutboxSender {
	return &ArticleSyncOutboxSender{svcCtx: svcCtx}
}

func (s *ArticleSyncOutboxSender) SendPending(ctx context.Context, limit int) error {
	maxRetry := s.svcCtx.Config.ArticleSyncOutbox.MaxRetry
	events, err := s.svcCtx.ArticleSyncOutbox.FetchPending(ctx, limit, maxRetry)
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, fmt.Errorf("fetch article sync outbox failed: %w", err))
		return err
	}

	for _, event := range events {
		pushErr := s.svcCtx.ArticleSyncPusher.PushWithKey(ctx, event.AggregateID, event.Payload)
		if pushErr != nil {
			metrics.KafkaPushErrors.WithLabelValues("article_sync_event").Inc()
			logger.LogBusinessErr(ctx, errmsg.Error, fmt.Errorf("push article sync event failed: %w", pushErr), logger.WithArticleID(event.AggregateID))
			if markErr := s.svcCtx.ArticleSyncOutbox.MarkFailed(ctx, event.EventID, pushErr.Error()); markErr != nil {
				logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("mark article sync outbox failed: %w", markErr), logger.WithArticleID(event.AggregateID))
			}
			continue
		}

		if err := s.svcCtx.ArticleSyncOutbox.MarkSent(ctx, event.EventID); err != nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("mark article sync outbox sent failed: %w", err), logger.WithArticleID(event.AggregateID))
		}
	}

	return nil
}
