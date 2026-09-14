package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/like/common/errmsg"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/like/rpc/internal/mq/internal/metrics"
	"sea-try-go/service/like/rpc/internal/mq/internal/svc"
	"sea-try-go/service/like/rpc/internal/types"
)

type LikeUpdateService struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type likeMessage struct {
	MsgID      string
	Topic      string
	Consumer   string
	Record     *model.LikeRecord
	IsFirst    bool
	OccurredAt int64
}

func buildFirstLikeOutbox(ctx context.Context, task *likeMessage) (*model.LikeOutboxEvent, error) {
	if !(task.Record.State == 1 && task.IsFirst) {
		return nil, nil
	}

	event := types.ArticleHotEvent{
		ArticleID: task.Record.TargetID,
		Type:      "like",
		UserId:    fmt.Sprintf("%d", task.Record.UserID),
		Timestamp: time.Now().Unix(),
		IsFirst:   true,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	eventKey := fmt.Sprintf("first_like:%d:%s:%s",
		task.Record.UserID,
		task.Record.TargetType,
		task.Record.TargetID,
	)
	eventID, err := snowflake.GetID()
	if err != nil {
		metrics.ConsumeLikeMsgCount.WithLabelValues("flush", "snowflake_error").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorSnowflakeID, err)
		return nil, err
	}

	return &model.LikeOutboxEvent{
		EventID:     strconv.FormatInt(eventID, 10),
		EventKey:    eventKey,
		EventType:   "article_hot_like",
		AggregateID: task.Record.TargetID,
		Payload:     string(payload),
		Status:      0,
	}, nil
}

func NewLikeUpdateService(ctx context.Context, svcCtx *svc.ServiceContext) *LikeUpdateService {
	return &LikeUpdateService{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LikeUpdateService) Consume(ctx context.Context, key, val string) error {
	return observability.TraceConsumer(ctx, "like-mq", "LikeUpdateService.Consume", likeMessageAttrs("like-topic", key), func(ctx context.Context) error {
		var msg types.KafkaLikeMsg
		if err := json.Unmarshal([]byte(val), &msg); err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "json_error").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorJsonUnmarshal, err)
			return nil
		}

		operationID, parseErr := strconv.ParseInt(msg.MsgID, 10, 64)
		if parseErr != nil || operationID <= 0 || msg.UserId <= 0 || msg.TargetType == "" || msg.TargetId == "" || msg.State < 1 || msg.State > 4 {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "empty_msg_id").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("invalid like message fields"))
			return nil
		}

		record := &model.LikeRecord{
			UserID:     msg.UserId,
			TargetType: msg.TargetType,
			TargetID:   msg.TargetId,
			AuthorID:   msg.AuthorID,
			State:      msg.State,
		}

		task := &likeMessage{
			MsgID:      msg.MsgID,
			Topic:      "like-topic",
			Consumer:   "like_update_service",
			Record:     record,
			IsFirst:    msg.IsFirst,
			OccurredAt: msg.CreatedAt,
		}

		outbox, err := buildFirstLikeOutbox(ctx, task)
		if err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "outbox_error").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorBuildOutbox, err)
			return err
		}
		payload := &model.LikeProcessPayload{
			Inbox:  &model.LikeConsumeInbox{MsgId: task.MsgID, Topic: task.Topic, Consumer: task.Consumer},
			Record: task.Record, Outbox: outbox, OccurredAt: task.OccurredAt, IsFirst: task.IsFirst,
		}
		// Kafka can acknowledge only after inbox, record and domain fact commit.
		if err := l.svcCtx.LikeRecordModel.ProcessLikeMessageBatch(ctx, []*model.LikeProcessPayload{payload}); err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "tx_error").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, fmt.Errorf("like message %s commit failed: %w", msg.MsgID, err))
			return err
		}
		metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "committed").Inc()
		return nil
	})
}

func likeMessageAttrs(topic, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", topic),
		attribute.String("messaging.operation", "consume"),
		attribute.String("messaging.message.key", key),
	}
}
