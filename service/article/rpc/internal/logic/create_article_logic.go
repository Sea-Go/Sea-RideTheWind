package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/mqs"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/metrics"
	__ "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"

	"github.com/minio/minio-go/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CreateArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateArticleLogic {
	return &CreateArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateArticleLogic) CreateArticle(in *__.CreateArticleRequest) (*__.CreateArticleResponse, error) {
	tracer := otel.Tracer("article-rpc")
	ctx, span := tracer.Start(l.ctx, "CreateArticle", trace.WithAttributes(
		attribute.String("author_id", in.AuthorId),
		attribute.String("title", in.Title),
	))
	defer span.End()

	idInt, err := snowflake.GetID()
	if err != nil {
		span.RecordError(err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	articleID := fmt.Sprintf("%d", idInt)
	span.SetAttributes(attribute.String("article_id", articleID))

	objectName := fmt.Sprintf("%s%s.md", l.svcCtx.Config.MinIO.ArticlePath, articleID)
	timer := prometheus.NewTimer(metrics.MinioRequestDuration.WithLabelValues("put"))
	_, err = l.svcCtx.MinioClient.PutObject(
		ctx,
		l.svcCtx.Config.MinIO.BucketName,
		objectName,
		strings.NewReader(in.MarkdownContent),
		int64(len(in.MarkdownContent)),
		minio.PutObjectOptions{ContentType: "text/markdown"},
	)
	timer.ObserveDuration()
	if err != nil {
		span.RecordError(err)
		metrics.MinioRequestErrors.WithLabelValues("put").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioUpload, fmt.Errorf("upload to minio failed: %w", err), logger.WithArticleID(articleID), logger.WithUserID(in.AuthorId))
		return nil, status.Error(codes.Internal, err.Error())
	}
	metrics.FileUploadTotal.WithLabelValues("markdown").Inc()

	brief := ""
	if in.Brief != nil {
		brief = *in.Brief
	}
	coverImageURL := ""
	if in.CoverImageUrl != nil {
		coverImageURL = *in.CoverImageUrl
	}

	newArticle := &model.Article{
		ID:            articleID,
		Title:         in.Title,
		Brief:         brief,
		Content:       objectName,
		CoverImageURL: coverImageURL,
		ManualTypeTag: in.ManualTypeTag,
		SecondaryTags: model.StringArray(in.SecondaryTags),
		AuthorID:      in.AuthorId,
		Status:        int32(__.ArticleStatus_REVIEWING),
		ExtInfo: model.JSONMap{
			mqs.ExtPublishStage:      "queued",
			mqs.ExtRecoSyncState:     "pending_review",
			mqs.ExtLastSyncError:     "",
			mqs.ExtLastSyncEventID:   "",
			mqs.ExtLastSyncVersion:   "0",
			mqs.ExtPendingSyncReason: mqs.ArticleSyncReasonCreate,
			mqs.ExtLastSyncReason:    mqs.ArticleSyncReasonCreate,
		},
	}

	if err := l.svcCtx.ArticleRepo.Insert(ctx, newArticle); err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err, logger.WithArticleID(articleID), logger.WithUserID(in.AuthorId))
		return nil, status.Error(codes.Internal, err.Error())
	}
	metrics.ArticleTotal.WithLabelValues("create").Inc()
	metrics.ArticleStatusTotal.WithLabelValues("reviewing").Inc()

	msg := mqs.ArticleReviewMessage{
		ArticleID:   articleID,
		AuthorID:    in.AuthorId,
		ContentPath: objectName,
	}

	msgBytes, _ := json.Marshal(msg)
	if err := l.svcCtx.KqPusher.PushWithKey(ctx, articleID, string(msgBytes)); err != nil {
		span.RecordError(err)
		metrics.KafkaPushErrors.WithLabelValues("article_review").Inc()
		logger.LogBusinessErr(ctx, errmsg.Error, fmt.Errorf("kafka push failed: %w", err), logger.WithArticleID(articleID), logger.WithUserID(in.AuthorId))
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &__.CreateArticleResponse{ArticleId: articleID}, nil
}
