package logic

import (
	"context"

	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ListArticlesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListArticlesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListArticlesLogic {
	return &ListArticlesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListArticlesLogic) ListArticles(in *__.ListArticlesRequest) (*__.ListArticlesResponse, error) {
	tracer := otel.Tracer("article-rpc")
	ctx, span := tracer.Start(l.ctx, "ListArticles", trace.WithAttributes(
		attribute.Int64("page", int64(in.Page)),
		attribute.Int64("page_size", int64(in.PageSize)),
		attribute.String("sort_by", in.SortBy),
		attribute.Bool("desc", in.Desc),
	))
	defer span.End()

	listOpt := model.ListArticlesOption{
		Page:     int(in.Page),
		PageSize: int(in.PageSize),
		SortBy:   in.SortBy,
		Desc:     in.Desc,
	}
	if in.PublicOnly {
		return l.listPublicArticles(ctx, in, listOpt)
	}
	if in.ManualTypeTag != nil {
		listOpt.ManualTypeTag = *in.ManualTypeTag
		span.SetAttributes(attribute.String("manual_type_tag", *in.ManualTypeTag))
	}
	if in.SecondaryTag != nil {
		listOpt.SecondaryTag = *in.SecondaryTag
		span.SetAttributes(attribute.String("secondary_tag", *in.SecondaryTag))
	}
	if in.AuthorId != nil {
		listOpt.AuthorId = *in.AuthorId
		span.SetAttributes(attribute.String("author_id", *in.AuthorId))
	}

	span.AddEvent("start db list")
	articles, total, err := l.svcCtx.ArticleRepo.List(ctx, listOpt)

	if err != nil {
		span.RecordError(err)
		l.Logger.Errorf("ListArticles error: %v", err)
		return nil, err
	}
	span.AddEvent("db list success")
	span.SetAttributes(attribute.Int64("total_articles", total))

	var pbArticles []*__.Article
	for _, article := range articles {
		pbArticles = append(pbArticles, &__.Article{
			Id:              article.ID,
			Title:           article.Title,
			Brief:           article.Brief,
			MarkdownContent: article.Content,
			CoverImageUrl:   article.CoverImageURL,
			ManualTypeTag:   article.ManualTypeTag,
			SecondaryTags:   article.SecondaryTags,
			AuthorId:        article.AuthorID,
			CreateTime:      article.CreatedAt.UnixMilli(),
			UpdateTime:      article.UpdatedAt.UnixMilli(),
			Status:          __.ArticleStatus(article.Status),
			ViewCount:       article.ViewCount,
			LikeCount:       article.LikeCount,
			CommentCount:    article.CommentCount,
			ShareCount:      article.ShareCount,
			ExtInfo:         cloneStringMap(map[string]string(article.ExtInfo)),
		})
	}

	return &__.ListArticlesResponse{
		Articles: pbArticles,
		Total:    total,
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}

func (l *ListArticlesLogic) listPublicArticles(ctx context.Context, in *__.ListArticlesRequest, opt model.ListArticlesOption) (*__.ListArticlesResponse, error) {
	if in.ManualTypeTag != nil {
		opt.ManualTypeTag = *in.ManualTypeTag
	}
	if in.SecondaryTag != nil {
		opt.SecondaryTag = *in.SecondaryTag
	}
	if in.AuthorId != nil {
		opt.AuthorId = *in.AuthorId
	}
	articles, total, err := l.svcCtx.ArticleRepo.ListPublic(ctx, opt)
	if err != nil {
		l.Logger.Errorf("ListPublicArticles error: %v", err)
		return nil, err
	}
	result := make([]*__.Article, 0, len(articles))
	for _, article := range articles {
		// Public lists carry approved metadata, never a mutable object key or
		// the full Markdown body. Detail resolves the frozen body separately.
		result = append(result, publishedArticleResponse(article, ""))
	}
	page, size := in.Page, in.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	} else if size > 100 {
		size = 100
	}
	return &__.ListArticlesResponse{Articles: result, Total: total, Page: page, PageSize: size}, nil
}
