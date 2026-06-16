package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/points/rpc/internal/metrics"
	"sea-try-go/service/points/rpc/internal/model"
	"sea-try-go/service/points/rpc/internal/svc"
	"sea-try-go/service/user/common/errmsg"
)

type RetryHandler struct {
	svcCtx *svc.ServiceContext
}

func NewRetryHandler(svcCtx *svc.ServiceContext) *RetryHandler {
	return &RetryHandler{svcCtx: svcCtx}
}

func (h *RetryHandler) Consume(body []byte) {
	_ = observability.TraceConsumer(context.Background(), "points-rpc", "RetryHandler.Consume", pointsMessageAttrs("points_retry", ""), func(ctx context.Context) error {
		return h.consume(ctx, body)
	})
}

func (h *RetryHandler) consume(ctx context.Context, body []byte) error {
	msg := &UserPointsMsg{}
	if err := json.Unmarshal(body, msg); err != nil {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "json_unmarshal").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorJsonUnmarshal, err)
		return err
	}

	points, err := h.svcCtx.PointsModel.FindByAccountIdAndUserId(ctx, msg.AccountId, msg.UserId)
	if err != nil || points == nil {
		if err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "db_select").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
			return err
		}
		return nil
	}
	if points.Status == model.StatusSuccess || points.Status == model.StatusFailed {
		return nil
	}

	if msg.RetryTimes > 3 {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "retry_exceeded").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorPointsRetryExceeded,
			fmt.Errorf("uid=%d retry exceeded: %d", msg.Uid, msg.RetryTimes),
			logger.WithUserID(fmt.Sprintf("%d", msg.UserId)))
		err = h.svcCtx.PointsModel.UpdateStatusByUid(ctx, msg.Uid, model.StatusFailed)
		if err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "status_update").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
			return err
		}
		return nil
	}

	ok, err := h.svcCtx.PointsModel.UpdateUserPoints(ctx, msg.UserId, msg.Amount)
	if err != nil {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "db_update").Inc()
		msg.RetryTimes += 1
		bytes, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "json_marshal").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorJsonMarshal, marshalErr)
			return marshalErr
		}
		if _, delayErr := h.svcCtx.RetryDqPusherClient.Delay(bytes, time.Second*3); delayErr != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "delay_push").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDelayMsg, delayErr)
			return delayErr
		}
		return err
	}

	if !ok {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "points_insufficient").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorPointsInsufficient,
			fmt.Errorf("userId=%d points not enough", msg.UserId),
			logger.WithUserID(fmt.Sprintf("%d", msg.UserId)))
		if err = h.svcCtx.PointsModel.UpdateStatusByUid(ctx, msg.Uid, model.StatusFailed); err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_consume", "status_update").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
			return err
		}
	}
	return nil
}
