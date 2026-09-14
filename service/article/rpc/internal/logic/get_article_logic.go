package logic

import (
	"context"
	"fmt"
	"io"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/metrics"
	"sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"

	"github.com/minio/minio-go/v7"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type GetArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetArticleLogic {
	return &GetArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetArticleLogic) GetArticle(in *__.GetArticleRequest) (*__.GetArticleResponse, error) {
	if in.PublicOnly {
		return l.getPublicArticle(in)
	}
	if in.RequesterId != "" && in.IncrView {
		return nil, status.Error(codes.InvalidArgument, "author read cannot increment view")
	}
	tracer := otel.Tracer("article-rpc")
	ctx, span := tracer.Start(l.ctx, "GetArticle", trace.WithAttributes(
		attribute.String("article_id", in.ArticleId),
	))
	defer span.End()

	span.AddEvent("start find article")
	article, err := l.svcCtx.ArticleRepo.FindOne(ctx, in.ArticleId)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			span.SetAttributes(attribute.Bool("article_found", false))
			return nil, status.Error(codes.NotFound, "article not found")
		}
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err, logger.WithArticleID(in.ArticleId))
		return nil, err
	}
	span.AddEvent("find article success")
	span.SetAttributes(attribute.Bool("article_found", true))
	if in.RequesterId != "" && article.AuthorID != in.RequesterId {
		return nil, status.Error(codes.PermissionDenied, "article author differs")
	}

	if in.IncrView {
		span.AddEvent("start incr view count")
		if err := l.svcCtx.ArticleRepo.IncrViewCount(ctx, in.ArticleId); err != nil {
			span.RecordError(err)
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithArticleID(in.ArticleId))
		}
		article.ViewCount++
		span.AddEvent("incr view count success")
	}

	//统计 MinIO get 操作耗时
	timer := prometheus.NewTimer(metrics.MinioRequestDuration.WithLabelValues("get"))
	span.AddEvent("start get minio object")
	object, err := l.svcCtx.MinioClient.GetObject(ctx, l.svcCtx.Config.MinIO.BucketName, article.Content, minio.GetObjectOptions{})
	timer.ObserveDuration()
	if err != nil {
		span.RecordError(err)
		//统计 MinIO get 操作失败数
		metrics.MinioRequestErrors.WithLabelValues("get").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioDownload, fmt.Errorf("minio get object failed: %w", err), logger.WithArticleID(in.ArticleId))
		return nil, err
	}
	defer object.Close()
	span.AddEvent("get minio object success")

	span.AddEvent("start read minio object")
	contentBytes, err := io.ReadAll(object)
	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioDownload, fmt.Errorf("read minio object failed: %w", err), logger.WithArticleID(in.ArticleId))
		return nil, err
	}
	span.AddEvent("read minio object success")

	return &__.GetArticleResponse{
		Article: &__.Article{
			Id:              article.ID,
			Title:           article.Title,
			Brief:           article.Brief,
			MarkdownContent: string(contentBytes),
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
		},
	}, nil
}

func (l *GetArticleLogic) getPublicArticle(in *__.GetArticleRequest) (*__.GetArticleResponse, error) {
	published, err := l.svcCtx.ArticleRepo.FindPublicOne(l.ctx, in.ArticleId)
	if err == gorm.ErrRecordNotFound {
		return nil, status.Error(codes.NotFound, "article not published")
	}
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err, logger.WithArticleID(in.ArticleId))
		return nil, status.Error(codes.Internal, "read publication failed")
	}
	article := published.Article
	var markdown string
	if published.Revision != nil {
		markdown = published.Revision.Markdown
	} else {
		// A legacy PUBLISHED row has no revision pointer. Its original object
		// remains the compatibility source, without asserting immutability.
		object, err := l.svcCtx.MinioClient.GetObject(l.ctx, l.svcCtx.Config.MinIO.BucketName,
			article.Content, minio.GetObjectOptions{})
		if err != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorMinioDownload, err, logger.WithArticleID(in.ArticleId))
			return nil, status.Error(codes.Internal, "read legacy article failed")
		}
		defer object.Close()
		body, err := io.ReadAll(object)
		if err != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorMinioDownload, err, logger.WithArticleID(in.ArticleId))
			return nil, status.Error(codes.Internal, "read legacy article failed")
		}
		markdown = string(body)
	}
	if in.IncrView {
		increased, err := l.svcCtx.ArticleRepo.IncrPublicViewCount(l.ctx, in.ArticleId)
		if err != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbUpdate, err, logger.WithArticleID(in.ArticleId))
			return nil, status.Error(codes.Internal, "increment public view failed")
		}
		if !increased {
			return nil, status.Error(codes.NotFound, "article no longer published")
		}
		article.ViewCount++
	}
	return &__.GetArticleResponse{Article: publishedArticleResponse(published, markdown)}, nil
}

func publishedArticleResponse(published model.PublicArticle, markdown string) *__.Article {
	article := published.Article
	response := &__.Article{
		Id: article.ID, Title: article.Title, Brief: article.Brief,
		MarkdownContent: markdown, CoverImageUrl: article.CoverImageURL,
		ManualTypeTag: article.ManualTypeTag, SecondaryTags: article.SecondaryTags,
		AuthorId: article.AuthorID, CreateTime: article.CreatedAt.UnixMilli(),
		UpdateTime: article.UpdatedAt.UnixMilli(), Status: __.ArticleStatus_PUBLISHED,
		ViewCount: article.ViewCount, LikeCount: article.LikeCount,
		CommentCount: article.CommentCount, ShareCount: article.ShareCount,
		ExtInfo: map[string]string{"publication_gap": "legacy_revision_missing"},
	}
	if revision := published.Revision; revision != nil {
		response.Title, response.Brief, response.CoverImageUrl = revision.Title, revision.Brief, revision.CoverImageURL
		response.ManualTypeTag, response.SecondaryTags = revision.ManualTypeTag, revision.SecondaryTags
		response.AuthorId, response.UpdateTime = revision.AuthorID, revision.PublishedAt.UnixMilli()
		response.ExtInfo = map[string]string{"published_revision_id": revision.RevisionID}
	}
	return response
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
