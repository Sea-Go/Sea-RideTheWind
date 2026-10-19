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

type UpdateSubjectStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateSubjectStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubjectStateLogic {
	return &UpdateSubjectStateLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateSubjectStateLogic) UpdateSubjectState(in *pb.UpdateSubjectStateReq) (resp *pb.UpdateSubjectStateResp, err error) {
	start := time.Now()
	result := "ok"

	tracer := otel.Tracer("comment-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Comment-UpdateSubjectState")
	defer span.End()

	// defer 统一埋点
	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "UpdateSubjectState", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "UpdateSubjectState").
			Add(time.Since(start).Seconds())
	}()

	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.UserId),
		attribute.String("audit.target_type", in.TargetType),
		attribute.String("audit.target_id", in.TargetId),
		attribute.Int64("audit.target_state", int64(in.State)),
	)

	// 输入校验
	if in.TargetType == "" || in.TargetId == "" {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("目标Type和ID不能为空"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "Type和ID不能为空")
		return nil, err
	}

	// 查询 subject
	subject, err := l.svcCtx.CommentModel.FindOneSubjectByTarget(ctx, in.TargetType, in.TargetId)
	if err != nil {
		if err == model.ErrorSubjectNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorSubjectNotExist, fmt.Errorf("评论区不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorSubjectNotExist, "评论区不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		return nil, err
	}

	// 权限校验
	if in.UserId != subject.OwnerId {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorUserNoRight, fmt.Errorf("用户 %d 越权操作", in.UserId))
		err = errmsg.NewGrpcErr(errmsg.ErrorUserNoRight, "非up主无法评论")
		return nil, err
	}

	// 更新 subject 状态
	err = l.svcCtx.CommentModel.UpdateSubjectState(ctx, in.TargetType, in.TargetId, int32(in.State))
	if err != nil {
		if err == model.ErrorSubjectNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorSubjectNotExist, fmt.Errorf("评论区不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorSubjectNotExist, "评论区不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbUpdate, "DB修改失败")
		return nil, err
	}

	logger.LogInfo(ctx, "update subject state success")

	resp = &pb.UpdateSubjectStateResp{
		Success: true,
	}
	return resp, nil
}
