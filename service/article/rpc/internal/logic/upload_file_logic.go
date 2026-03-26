package logic

import (
	"bytes"
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"mime"
	"path/filepath"
	"strings"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/metrics"
	__ "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type UploadFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUploadFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadFileLogic {
	return &UploadFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UploadFileLogic) UploadFile(in *__.UploadFileRequest) (*__.UploadFileResponse, error) {
	tracer := otel.Tracer("article-rpc")
	ctx, span := tracer.Start(l.ctx, "UploadFile", trace.WithAttributes(
		attribute.String("file_name", in.FileName),
	))
	defer span.End()

	id, err := snowflake.GetID()
	if err != nil {
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.Error, fmt.Errorf("generate snowflake id failed: %w", err)) // 雪花ID生成失败，暂时用通用错误
		return nil, err
	}
	span.SetAttributes(attribute.Int64("file_id", id))

	ext := filepath.Ext(in.FileName)
	objectName := fmt.Sprintf("%s%d%s", l.svcCtx.Config.MinIO.ImagePath, id, ext)

	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	span.SetAttributes(attribute.String("content_type", contentType), attribute.String("object_name", objectName))

	//统计 MinIO put 操作（文件上传）耗时
	timer := prometheus.NewTimer(metrics.MinioRequestDuration.WithLabelValues("put"))
	span.AddEvent("start upload to minio")
	_, err = l.svcCtx.MinioClient.PutObject(ctx, l.svcCtx.Config.MinIO.BucketName, objectName,
		bytes.NewReader(in.Content), int64(len(in.Content)),
		minio.PutObjectOptions{ContentType: contentType})
	timer.ObserveDuration()

	if err != nil {
		span.RecordError(err)
		//统计 MinIO put 操作（文件上传）失败数
		metrics.MinioRequestErrors.WithLabelValues("put").Inc()
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioUpload, fmt.Errorf("minio put object failed: %w", err))
		return nil, err
	}
	span.AddEvent("upload to minio success")
	//统计图片文件上传总数
	metrics.FileUploadTotal.WithLabelValues("image").Inc()

	fileUrl := l.buildPublicFileURL(objectName)

	return &__.UploadFileResponse{
		FileUrl: fileUrl,
	}, nil
}

func (l *UploadFileLogic) buildPublicFileURL(objectName string) string {
	baseURL := strings.TrimSpace(l.svcCtx.Config.MinIO.PublicBaseURL)
	if baseURL == "" {
		scheme := "http"
		if l.svcCtx.Config.MinIO.UseSSL {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, strings.TrimSpace(l.svcCtx.Config.MinIO.Endpoint))
	}

	return fmt.Sprintf(
		"%s/%s/%s",
		strings.TrimRight(baseURL, "/"),
		strings.Trim(l.svcCtx.Config.MinIO.BucketName, "/"),
		strings.TrimLeft(objectName, "/"),
	)
}
