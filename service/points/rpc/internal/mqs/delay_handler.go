package mqs

import (
	"context"
	"encoding/json"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/points/rpc/internal/metrics"
	"sea-try-go/service/points/rpc/internal/model"
	"sea-try-go/service/points/rpc/internal/svc"
	"sea-try-go/service/user/common/errmsg"
)

type DelayHandler struct {
	svcCtx *svc.ServiceContext
}

func NewDelayHandler(svcCtx *svc.ServiceContext) *DelayHandler {
	return &DelayHandler{svcCtx: svcCtx}
}

func (h *DelayHandler) Consume(body []byte) {
	_ = observability.TraceConsumer(context.Background(), "points-rpc", "DelayHandler.Consume", pointsMessageAttrs("points_delay", ""), func(ctx context.Context) error {
		return h.consume(ctx, body)
	})
}

func (h *DelayHandler) consume(ctx context.Context, body []byte) error {
	var uid int64
	if err := json.Unmarshal(body, &uid); err != nil {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "delay_consume", "json_unmarshal").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorJsonUnmarshal, err)
		return err
	}

	points, err := h.svcCtx.PointsModel.FindOneByUid(ctx, uid)
	if err != nil || points == nil {
		if err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "delay_consume", "db_select").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
			return err
		}
		return nil
	}
	if points.Status == model.StatusSuccess || points.Status == model.StatusFailed {
		return nil
	}

	metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "delay_consume", "timeout").Inc()
	timeoutErr := fmt.Errorf("uid=%d points process timeout after 15 minutes", uid)
	observability.MarkTimeout(ctx, timeoutErr, 0, pointsMessageAttrs("points_delay", "")...)
	logger.LogBusinessErr(ctx, errmsg.ErrorPointsTimeout,
		timeoutErr,
		logger.WithUserID(fmt.Sprintf("%d", points.UserId)),
	)
	if err = h.svcCtx.PointsModel.UpdateStatusByUid(ctx, uid, model.StatusFailed); err != nil {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "delay_consume", "status_update").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithUserID(fmt.Sprintf("%d", points.UserId)))
		return err
	}
	return nil
}
