package mqs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	pb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
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
	return observability.TraceConsumer(ctx, "article-rpc", "ArticleSyncResultConsumer.Consume", articleMessageAttrs("article_sync_result", key), func(ctx context.Context) error {
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
			return l.handleUpsertResult(ctx, result)
		case ArticleSyncOpDelete:
			return l.handleDeleteResult(ctx, article, result)
		default:
			logger.LogInfo(ctx, "unknown article sync result op ignored", logger.WithArticleID(result.ArticleID))
			return nil
		}
	})
}

func (l *ArticleSyncResultConsumer) handleUpsertResult(ctx context.Context, result ArticleSyncResult) error {
	var notifyAuthor, notifyTitle string
	err := l.svcCtx.ArticleRepo.WithArticleTx(ctx, result.ArticleID, func(tx *gorm.DB, article *model.Article) error {
		if article.DeletedAt.Valid {
			return nil
		}
		EnsureExtInfo(article)
		// A late result can never publish a newer review or restore a withdrawn
		// article. Replays of an already accepted result are no-op.
		if article.ExtInfo[ExtLastSyncEventID] != result.EventID ||
			article.ExtInfo[ExtLastSyncVersion] != strconv.FormatInt(result.VersionMs, 10) ||
			article.Status == int32(pb.ArticleStatus_PUBLISHED) {
			return nil
		}
		syncReason := article.ExtInfo[ExtLastSyncReason]
		if !result.Success {
			article.Status = int32(pb.ArticleStatus_REVIEWING)
			SetSyncState(article, "reco_failed", "failed", syncReason, result.EventID, result.VersionMs, result.ErrorMessage)
			return tx.Save(article).Error
		}
		if article.Status != int32(pb.ArticleStatus_REVIEWING) {
			return nil
		}
		var syncOutbox model.ArticleSyncOutboxEvent
		if err := tx.Where("event_id = ?", result.EventID).First(&syncOutbox).Error; err != nil {
			return err
		}
		var approved ArticleSyncEvent
		if err := json.Unmarshal([]byte(syncOutbox.Payload), &approved); err != nil {
			return err
		}
		if approved.Op != ArticleSyncOpUpsert || approved.EventID != result.EventID ||
			approved.VersionMs != result.VersionMs || approved.ArticleID != article.ID ||
			approved.AuthorID != article.AuthorID {
			return fmt.Errorf("article sync result does not match frozen approved input")
		}
		var highest int64
		if err := tx.Model(&model.ArticleRevision{}).Where("article_id = ?", article.ID).
			Select("COALESCE(MAX(revision),0)").Scan(&highest).Error; err != nil {
			return err
		}
		at := time.Now().UTC()
		contentHash := sha256.Sum256([]byte(approved.Markdown))
		revision := model.ArticleRevision{
			ArticleID: article.ID, Revision: highest + 1,
			RevisionID: fmt.Sprintf("%s:r%d", article.ID, highest+1),
			AuthorID:   article.AuthorID, SourceObject: article.Content,
			ContentSHA256: hex.EncodeToString(contentHash[:]),
			Title:         approved.Title, Brief: approved.Brief, CoverImageURL: approved.CoverURL,
			ManualTypeTag: approved.ManualTypeTag,
			SecondaryTags: append(model.StringArray(nil), approved.SecondaryTags...),
			Markdown:      approved.Markdown, PublishedAt: at, SyncEventID: result.EventID,
		}
		var pointer model.ArticlePublication
		err := tx.Where("article_id = ?", article.ID).First(&pointer).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		pointerVersion := pointer.PointerVersion + 1
		domainEvent, err := articleDomainEvent(article, &revision, "", "published", result.EventID, pointerVersion, at)
		if err != nil {
			return err
		}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		pointer = model.ArticlePublication{ArticleID: article.ID, CurrentRevision: revision.RevisionID,
			PointerVersion: pointerVersion, State: "published", LastEventID: domainEvent.EventID}
		if err := tx.Save(&pointer).Error; err != nil {
			return err
		}
		if err := tx.Create(&domainEvent).Error; err != nil {
			return err
		}
		if syncReason == ArticleSyncReasonCreate && article.ExtInfo[ExtPublishedOnce] == "" {
			notifyAuthor, notifyTitle = article.AuthorID, article.Title
		}
		article.Status = int32(pb.ArticleStatus_PUBLISHED)
		SetSyncState(article, "published", "succeeded", syncReason, result.EventID, result.VersionMs, "")
		article.ExtInfo[ExtPublishedOnce] = "true"
		return tx.Save(article).Error
	})
	if err != nil {
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate,
			fmt.Errorf("persist article revision and domain outbox failed: %w", err),
			logger.WithArticleID(result.ArticleID))
		return err
	}
	if notifyAuthor != "" {
		l.notifyArticlePublished(ctx, notifyAuthor, result.ArticleID, notifyTitle)
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
