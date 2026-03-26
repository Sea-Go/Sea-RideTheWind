package logic

import (
	"context"
	"fmt"
	"io"

	"sea-try-go/service/article/common/errmsg"
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
			return nil, nil
		}
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err, logger.WithArticleID(in.ArticleId))
		return nil, err
	}
	span.AddEvent("find article success")
	span.SetAttributes(attribute.Bool("article_found", true))

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
