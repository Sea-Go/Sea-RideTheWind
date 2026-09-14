package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FavoriteFactPending = 0
	FavoriteFactSent    = 1
	FavoriteFactFailed  = 2
)

var ErrFavoriteOwnerMismatch = errors.New("favorite owner mismatch")

type FavoriteFactOutbox struct {
	EventID          string    `gorm:"primaryKey;type:varchar(128)"`
	FavoriteID       int64     `gorm:"not null;uniqueIndex:uk_favorite_fact_version"`
	AggregateVersion int64     `gorm:"not null;uniqueIndex:uk_favorite_fact_version"`
	Payload          string    `gorm:"type:jsonb;not null"`
	Status           int32     `gorm:"type:smallint;not null;default:0;index"`
	RetryCount       int32     `gorm:"not null;default:0"`
	CreatedAt        time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime;not null"`
}

func (FavoriteFactOutbox) TableName() string { return "favorite_fact_outbox" }

type favoriteSubjectRef struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

type favoriteFactPayload struct {
	SchemaVersion  int                `json:"schema_version"`
	EventID        string             `json:"event_id"`
	Subject        favoriteSubjectRef `json:"subject_ref"`
	TargetType     string             `json:"target_type"`
	TargetID       string             `json:"target_id"`
	TargetRevision *string            `json:"target_revision"` // unavailable from current Article RPC
	Operation      string             `json:"operation"`       // assert or retract
	SourceRef      string             `json:"source_ref"`
	EventTime      string             `json:"event_time"`
	AvailableAt    string             `json:"available_at"`
	FavoriteID     int64              `json:"favorite_id"`
	FolderID       int64              `json:"folder_id"`
}

type favoriteEvent struct {
	EventID          string              `json:"event_id"`
	EventType        string              `json:"event_type"`
	SchemaVersion    int                 `json:"schema_version"`
	Producer         string              `json:"producer"`
	AggregateID      string              `json:"aggregate_id"`
	AggregateVersion int64               `json:"aggregate_version"`
	OperationID      string              `json:"operation_id"`
	OccurredAt       string              `json:"occurred_at"`
	Payload          favoriteFactPayload `json:"payload"`
}

func favoriteOutbox(item FavoriteItem, version int64, operation string, now time.Time) (FavoriteFactOutbox, error) {
	if item.FavoriteId <= 0 || item.FolderId <= 0 || item.UserId <= 0 || item.TargetType == "" || item.TargetId == "" ||
		(version != 1 && version != 2) || (operation != "assert" && operation != "retract") {
		return FavoriteFactOutbox{}, errors.New("invalid favorite fact")
	}
	eventID := fmt.Sprintf("favorite.%d.v%d", item.FavoriteId, version)
	at := now.UTC().Format(time.RFC3339Nano)
	event := favoriteEvent{
		EventID: eventID, EventType: "rtw.favorite." + operation, SchemaVersion: 1,
		Producer: "rtw.community.favorite", AggregateID: strconv.FormatInt(item.FavoriteId, 10),
		AggregateVersion: version, OperationID: eventID, OccurredAt: at,
		Payload: favoriteFactPayload{
			SchemaVersion: 1, EventID: eventID,
			Subject:    favoriteSubjectRef{"rtw.identity", "platform", strconv.FormatInt(item.UserId, 10)},
			TargetType: item.TargetType, TargetID: item.TargetId, TargetRevision: nil,
			Operation: operation, SourceRef: fmt.Sprintf("rtw.favorite/%d", item.FavoriteId),
			EventTime: at, AvailableAt: at, FavoriteID: item.FavoriteId, FolderID: item.FolderId,
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return FavoriteFactOutbox{}, err
	}
	return FavoriteFactOutbox{EventID: eventID, FavoriteID: item.FavoriteId,
		AggregateVersion: version, Payload: string(body), Status: FavoriteFactPending}, nil
}

// InsertFavorite commits the existing favorite ID and its business fact in
// the same owner database. An already saved folder target fails its unique key
// before an outbox row can be committed.
func (m *FavoriteModel) InsertFavorite(ctx context.Context, item *FavoriteItem) error {
	if m == nil || m.conn == nil || item == nil {
		return errors.New("favorite store unavailable")
	}
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var folder FavoriteFolder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("folder_id = ?", item.FolderId).First(&folder).Error; err != nil {
			return err
		}
		if folder.UserId != item.UserId {
			return ErrFavoriteOwnerMismatch
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		fact, err := favoriteOutbox(*item, 1, "assert", time.Now())
		if err != nil {
			return err
		}
		return tx.Create(&fact).Error
	})
}

// DeleteFavoriteByFavoriteId retains the old favorite ID in a retract fact.
// The business row and outbox either both commit or both roll back.
func (m *FavoriteModel) DeleteFavoriteByFavoriteId(ctx context.Context, favoriteID, userID int64) error {
	if m == nil || m.conn == nil || favoriteID <= 0 || userID <= 0 {
		return errors.New("invalid favorite delete")
	}
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item FavoriteItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("favorite_id = ?", favoriteID).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrorNotFound
			}
			return err
		}
		if item.UserId != userID {
			return ErrFavoriteOwnerMismatch
		}
		fact, err := favoriteOutbox(item, 2, "retract", time.Now())
		if err != nil {
			return err
		}
		if err := tx.Create(&fact).Error; err != nil {
			return err
		}
		return tx.Where("favorite_id = ?", favoriteID).Delete(&FavoriteItem{}).Error
	})
}

// DeleteFolderCascade emits one retract for each saved item. Deleting an empty
// folder emits none; a retry cannot fabricate a second business transition.
func (m *FavoriteModel) DeleteFolderCascade(ctx context.Context, folderID, userID int64) error {
	if m == nil || m.conn == nil || folderID <= 0 || userID <= 0 {
		return errors.New("invalid favorite folder delete")
	}
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var folder FavoriteFolder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("folder_id = ?", folderID).First(&folder).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrorNotFound
			}
			return err
		}
		if folder.UserId != userID {
			return ErrFavoriteOwnerMismatch
		}
		var items []FavoriteItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("folder_id = ?", folderID).Order("favorite_id").Find(&items).Error; err != nil {
			return err
		}
		for _, item := range items {
			fact, err := favoriteOutbox(item, 2, "retract", time.Now())
			if err != nil {
				return err
			}
			if err := tx.Create(&fact).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("folder_id = ?", folderID).Delete(&FavoriteItem{}).Error; err != nil {
			return err
		}
		return tx.Where("folder_id = ?", folderID).Delete(&FavoriteFolder{}).Error
	})
}
