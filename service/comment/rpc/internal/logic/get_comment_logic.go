package logic

import (
	"context"
	"time"

	"sea-try-go/service/comment/rpc/internal/metrics"
	"sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/comment/rpc/internal/svc"
	"sea-try-go/service/comment/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type GetCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCommentLogic {
	return &GetCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetCommentLogic) GetComment(in *pb.GetCommentReq) (resp *pb.GetCommentResp, err error) {
	start := time.Now()
	defer func() {
		result := "ok"
		if err != nil {
			result = "sys_fail"
		}

		metrics.CommentRequestCounterMetric.
			WithLabelValues("comment_rpc", "GetComment", result).
			Inc()

		metrics.CommentRequestSecondsCounterMetric.
			WithLabelValues("comment_rpc", "GetComment").
			Add(time.Since(start).Seconds())
	}()

	ctx, cancel := context.WithTimeout(l.ctx, time.Second)
	defer cancel()

	tracer := otel.Tracer("comment.rpc")
	ctx, span := tracer.Start(ctx, "GetComment")
	defer span.End()

	page := int(in.Page)
	if page <= 0 {
		page = 1
	}
	pageSize := int(in.PageSize)
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	span.SetAttributes(
		attribute.String("target_id", in.TargetId),
		attribute.Int64("root_id", in.RootId),
		attribute.Int64("page", in.Page),
		attribute.Int64("page_size", in.PageSize),
		attribute.String("target_type", in.TargetType),
		attribute.Int64("sort_type", in.SortType),
	)

	sortType := model.ReplySortTime
	if in.SortType == 0 {
		sortType = model.ReplySortHot
	}

	subject, err := l.loadSubject(ctx, in.TargetType, in.TargetId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	conn := l.svcCtx.CommentModel
	ids, err := l.svcCtx.CommentCache.GetReplyIDsPageCache(ctx, model.GetReplyIDsPageReq{
		TargetType: in.TargetType,
		TargetId:   in.TargetId,
		RootId:     in.RootId,
		Offset:     offset,
		Limit:      pageSize,
		Sort:       sortType,
		OnlyNormal: false,
	}, conn)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	commentItems := make([]*pb.CommentItem, 0, len(ids))
	if len(ids) > 0 {
		indexRows, indexErr := l.svcCtx.CommentCache.GetCommentIndexCache(ctx, ids, conn)
		if indexErr != nil {
			span.RecordError(indexErr)
			span.SetStatus(codes.Error, indexErr.Error())
			return nil, indexErr
		}

		contentRows, contentErr := l.svcCtx.CommentCache.BatchGetContentCache(ctx, ids, conn)
		if contentErr != nil {
			span.RecordError(contentErr)
			span.SetStatus(codes.Error, contentErr.Error())
			return nil, contentErr
		}

		for idx := range indexRows {
			if idx >= len(contentRows) {
				break
			}
			indexRow := indexRows[idx]
			contentRow := contentRows[idx]
			commentItems = append(commentItems, &pb.CommentItem{
				Id:           indexRow.Id,
				UserId:       indexRow.UserId,
				Content:      contentRow.Content,
				RootId:       indexRow.RootId,
				ParentId:     indexRow.ParentId,
				LikeCount:    indexRow.LikeCount,
				DislikeCount: indexRow.DislikeCount,
				ReplyCount:   indexRow.ReplyCount,
				Attribute:    indexRow.Attribute,
				State:        pb.CommentState(indexRow.State),
				CreatedAt:    indexRow.CreatedAt.Format("2006-01-02 15:04:05"),
				Meta:         contentRow.Meta,
				Children:     nil,
			})
		}
	}

	metrics.CommentListSizeGaugeMetric.
		WithLabelValues("comment_list", "GetComment").
		Set(float64(len(commentItems)))

	return &pb.GetCommentResp{
		Comment: commentItems,
		Subject: &pb.SubjectInfo{
			TargetType: subject.TargetType,
			TargetId:   subject.TargetId,
			TotalCount: subject.TotalCount,
			RootCount:  subject.RootCount,
			State:      pb.SubjectState(subject.State),
			Attribute:  subject.Attribute,
		},
	}, nil
}

func (l *GetCommentLogic) loadSubject(ctx context.Context, targetType, targetID string) (model.Subject, error) {
	subject, err := l.svcCtx.CommentCache.GetSubjectWithCache(ctx, targetType, targetID, l.svcCtx.CommentModel)
	if err == nil {
		return subject, nil
	}
	if err != model.ErrorSubjectNotFound {
		return model.Subject{}, err
	}

	subject, fallbackErr := resolveSubjectFallback(ctx, l.svcCtx.ArticleRpc, targetType, targetID)
	if fallbackErr != nil {
		return model.Subject{}, err
	}
	_ = l.svcCtx.CommentCache.SetSubjectCache(ctx, targetID, &subject, 5*time.Minute)
	return subject, nil
}
