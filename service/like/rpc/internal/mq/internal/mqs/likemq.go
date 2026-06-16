package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/like/common/errmsg"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/like/rpc/internal/mq/internal/metrics"
	"sea-try-go/service/like/rpc/internal/mq/internal/svc"
	"sea-try-go/service/like/rpc/internal/types"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/executors"
	"go.opentelemetry.io/otel/attribute"
)

type LikeUpdateService struct {
	ctx          context.Context
	svcCtx       *svc.ServiceContext
	bulkExecutor *executors.BulkExecutor
}

type LikeFlushTask struct {
	MsgID    string
	Topic    string
	Consumer string
	Record   *model.LikeRecord
	IsFirst  bool
}

func buildFirstLikeOutbox(ctx context.Context, task *LikeFlushTask) (*model.LikeOutboxEvent, error) {
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

func (l *LikeUpdateService) flush(items []any) {
	if len(items) == 0 {
		return
	}

	_ = observability.TraceConsumer(context.Background(), "like-mq", "Flush-Like-Batch", []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", "like-topic"),
		attribute.String("messaging.operation", "flush"),
		attribute.Int("batch.size", len(items)),
	}, func(ctx context.Context) error {

		var payloads []*model.LikeProcessPayload

		for _, item := range items {
			task, ok := item.(*LikeFlushTask)
			if !ok {
				logger.LogBusinessErr(ctx, errmsg.ErrorTokenTypeWrong, fmt.Errorf("传入类型错误"))
				continue
			}

			inbox := &model.LikeConsumeInbox{
				MsgId:    task.MsgID,
				Topic:    task.Topic,
				Consumer: task.Consumer,
				Status:   0,
			}

			outbox, err := buildFirstLikeOutbox(ctx, task)
			if err != nil {
				logger.LogBusinessErr(ctx, errmsg.ErrorBuildOutbox, fmt.Errorf("构建 Outbox 失败: %v", err))
				continue
			}

			payloads = append(payloads, &model.LikeProcessPayload{
				Inbox:  inbox,
				Record: task.Record,
				Outbox: outbox,
			})
		}

		if len(payloads) == 0 {
			return nil
		}

		err := l.svcCtx.LikeRecordModel.ProcessLikeMessageBatch(ctx, payloads)

		if err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("flush", "tx_error").Add(float64(len(items)))

			var failedIDs []string
			for _, p := range payloads {
				failedIDs = append(failedIDs, p.Inbox.MsgId)
			}
			logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, fmt.Errorf("本地事务闭环彻底失败，数据假死! MsgIDs: %v, err: %v", failedIDs, err))
			return err
		}

		metrics.ConsumeLikeMsgCount.WithLabelValues("flush", "success").Add(float64(len(payloads)))
		return nil
	})
}

func NewLikeUpdateService(ctx context.Context, svcCtx *svc.ServiceContext) *LikeUpdateService {
	s := &LikeUpdateService{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
	s.bulkExecutor = executors.NewBulkExecutor(s.flush, executors.WithBulkTasks(1000), executors.WithBulkInterval(time.Second))
	return s
}

func (l *LikeUpdateService) Consume(ctx context.Context, key, val string) error {
	return observability.TraceConsumer(ctx, "like-mq", "LikeUpdateService.Consume", likeMessageAttrs("like-topic", key), func(ctx context.Context) error {
		fmt.Printf("🚀 [MQ接收] 收到Kafka消息: %s\n", val)

		var msg types.KafkaLikeMsg
		if err := json.Unmarshal([]byte(val), &msg); err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "json_error").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorJsonUnmarshal, err)
			return nil
		}

		if msg.MsgID == "" {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "empty_msg_id").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("msg_id empty"))
			return nil
		}

		inbox, err := l.svcCtx.LikeConsumeInboxModel.FindByMsgID(ctx, msg.MsgID)
		if err != nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
			return err
		}
		if inbox != nil && inbox.Status == 1 {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "duplicate_skip").Inc()
			return nil
		}

		record := &model.LikeRecord{
			UserID:     msg.UserId,
			TargetType: msg.TargetType,
			TargetID:   msg.TargetId,
			AuthorID:   msg.AuthorID,
			State:      msg.State,
		}

		task := &LikeFlushTask{
			MsgID:    msg.MsgID,
			Topic:    "like-topic",
			Consumer: "like_update_service",
			Record:   record,
			IsFirst:  msg.IsFirst,
		}

		err = l.bulkExecutor.Add(task)
		if err != nil {
			metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "executor_error").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, fmt.Errorf("BulkExecutor添加记录失败, msg: %s, err: %v", val, err))
			return err
		}

		metrics.ConsumeLikeMsgCount.WithLabelValues("receive", "queued").Inc()
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
