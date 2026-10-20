package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type ManageCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewManageCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ManageCommentLogic {
	return &ManageCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ManageCommentLogic) ManageComment(in *pb.ManageCommentReq) (resp *pb.ManageCommentResp, err error) {
	start := time.Now()
	result := "ok"

	tracer := otel.Tracer("comment-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Comment-Manage")
	defer span.End()

	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "ManageComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "ManageComment").
			Add(time.Since(start).Seconds())
	}()

	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.UserId),
		attribute.Int64("audit.comment_id", in.CommentId),
		attribute.String("audit.action_type", in.ActionType.String()),
		attribute.String("audit.target_type", in.TargetType),
		attribute.String("audit.target_id", in.TargetId),
	)

	if in.CommentId == 0 {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("评论ID不能为空"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "评论ID不能为空")
		return nil, err
	}

	var bitOffset uint
	var isSet bool
	switch in.ActionType {
	case pb.ManageType_MANAGE_PIN:
		bitOffset = 2
		isSet = true
	case pb.ManageType_MANAGE_UNPIN:
		bitOffset = 2
		isSet = false
	case pb.ManageType_MANAGE_FEATURE:
		bitOffset = 3
		isSet = true
	case pb.ManageType_MANAGE_UNFEATURE:
		bitOffset = 3
		isSet = false
	default:
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("未知操作类型"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "操作类型输入错误")
		return nil, err
	}

	err = l.svcCtx.CommentModel.ManageCommentAttribute(ctx, in.CommentId, bitOffset, isSet)
	if err != nil {
		if err == model.ErrorCommentNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorCommentNotExist, fmt.Errorf("评论不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorCommentNotExist, "评论不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbUpdate, "DB更新失败")
		return nil, err
	}

	logger.LogInfo(ctx, "manage comment success")
	resp = &pb.ManageCommentResp{
		Success: true,
	}
	return resp, nil
}
