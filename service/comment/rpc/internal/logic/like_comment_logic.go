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
)

type LikeCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLikeCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeCommentLogic {
	return &LikeCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LikeCommentLogic) LikeComment(in *pb.LikeCommentReq) (resp *pb.LikeCommentResp, err error) {
	start := time.Now()
	result := "ok"

	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "LikeComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "LikeComment").
			Add(time.Since(start).Seconds())
	}()

	if in.CommentId == 0 {
		result = "biz_fail"
		logger.LogBusinessErr(l.ctx, errmsg.ErrorInputWrong, fmt.Errorf("评论ID不能为空"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "评论ID不能为空")
		return nil, err
	}

	ownerId, err := l.svcCtx.CommentModel.GetOwnerId(l.ctx, in.TargetType, in.TargetId)
	if err != nil {
		if err == model.ErrorSubjectNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(l.ctx, errmsg.ErrorSubjectNotExist, fmt.Errorf("评论区不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorSubjectNotExist, "评论区不存在")
			return nil, err
		}

		result = "sys_fail"
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbInsert, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "DB更新失败")
		return nil, err
	}

	err = l.svcCtx.CommentModel.LikeCommentTx(
		l.ctx,
		in.UserId,
		in.CommentId,
		in.TargetType,
		in.TargetId,
		in.ActionType,
		int64(ownerId),
	)
	if err != nil {
		if err == model.ErrorCommentNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(l.ctx, errmsg.ErrorCommentNotExist, fmt.Errorf("评论不存在"))
			err = errmsg.NewGrpcErr(errmsg.ErrorCommentNotExist, "评论不存在")
			return nil, err
		}

		result = "sys_fail"
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbUpdate, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbUpdate, "DB更新失败")
		return nil, err
	}
	if err := l.svcCtx.CommentCache.DeleteCommentIndexCache(l.ctx, in.CommentId); err != nil {
		l.Errorf("invalidate comment cache failed after like, comment %d: %v", in.CommentId, err)
	}

	logger.LogInfo(l.ctx, "like comment success")
	resp = &pb.LikeCommentResp{
		Success: true,
	}
	return resp, nil
}
