package logic

import (
	"context"
	"fmt"
	"time"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/mqs"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/metrics"
	__ "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"

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

type DeleteArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteArticleLogic {
	return &DeleteArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteArticleLogic) DeleteArticle(in *__.DeleteArticleRequest) (*__.DeleteArticleResponse, error) {
	tracer := otel.Tracer("article-rpc")
	spanCtx, span := tracer.Start(l.ctx, "DeleteArticle", trace.WithAttributes(
		attribute.String("article_id", in.ArticleId),
	))
	defer span.End()

	article, err := l.svcCtx.ArticleRepo.FindOne(spanCtx, in.ArticleId)
	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbSelect, err, logger.WithArticleID(in.ArticleId))
		return nil, err
	}
	if article == nil {
		err = status.Error(codes.NotFound, "article not found")
		span.RecordError(err)
		return nil, err
	}
	if in.GetOperatorId() == "" || in.GetOperatorId() != article.AuthorID {
		err = status.Error(codes.PermissionDenied, "forbidden")
		span.RecordError(err)
		return nil, err
	}

	if article.Content != "" {
		timer := prometheus.NewTimer(metrics.MinioRequestDuration.WithLabelValues("delete"))
		err = l.svcCtx.MinioClient.RemoveObject(spanCtx, l.svcCtx.Config.MinIO.BucketName, article.Content, minio.RemoveObjectOptions{})
		timer.ObserveDuration()
		if err != nil {
			metrics.MinioRequestErrors.WithLabelValues("delete").Inc()
			logger.LogBusinessErr(spanCtx, errmsg.ErrorMinioDelete, fmt.Errorf("remove minio object failed: %w", err), logger.WithArticleID(in.ArticleId))
			return nil, err
		}
	}

	eventID, err := l.newDeleteEventID()
	if err != nil {
		span.RecordError(err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	versionMs := time.Now().UnixMilli()
	event := mqs.NewArticleSyncEvent(article, "", mqs.ArticleSyncOpDelete, mqs.ArticleSyncReasonDelete, eventID, versionMs)
	outbox := &model.ArticleSyncOutboxEvent{
		EventID:     event.EventID,
		EventKey:    mqs.ArticleSyncEventKey(event.ArticleID, event.Op, event.EventID),
		EventType:   "article_sync",
		AggregateID: event.ArticleID,
		Payload:     mqs.MustMarshalSyncEvent(event),
		Status:      model.ArticleSyncOutboxStatusPending,
	}

	if err := l.svcCtx.ArticleRepo.RunInTx(spanCtx, func(tx *gorm.DB) error {
		if err := l.svcCtx.ArticleSyncOutbox.CreateTx(spanCtx, tx, outbox); err != nil {
			return err
		}
		return l.svcCtx.ArticleRepo.DeleteTx(spanCtx, tx, in.ArticleId)
	}); err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(spanCtx, errmsg.ErrorDbUpdate, err, logger.WithArticleID(in.ArticleId))
		return nil, err
	}

	metrics.ArticleTotal.WithLabelValues("delete").Inc()
	return &__.DeleteArticleResponse{Success: true}, nil
}

func (l *DeleteArticleLogic) newDeleteEventID() (string, error) {
	id, err := snowflake.GetID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}
