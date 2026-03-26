package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"sea-try-go/service/comment/common/errmsg"
	kqtypes "sea-try-go/service/comment/rpc/common/types"
	"sea-try-go/service/comment/rpc/internal/metrics"
	"sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/comment/rpc/internal/svc"
	"sea-try-go/service/comment/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type CreateCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCommentLogic {
	return &CreateCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateCommentLogic) CreateComment(in *pb.CreateCommentReq) (resp *pb.CreateCommentResp, err error) {
	start := time.Now()
	result := "ok"

	tracer := otel.Tracer("comment-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Comment-Create")
	defer span.End()

	defer func() {
		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "CreateComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "CreateComment").
			Add(time.Since(start).Seconds())
	}()

	span.SetAttributes(
		attribute.Int64("audit.operator_id", in.UserId),
		attribute.String("audit.target_type", in.TargetType),
		attribute.String("audit.target_id", in.TargetId),
		attribute.Int64("audit.root_id", in.RootId),
		attribute.Int64("audit.parent_id", in.ParentId),
	)

	if in.TargetId == "" || in.TargetType == "" {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("target type and id cannot be empty"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "目标类型和ID不能为空")
		return nil, err
	}

	if in.Content == "" {
		result = "biz_fail"
		logger.LogBusinessErr(ctx, errmsg.ErrorInputWrong, fmt.Errorf("comment content cannot be empty"))
		err = errmsg.NewGrpcErr(errmsg.ErrorInputWrong, "评论内容不能为空")
		return nil, err
	}

	ownerID, err := l.resolveOwnerID(ctx, in.TargetType, in.TargetId)
	if err != nil {
		if err == model.ErrorSubjectNotFound {
			result = "biz_fail"
			logger.LogBusinessErr(ctx, errmsg.ErrorSubjectNotExist, fmt.Errorf("subject not found"))
			err = errmsg.NewGrpcErr(errmsg.ErrorSubjectNotExist, "评论区不存在")
			return nil, err
		}

		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorDbSelect, "DB查询失败")
		return nil, err
	}

	commentID, err := snowflake.GetID()
	if err != nil {
		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorSnowflakeID, fmt.Errorf("snowflake id generation failed"))
		err = errmsg.NewGrpcErr(errmsg.ErrorSnowflakeID, "雪花算法生成ID出错")
		return nil, err
	}

	now := time.Now()
	msg := kqtypes.CommentKafkaMsg{
		CommentId:  commentID,
		TargetType: in.TargetType,
		TargetId:   in.TargetId,
		UserId:     in.UserId,
		RootId:     in.RootId,
		ParentId:   in.ParentId,
		Content:    in.Content,
		OwnerId:    ownerID,
		Meta:       in.Meta,
		Attribute:  0,
		CreateTime: now.Unix(),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorJsonMarshal, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorJsonMarshal, "Json序列化失败")
		return nil, err
	}

	if err = l.svcCtx.KqPusherClient.Push(ctx, string(msgBytes)); err != nil {
		result = "sys_fail"
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorKafkaPush, err)
		err = errmsg.NewGrpcErr(errmsg.ErrorKafkaPush, "Kafka推送失败")
		return nil, err
	}

	logger.LogInfo(ctx, "create comment success")

	return &pb.CreateCommentResp{
		Id:           commentID,
		CreatedAt:    now.Format(time.DateTime),
		SubjectCount: 0,
	}, nil
}

func (l *CreateCommentLogic) resolveOwnerID(ctx context.Context, targetType, targetID string) (int64, error) {
	ownerID, err := l.svcCtx.CommentModel.GetOwnerId(ctx, targetType, targetID)
	if err == nil {
		return int64(ownerID), nil
	}
	if err != model.ErrorSubjectNotFound {
		return 0, err
	}

	subject, fallbackErr := resolveSubjectFallback(ctx, l.svcCtx.ArticleRpc, targetType, targetID)
	if fallbackErr != nil {
		return 0, err
	}
	return subject.OwnerId, nil
}
