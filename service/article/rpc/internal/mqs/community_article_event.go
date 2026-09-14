package mqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"sea-try-go/service/article/rpc/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type communitySubject struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

type communityRevisionPayload struct {
	ArticleID      string           `json:"article_id"`
	RevisionID     string           `json:"revision_id"`
	Revision       int64            `json:"revision"`
	Author         communitySubject `json:"subject_ref"`
	SourceRef      string           `json:"source_ref"`
	SourceObject   string           `json:"source_object"`
	ContentSHA256  string           `json:"content_sha256"`
	Title          string           `json:"title"`
	Brief          string           `json:"brief"`
	CoverImageURL  string           `json:"cover_image_url"`
	ManualTypeTag  string           `json:"manual_type_tag"`
	SecondaryTags  []string         `json:"secondary_tags"`
	Markdown       string           `json:"markdown,omitempty"`
	Operation      string           `json:"operation"`
	SearchEvidence bool             `json:"search_evidence"`
	WikiModule     *string          `json:"wiki_module_id"` // never inferred from a community article
}

type communityArticleEnvelope struct {
	EventID          string                   `json:"event_id"`
	EventType        string                   `json:"event_type"`
	SchemaVersion    int                      `json:"schema_version"`
	Producer         string                   `json:"producer"`
	AggregateID      string                   `json:"aggregate_id"`
	AggregateVersion int64                    `json:"aggregate_version"`
	OperationID      string                   `json:"operation_id"`
	OccurredAt       string                   `json:"occurred_at"`
	Payload          communityRevisionPayload `json:"payload"`
}

func articleDomainEvent(article *model.Article, revision *model.ArticleRevision,
	previousRevision, operation, syncEventID string, aggregateVersion int64,
	at time.Time) (model.ArticleDomainOutbox, error) {
	if article == nil || article.ID == "" || article.AuthorID == "" || aggregateVersion <= 0 ||
		syncEventID == "" || (operation != "published" && operation != "retracted") {
		return model.ArticleDomainOutbox{}, errors.New("invalid community article event")
	}
	uid, err := strconv.ParseInt(article.AuthorID, 10, 64)
	if err != nil || uid <= 0 {
		return model.ArticleDomainOutbox{}, errors.New("invalid community article author UID")
	}
	eventID := fmt.Sprintf("community.article.%s.%s", syncEventID, operation)
	payload := communityRevisionPayload{
		ArticleID: article.ID, Author: communitySubject{"rtw.identity", "platform", article.AuthorID},
		Operation: operation, SearchEvidence: operation == "published", WikiModule: nil,
	}
	if operation == "published" {
		if revision == nil || revision.ArticleID != article.ID || revision.Revision <= 0 ||
			revision.RevisionID == "" || revision.ContentSHA256 == "" || revision.Markdown == "" {
			return model.ArticleDomainOutbox{}, errors.New("published article revision is incomplete")
		}
		payload.RevisionID, payload.Revision = revision.RevisionID, revision.Revision
		payload.SourceRef = "rtw.community.article/" + revision.RevisionID
		payload.SourceObject = revision.SourceObject
		payload.ContentSHA256 = revision.ContentSHA256
		payload.Title, payload.Brief = revision.Title, revision.Brief
		payload.CoverImageURL, payload.ManualTypeTag = revision.CoverImageURL, revision.ManualTypeTag
		payload.SecondaryTags = append([]string(nil), revision.SecondaryTags...)
		payload.Markdown = revision.Markdown
	} else {
		if previousRevision == "" {
			return model.ArticleDomainOutbox{}, errors.New("retraction requires the published revision")
		}
		payload.RevisionID = previousRevision
		payload.SourceRef = "rtw.community.article/" + previousRevision
	}
	envelope := communityArticleEnvelope{
		EventID: eventID, EventType: "community.article." + operation, SchemaVersion: 1,
		Producer: "rtw.community.article", AggregateID: article.ID,
		AggregateVersion: aggregateVersion, OperationID: syncEventID,
		OccurredAt: at.UTC().Format(time.RFC3339Nano), Payload: payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return model.ArticleDomainOutbox{}, err
	}
	return model.ArticleDomainOutbox{
		EventID: eventID, AggregateID: article.ID, AggregateVersion: aggregateVersion,
		EventType: envelope.EventType, Payload: string(body), Status: 0,
	}, nil
}

// RetractPublicationTx advances the community pointer and writes H03 in the
// same transaction as the Article status/delete transition. Legacy published
// rows with no revision are explicitly marked as a migration gap; no fake
// revision ID is emitted for them.
func RetractPublicationTx(ctx context.Context, tx *gorm.DB, article *model.Article,
	syncEventID string, at time.Time) error {
	var pointer model.ArticlePublication
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("article_id = ?", article.ID).First(&pointer).Error
	if err == gorm.ErrRecordNotFound {
		EnsureExtInfo(article)
		article.ExtInfo["h03_publication_gap"] = "legacy_revision_missing"
		return nil
	}
	if err != nil {
		return err
	}
	if pointer.State != "published" || pointer.CurrentRevision == "" {
		return errors.New("community publication pointer conflicts with published article")
	}
	nextVersion := pointer.PointerVersion + 1
	event, err := articleDomainEvent(article, nil, pointer.CurrentRevision,
		"retracted", syncEventID, nextVersion, at)
	if err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return err
	}
	pointer.State, pointer.PointerVersion, pointer.LastEventID = "retracted", nextVersion, event.EventID
	return tx.WithContext(ctx).Save(&pointer).Error
}
