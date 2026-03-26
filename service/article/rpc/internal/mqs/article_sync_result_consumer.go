package mqs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	pb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	messagepb "sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ArticleSyncResultConsumer struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewArticleSyncResultConsumer(ctx context.Context, svcCtx *svc.ServiceContext) *ArticleSyncResultConsumer {
	return &ArticleSyncResultConsumer{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ArticleSyncResultConsumer) Consume(ctx context.Context, key, val string) error {
	var result ArticleSyncResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("unmarshal article sync result failed: %w", err))
		return nil
	}
	if strings.TrimSpace(result.ArticleID) == "" {
		return nil
	}

	article, err := l.svcCtx.ArticleRepo.FindOneUnscoped(ctx, result.ArticleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.LogInfo(ctx, "article sync result ignored for missing article", logger.WithArticleID(result.ArticleID))
			return nil
		}
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, fmt.Errorf("find article for sync result failed: %w", err), logger.WithArticleID(result.ArticleID))
		return err
	}

	switch result.Op {
	case ArticleSyncOpUpsert:
		return l.handleUpsertResult(ctx, article, result)
	case ArticleSyncOpDelete:
		return l.handleDeleteResult(ctx, article, result)
	default:
		logger.LogInfo(ctx, "unknown article sync result op ignored", logger.WithArticleID(result.ArticleID))
		return nil
	}
}

func (l *ArticleSyncResultConsumer) handleUpsertResult(ctx context.Context, article *model.Article, result ArticleSyncResult) error {
	if article.DeletedAt.Valid {
		logger.LogInfo(ctx, "ignore upsert sync result for deleted article", logger.WithArticleID(article.ID), logger.WithUserID(article.AuthorID))
		return nil
	}
	EnsureExtInfo(article)
	syncReason := article.ExtInfo[ExtLastSyncReason]
	shouldNotify := result.Success && syncReason == ArticleSyncReasonCreate && article.ExtInfo[ExtPublishedOnce] == ""
	if result.Success {
		article.Status = int32(pb.ArticleStatus_PUBLISHED)
		SetSyncState(article, "published", "succeeded", syncReason, result.EventID, result.VersionMs, "")
		if article.ExtInfo[ExtPublishedOnce] == "" {
			article.ExtInfo[ExtPublishedOnce] = "true"
		}
	} else {
		article.Status = int32(pb.ArticleStatus_REVIEWING)
		SetSyncState(article, "reco_failed", "failed", syncReason, result.EventID, result.VersionMs, result.ErrorMessage)
	}

	if err := l.svcCtx.ArticleRepo.Update(ctx, article); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("persist article sync result failed: %w", err), logger.WithArticleID(article.ID), logger.WithUserID(article.AuthorID))
		return err
	}

	if shouldNotify {
		l.notifyArticlePublished(ctx, article.AuthorID, article.ID, article.Title)
	}

	return nil
}

func (l *ArticleSyncResultConsumer) handleDeleteResult(ctx context.Context, article *model.Article, result ArticleSyncResult) error {
	EnsureExtInfo(article)
	if result.Success {
		SetSyncState(article, "source_only", "succeeded", ArticleSyncReasonDelete, result.EventID, result.VersionMs, "")
	} else {
		SetSyncState(article, "delete_failed", "failed", ArticleSyncReasonDelete, result.EventID, result.VersionMs, result.ErrorMessage)
	}

	if article.DeletedAt.Valid {
		return nil
	}

	if err := l.svcCtx.ArticleRepo.Update(ctx, article); err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, fmt.Errorf("persist article delete sync result failed: %w", err), logger.WithArticleID(article.ID), logger.WithUserID(article.AuthorID))
		return err
	}
	return nil
}

func (l *ArticleSyncResultConsumer) notifyArticlePublished(ctx context.Context, authorID, articleID, title string) {
	uid, err := strconv.ParseInt(authorID, 10, 64)
	if err != nil || uid <= 0 {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("invalid article author id for notification: %s", authorID), logger.WithArticleID(articleID))
		return
	}

	content := "你的文章已经完成审核并成功入库。"
	if strings.TrimSpace(title) != "" {
		content = fmt.Sprintf("《%s》已经完成审核并成功入库。", strings.TrimSpace(title))
	}

	_, err = l.svcCtx.MessageRpc.SendNotification(ctx, &messagepb.SendNotificationReq{
		RecipientIds: []int64{uid},
		Broadcast:    false,
		SenderId:     0,
		SenderRole:   messagepb.SenderRole_SYSTEM,
		Kind:         messagepb.NotificationKind_ARTICLE_PUBLISHED,
		Title:        "文章发布成功",
		Content:      content,
		Extra: map[string]string{
			"article_id": articleID,
			"author_id":  authorID,
		},
	})
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, fmt.Errorf("send article published notification failed: %w", err), logger.WithArticleID(articleID), logger.WithUserID(authorID))
	}
}
