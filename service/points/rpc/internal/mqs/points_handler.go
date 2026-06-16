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

	"go.opentelemetry.io/otel/attribute"
)

type UserPointsMsg struct {
	Uid        int64 `json:"uid"`
	AccountId  int64 `json:"accountId"`
	UserId     int64 `json:"userId"`
	Amount     int32 `json:"amount"`
	RetryTimes int32 `json:"retryTimes"`
}

type PointsHandler struct {
	svcCtx *svc.ServiceContext
}

func NewPointsHandler(svcCtx *svc.ServiceContext) *PointsHandler {
	return &PointsHandler{svcCtx: svcCtx}
}

func (p *PointsHandler) Consume(ctx context.Context, key, value string) error {
	return observability.TraceConsumer(ctx, "points-rpc", "PointsHandler.Consume", pointsMessageAttrs("points", key), func(ctx context.Context) error {
		msg := &UserPointsMsg{}
		if err := json.Unmarshal([]byte(value), &msg); err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_consume", "json_unmarshal").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorJsonUnmarshal, err)
			return err
		}

		points, err := p.svcCtx.PointsModel.FindByAccountIdAndUserId(ctx, msg.AccountId, msg.UserId)
		if err != nil || points == nil {
			if err != nil {
				metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_consume", "db_select").Inc()
				logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
			}
			return err
		}
		if points.Status == model.StatusSuccess || points.Status == model.StatusFailed {
			return nil
		}

		ok, err := p.svcCtx.PointsModel.UpdateUserPoints(ctx, msg.UserId, msg.Amount)
		if err != nil {
			metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_consume", "db_update").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithUserID(fmt.Sprintf("%d", msg.UserId)))
			_, err := p.svcCtx.RetryDqPusherClient.Delay([]byte(value), time.Second*3)
			if err != nil {
				metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "retry_delay", "delay_push").Inc()
				logger.LogBusinessErr(ctx, errmsg.ErrorDelayMsg, err)
				return err
			}
			return nil
		}

		if !ok {
			err = p.svcCtx.PointsModel.UpdateStatusByUid(ctx, msg.Uid, model.StatusFailed)
			if err != nil {
				metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_consume", "status_update").Inc()
				logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithUserID(fmt.Sprintf("%d", msg.UserId)))
				return err
			}
		}

		return nil
	})
}

func pointsMessageAttrs(topic, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", topic),
		attribute.String("messaging.operation", "consume"),
		attribute.String("messaging.message.key", key),
	}
}
