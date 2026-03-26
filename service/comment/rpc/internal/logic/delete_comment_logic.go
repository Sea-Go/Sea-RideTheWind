package logic

import (
	"context"
	"fmt"
	"sea-try-go/service/comment/common/errmsg"
	"sea-try-go/service/comment/rpc/internal/metrics"
	"sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/comment/rpc/internal/svc"
	"sea-try-go/service/comment/rpc/pb"
	"sea-try-go/service/common/logger"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type DeleteCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteCommentLogic {
	return &DeleteCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteCommentLogic) DeleteComment(in *pb.DeleteCommentReq) (resp *pb.DeleteCommentResp, err error) {
	start := time.Now()
	result := "ok"

	tracer := otel.Tracer("comment-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Comment-Delete")
	defer span.End()

	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "DeleteComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "DeleteComment").
			Add(time.Since(start).Seconds())
	}()

	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.UserId),
		attribute.Int64("audit.comment_id", in.CommentId),
		attribute.String("audit.target_type", in.TargetType),
		attribute.String("audit.target_id", in.TargetId),
	)

	if in.CommentId == 0 || in.TargetType == "" || in.TargetId == "" {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("评论ID和目标不能为空"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "评论ID和目标不能为空")
		return nil, err
	}

	comment, err := l.svcCtx.CommentModel.FindOneCommentById(ctx, in.CommentId)
	if err != nil {
		if err == model.ErrorCommentNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorCommentNotExist, fmt.Errorf("评论不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorCommentNotExist, "评论不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		return nil, err
	}

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

	if in.UserId != comment.UserId && in.UserId != subject.OwnerId {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorUserNoRight, fmt.Errorf("无权执行该操作"))
		err = errmsg.NewGrpcErr(errmsg.ErrorUserNoRight, "无权执行删除操作")
		return nil, err
	}

	remainCount, err := l.svcCtx.CommentModel.DeleteCommentTx(ctx, in.CommentId, in.UserId, in.TargetType, in.TargetId)
	if err != nil {
		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbUpdate, "DB更新失败")
		return nil, err
	}
	if err := l.svcCtx.CommentCache.InvalidateTargetCaches(ctx, in.TargetType, in.TargetId); err != nil {
		l.Errorf("invalidate comment target cache failed after delete, comment %d: %v", in.CommentId, err)
	}
	if err := l.svcCtx.CommentCache.DeleteCommentIndexCache(ctx, in.CommentId, comment.ParentId); err != nil {
		l.Errorf("invalidate comment item cache failed after delete, comment %d: %v", in.CommentId, err)
	}

	logger.LogInfo(ctx, "delete comment success")

	resp = &pb.DeleteCommentResp{
		Success:           true,
		SubjectTotalCount: remainCount,
	}
	return resp, nil
}
