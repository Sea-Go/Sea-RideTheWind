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

type ReportCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReportCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportCommentLogic {
	return &ReportCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ReportCommentLogic) ReportComment(in *pb.ReportCommentReq) (resp *pb.ReportCommentResp, err error) {
	start := time.Now()
	result := "ok"

	tracer := otel.Tracer("comment-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Comment-Report")
	defer span.End()

	// defer 统一埋点
	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "ReportComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "ReportComment").
			Add(time.Since(start).Seconds())
	}()

	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.UserId),
		attribute.Int64("audit.comment_id", in.CommentId),
		attribute.String("audit.target_type", in.TargetType),
		attribute.String("audit.target_id", in.TargetId),
		attribute.String("audit.reason", in.Reason.String()),
	)

	// 输入校验
	if in.CommentId == 0 {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("评论ID不能为空"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "评论ID不能为空")
		return nil, err
	}

	// 构造 report
	report := &model.ReportRecord{
		UserId:     in.UserId,
		CommentId:  in.CommentId,
		TargetType: in.TargetType,
		TargetId:   in.TargetId,
		Reason:     int32(in.Reason),
		Detail:     in.Detail,
	}

	// DB 插入
	err = l.svcCtx.CommentModel.InsertReport(ctx, report)
	if err != nil {
		if err == model.ErrorCommentNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorCommentNotExist, fmt.Errorf("评论不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorCommentNotExist, "评论不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbInsert, "DB插入失败")
		return nil, err
	}

	logger.LogInfo(ctx, "report comment success")

	resp = &pb.ReportCommentResp{
		Success: true,
	}
	return resp, nil
}
