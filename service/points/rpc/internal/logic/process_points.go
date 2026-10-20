package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/snowflake"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
)

// UserPointsMsg defines the kafka payload.
type UserPointsMsg struct {
	Uid        int64 `json:"uid"`
	AccountId  int64 `json:"accountId"`
	UserId     int64 `json:"userId"`
	Amount     int32 `json:"amount"`
	RetryTimes int32 `json:"retryTimes"`
}

// PointsResult is a unified result for points operations.
type PointsResult struct {
	Success bool
	Message string
}

func pointsAction(amount int32) string {
	if amount < 0 {
		return "dec"
	}
	return "add"
}

// ProcessPoints handles both add and dec points requests.
func ProcessPoints(ctx context.Context, svcCtx *svc.ServiceContext, userId int64, accountId int64, amount int32) (*PointsResult, error) {
	action := pointsAction(amount)
	resultLabel := "ok"
	defer func() {
		metrics.PointsOpsCounterMetric.WithLabelValues("points_ops", action, resultLabel).Inc()
	}()

	uid, err := snowflake.GetID()
	if err != nil {
		resultLabel = "fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorSnowflakeID, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
		return nil, err
	}

	hasProcessing, err := svcCtx.PointsModel.HasProcessingByUserId(ctx, userId)
	if err != nil {
		resultLabel = "fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
		return nil, err
	}
	if hasProcessing {
		err := svcCtx.PointsModel.Insert(ctx, &model.Points{
			Uid:       uid,
			UserId:    userId,
			AccountId: accountId,
			Amount:    amount,
			Status:    model.StatusQueued,
		})
		if err != nil {
			resultLabel = "fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
			return nil, err
		}
		return &PointsResult{Success: true, Message: "任务已排队"}, nil
	}

	tx := svcCtx.PointsModel.BeginTransaction()
	pointsLog := &model.Points{
		Uid:       uid,
		AccountId: accountId,
		UserId:    userId,
		Amount:    amount,
		Status:    model.StatusProcessing,
	}
	if err := tx.Create(pointsLog).Error; err != nil {
		resultLabel = "fail"
		tx.Rollback()
		logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
		return nil, err
	}

	for i := 0; i < 3; i++ {
		if err = sendDelayCheck(ctx, svcCtx, uid); err == nil {
			break
		}
		if i == 2 {
			resultLabel = "fail"
			tx.Rollback()
			logger.LogBusinessErr(ctx, errmsg.ErrorDelayMsg, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
			return nil, errors.New("发送延时消息失败")
		}
	}

	tx.Commit()

	hasOther, _ := svcCtx.PointsModel.HasOtherProcessingByUserId(ctx, userId, pointsLog.Uid)
	if hasOther {
		// 发现冲突，降级为 Queued
		logger.LogInfo(ctx, "并发冲突，任务降级为排队", logger.WithUserID(fmt.Sprintf("%d", userId)))
		err := svcCtx.PointsModel.UpdateStatusByUid(ctx, pointsLog.Uid, model.StatusQueued)
		if err != nil {
			resultLabel = "fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
			return nil, err
		}
		return &PointsResult{Success: true, Message: "任务已排队"}, nil
	}

	msg := UserPointsMsg{
		Uid:        uid,
		AccountId:  accountId,
		UserId:     userId,
		Amount:     amount,
		RetryTimes: 0,
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		resultLabel = "fail"
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_push", "json_marshal").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorJsonMarshal, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
		return nil, err
	}
	if err := svcCtx.KqPusherClient.Push(ctx, string(msgBytes)); err != nil {
		resultLabel = "fail"
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "kafka_push", "push_fail").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorKafkaPush, err, logger.WithUserID(fmt.Sprintf("%d", userId)))
		return nil, err
	}

	return &PointsResult{Success: true, Message: "处理中"}, nil
}

// sendDelayCheck pushes a delayed message into dq.
func sendDelayCheck(ctx context.Context, svcCtx *svc.ServiceContext, uid int64) error {
	delay := time.Minute * 15
	body := []byte(strconv.FormatInt(uid, 10))

	_, err := svcCtx.DqPusherClient.Delay(body, delay)
	if err != nil {
		metrics.PointsKafkaErrorCounterMetric.WithLabelValues("points_mq", "delay_push", "delay_push_fail").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorDelayMsg, err, logger.WithUserID(fmt.Sprintf("%d", uid)))
		return err
	}
	return nil
}
