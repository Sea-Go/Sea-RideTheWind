package mqs

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/article/rpc/internal/model"
	pb "sea-try-go/service/article/rpc/pb"
)

const (
	ArticleSyncScope = "article_sync"

	ArticleSyncOpUpsert = "upsert"
	ArticleSyncOpDelete = "delete"

	ArticleSyncReasonCreate       = "create"
	ArticleSyncReasonUpdate       = "update"
	ArticleSyncReasonStatusChange = "status_change"
	ArticleSyncReasonDelete       = "delete"

	ExtPublishStage      = "publish_stage"
	ExtRecoSyncState     = "reco_sync_state"
	ExtLastSyncError     = "last_sync_error"
	ExtLastSyncEventID   = "last_sync_event_id"
	ExtLastSyncVersion   = "last_sync_version_ms"
	ExtLastSyncReason    = "last_sync_reason"
	ExtPublishedOnce     = "published_once"
	ExtPendingSyncReason = "pending_sync_reason"
)

type ArticleReviewMessage struct {
	ArticleID   string `json:"article_id"`
	AuthorID    string `json:"author_id"`
	ContentPath string `json:"content_path"`
}

type ArticleSyncEvent struct {
	EventScope    string   `json:"event_scope"`
	EventID       string   `json:"event_id"`
	ArticleID     string   `json:"article_id"`
	Op            string   `json:"op"`
	Reason        string   `json:"reason"`
	AuthorID      string   `json:"author_id"`
	Status        string   `json:"status"`
	VersionMs     int64    `json:"version_ms"`
	Title         string   `json:"title,omitempty"`
	Brief         string   `json:"brief,omitempty"`
	CoverURL      string   `json:"cover_url,omitempty"`
	ManualTypeTag string   `json:"manual_type_tag,omitempty"`
	SecondaryTags []string `json:"secondary_tags,omitempty"`
	Markdown      string   `json:"markdown,omitempty"`
}

type ArticleSyncResult struct {
	EventScope   string `json:"event_scope"`
	EventID      string `json:"event_id"`
	ArticleID    string `json:"article_id"`
	Op           string `json:"op"`
	VersionMs    int64  `json:"version_ms"`
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func EnsureExtInfo(article *model.Article) {
	if article.ExtInfo == nil {
		article.ExtInfo = model.JSONMap{}
	}
}

func SetSyncState(article *model.Article, publishStage, syncState, syncReason, eventID string, versionMs int64, syncError string) {
	EnsureExtInfo(article)
	article.ExtInfo[ExtPublishStage] = publishStage
	article.ExtInfo[ExtRecoSyncState] = syncState
	article.ExtInfo[ExtLastSyncError] = syncError
	article.ExtInfo[ExtLastSyncEventID] = eventID
	article.ExtInfo[ExtLastSyncReason] = syncReason
	if versionMs > 0 {
		article.ExtInfo[ExtLastSyncVersion] = strconv.FormatInt(versionMs, 10)
	}
	if syncReason != "" {
		article.ExtInfo[ExtPendingSyncReason] = syncReason
	}
}

func ArticleSyncEventKey(articleID, op, eventID string) string {
	return strings.TrimSpace(articleID) + ":" + strings.TrimSpace(op) + ":" + strings.TrimSpace(eventID)
}

func NewArticleSyncEvent(article *model.Article, markdown, op, reason, eventID string, versionMs int64) ArticleSyncEvent {
	status := pb.ArticleStatus(article.Status).String()
	if op == ArticleSyncOpUpsert {
		status = pb.ArticleStatus_PUBLISHED.String()
	}

	return ArticleSyncEvent{
		EventScope:    ArticleSyncScope,
		EventID:       eventID,
		ArticleID:     article.ID,
		Op:            op,
		Reason:        reason,
		AuthorID:      article.AuthorID,
		Status:        status,
		VersionMs:     versionMs,
		Title:         article.Title,
		Brief:         article.Brief,
		CoverURL:      article.CoverImageURL,
		ManualTypeTag: article.ManualTypeTag,
		SecondaryTags: append([]string(nil), article.SecondaryTags...),
		Markdown:      markdown,
	}
}

func MustMarshalSyncEvent(event ArticleSyncEvent) string {
	data, _ := json.Marshal(event)
	return string(data)
}

func nextSyncVersionMs(now time.Time) int64 {
	return now.UnixMilli()
}
