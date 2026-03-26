package mqs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	pb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"
	imagesecurity "sea-try-go/service/security/rpc/client/imagesecurityservice"
	security "sea-try-go/service/security/rpc/pb/sea-try-go/service/security/rpc/pb"

	"github.com/minio/minio-go/v7"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ArticleConsumer struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewArticleConsumer(ctx context.Context, svcCtx *svc.ServiceContext) *ArticleConsumer {
	return &ArticleConsumer{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ArticleConsumer) Consume(ctx context.Context, key, val string) error {
	logger.LogInfo(ctx, "article review consumer received message")

	var msg ArticleReviewMessage
	if err := json.Unmarshal([]byte(val), &msg); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("unmarshal review message failed: %w", err))
		return nil
	}

	object, err := l.svcCtx.MinioClient.GetObject(ctx, l.svcCtx.Config.MinIO.BucketName, msg.ContentPath, minio.GetObjectOptions{})
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioDownload, fmt.Errorf("failed to get content from minio: %w", err), logger.WithArticleID(msg.ArticleID))
		return err
	}
	defer object.Close()

	contentBytes, err := io.ReadAll(object)
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorMinioDownload, fmt.Errorf("failed to read minio content: %w", err), logger.WithArticleID(msg.ArticleID))
		return err
	}
	articleContent := string(contentBytes)

	article, err := l.svcCtx.ArticleRepo.FindOne(ctx, msg.ArticleID)
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, fmt.Errorf("failed to find article %s: %w", msg.ArticleID, err), logger.WithArticleID(msg.ArticleID), logger.WithUserID(msg.AuthorID))
		return err
	}
	if article == nil {
		return nil
	}
	if article.Status != int32(pb.ArticleStatus_REVIEWING) {
		logger.LogInfo(ctx, "article review skipped because status changed", logger.WithArticleID(msg.ArticleID), logger.WithUserID(msg.AuthorID))
		return nil
	}

	EnsureExtInfo(article)
	SetSyncState(article, "security_checking", "pending_review", article.ExtInfo[ExtPendingSyncReason], "", article.UpdatedAt.UnixMilli(), "")
	if err := l.svcCtx.ArticleRepo.Update(ctx, article); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("failed to persist publish stage: %w", err), logger.WithArticleID(msg.ArticleID))
		return err
	}

	if err := l.auditArticle(ctx, article, articleContent, msg.AuthorID); err != nil {
		return err
	}
	if article.Status == int32(pb.ArticleStatus_REJECTED) {
		logger.LogInfo(ctx, "article rejected by security check", logger.WithArticleID(msg.ArticleID), logger.WithUserID(msg.AuthorID))
		return nil
	}

	syncReason := strings.TrimSpace(article.ExtInfo[ExtPendingSyncReason])
	if syncReason == "" {
		syncReason = ArticleSyncReasonCreate
	}

	eventID, err := l.newSyncEventID()
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("generate article sync event id failed: %w", err), logger.WithArticleID(msg.ArticleID))
		return err
	}
	versionMs := time.Now().UnixMilli()
	event := NewArticleSyncEvent(article, articleContent, ArticleSyncOpUpsert, syncReason, eventID, versionMs)
	outbox := &model.ArticleSyncOutboxEvent{
		EventID:     event.EventID,
		EventKey:    ArticleSyncEventKey(event.ArticleID, event.Op, event.EventID),
		EventType:   "article_sync",
		AggregateID: event.ArticleID,
		Payload:     MustMarshalSyncEvent(event),
		Status:      model.ArticleSyncOutboxStatusPending,
	}

	SetSyncState(article, "reco_queued", "pending", syncReason, eventID, versionMs, "")
	if err := l.svcCtx.ArticleRepo.RunInTx(ctx, func(tx *gorm.DB) error {
		if err := l.svcCtx.ArticleRepo.UpdateTx(ctx, tx, article); err != nil {
			return err
		}
		return l.svcCtx.ArticleSyncOutbox.CreateTx(ctx, tx, outbox)
	}); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("persist article sync outbox failed: %w", err), logger.WithArticleID(msg.ArticleID), logger.WithUserID(msg.AuthorID))
		return err
	}

	logger.LogInfo(ctx, "article sync event queued", logger.WithArticleID(msg.ArticleID), logger.WithUserID(msg.AuthorID))
	return nil
}

func (l *ArticleConsumer) auditArticle(ctx context.Context, article *model.Article, content string, authorID string) error {
	if l.svcCtx.SecurityRpc == nil {
		return fmt.Errorf("content security client not initialized")
	}

	result, err := l.svcCtx.SecurityRpc.SanitizeContent(ctx, &security.SanitizeContentRequest{
		Text: content,
		Options: &security.SanitizeOptions{
			EnableAdDetection:             true,
			EnableHtmlSanitization:        true,
			EnableUnicodeNormalization:    true,
			EnableWhitespaceNormalization: true,
		},
	})
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("content security rpc error: %w", err), logger.WithArticleID(article.ID), logger.WithUserID(authorID))
		return err
	}
	if !result.Success {
		return fmt.Errorf("content security service error: %s", result.ErrorMessage)
	}
	if result.IsAd {
		article.Status = int32(pb.ArticleStatus_REJECTED)
		SetSyncState(article, "rejected", "failed", article.ExtInfo[ExtPendingSyncReason], "", time.Now().UnixMilli(), "content_rejected")
		if err := l.svcCtx.ArticleRepo.Update(ctx, article); err != nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("failed to update article status to rejected: %w", err), logger.WithArticleID(article.ID), logger.WithUserID(authorID))
			return err
		}
		return nil
	}

	imageURLs := l.extractImageUrls(content)
	if article.CoverImageURL != "" {
		imageURLs = append(imageURLs, article.CoverImageURL)
	}
	for _, imgURL := range imageURLs {
		isAd, _, err := l.auditImage(ctx, imgURL)
		if err != nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("audit image %s failed: %w", imgURL, err), logger.WithArticleID(article.ID), logger.WithUserID(authorID))
			return err
		}
		if isAd {
			article.Status = int32(pb.ArticleStatus_REJECTED)
			SetSyncState(article, "rejected", "failed", article.ExtInfo[ExtPendingSyncReason], "", time.Now().UnixMilli(), "image_rejected")
			if err := l.svcCtx.ArticleRepo.Update(ctx, article); err != nil {
				logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("failed to update article status to rejected: %w", err), logger.WithArticleID(article.ID), logger.WithUserID(authorID))
				return err
			}
			return nil
		}
	}

	return nil
}

func (l *ArticleConsumer) extractImageUrls(content string) []string {
	re := regexp.MustCompile(`!\[.*?\]\((.*?)\)`)
	matches := re.FindAllStringSubmatch(content, -1)
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}
	return urls
}

func (l *ArticleConsumer) auditImage(ctx context.Context, imgURL string) (bool, float32, error) {
	u, err := url.Parse(imgURL)
	if err != nil {
		return false, 0, err
	}
	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return false, 0, fmt.Errorf("invalid image path: %s", path)
	}
	objectName := parts[1]

	object, err := l.svcCtx.MinioClient.GetObject(ctx, l.svcCtx.Config.MinIO.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return false, 0, err
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		return false, 0, err
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	mimeType := "image/jpeg"
	lower := strings.ToLower(objectName)
	if strings.HasSuffix(lower, ".png") {
		mimeType = "image/png"
	} else if strings.HasSuffix(lower, ".webp") {
		mimeType = "image/webp"
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

	resp, err := l.svcCtx.ImageSecurityRpc.DetectImageAd(ctx, &imagesecurity.DetectImageAdRequest{
		ImageBase64: dataURI,
		Options: &imagesecurity.DetectOptions{
			ConfidenceThreshold:  0.7,
			EnableTextExtraction: true,
		},
	})
	if err != nil {
		return false, 0, err
	}
	if !resp.GetSuccess() {
		return false, 0, fmt.Errorf("image moderation service error: %s", resp.GetErrorMessage())
	}

	return resp.IsAd, resp.AdConfidence, nil
}

func (l *ArticleConsumer) newSyncEventID() (string, error) {
	id, err := snowflake.GetID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}
