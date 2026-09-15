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
	FavoriteFactBlocked = 3 // frozen envelope requires explicit migration
)

var ErrFavoriteOwnerMismatch = errors.New("favorite owner mismatch")

type FavoriteFactOutbox struct {
	EventID             string `gorm:"primaryKey;type:varchar(128)"`
	FavoriteID          int64  `gorm:"not null;uniqueIndex:uk_favorite_fact_version"`
	AggregateVersion    int64  `gorm:"not null;uniqueIndex:uk_favorite_fact_version"`
	Payload             string `gorm:"type:jsonb;not null"`
	Status              int32  `gorm:"type:smallint;not null;default:0;index"`
	RetryCount          int32  `gorm:"not null;default:0"`
	TechnicalReceiptID  string `gorm:"type:varchar(128)"`
	TechnicalInputHash  string `gorm:"type:char(64)"`
	TechnicalOffset     int64  `gorm:"not null;default:0"`
	TechnicalReceivedAt *time.Time
	DeliveredAt         *time.Time
	CreatedAt           time.Time `gorm:"autoCreateTime;not null"`
	UpdatedAt           time.Time `gorm:"autoUpdateTime;not null"`
}

func (FavoriteFactOutbox) TableName() string { return "favorite_fact_outbox" }

type FavoriteSubjectRef struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

// FavoriteSubjectRefV2 contains only the RTW-owned identity pair. The legacy
// fixed platform slot is deliberately absent from the v2 EventSpec.
type FavoriteSubjectRefV2 struct {
	Issuer    string `json:"issuer"`
	SubjectID string `json:"subject_id"`
}

type favoriteFactPayload struct {
	SchemaVersion  int                `json:"schema_version"`
	EventID        string             `json:"event_id"`
	Subject        FavoriteSubjectRef `json:"subject_ref"`
	TargetType     string             `json:"target_type"`
	TargetID       string             `json:"target_id"`
	TargetRevision *string            `json:"target_revision"` // frozen at FavoriteItem creation
	Operation      string             `json:"operation"`       // assert or retract
	SourceRef      string             `json:"source_ref"`
	EventTime      string             `json:"event_time"`
	AvailableAt    string             `json:"available_at"`
	// Business IDs are decimal strings: DC's JCS input hash rejects JSON
	// integers above 2^53, while production Snowflake IDs exceed that range.
	FavoriteID string `json:"favorite_id"`
	FolderID   string `json:"folder_id"`
}

type favoriteFactPayloadV2 struct {
	SchemaVersion  int                  `json:"schema_version"`
	EventID        string               `json:"event_id"`
	Subject        FavoriteSubjectRefV2 `json:"subject_ref"`
	TargetType     string               `json:"target_type"`
	TargetID       string               `json:"target_id"`
	TargetRevision *string              `json:"target_revision"`
	Operation      string               `json:"operation"`
	SourceRef      string               `json:"source_ref"`
	EventTime      string               `json:"event_time"`
	AvailableAt    string               `json:"available_at"`
	FavoriteID     string               `json:"favorite_id"`
	FolderID       string               `json:"folder_id"`
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
		(version != 1 && version != 2) || (operation != "assert" && operation != "retract") ||
		(item.TargetRevision != nil && (item.TargetType != "article" || *item.TargetRevision == "")) {
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
			Subject:    FavoriteSubjectRef{"rtw.identity", "platform", strconv.FormatInt(item.UserId, 10)},
			TargetType: item.TargetType, TargetID: item.TargetId, TargetRevision: item.TargetRevision,
			Operation: operation, SourceRef: fmt.Sprintf("rtw.favorite/%d", item.FavoriteId),
			EventTime: at, AvailableAt: at, FavoriteID: strconv.FormatInt(item.FavoriteId, 10),
			FolderID: strconv.FormatInt(item.FolderId, 10),
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return FavoriteFactOutbox{}, err
	}
	return FavoriteFactOutbox{EventID: eventID, FavoriteID: item.FavoriteId,
		AggregateVersion: version, Payload: string(body), Status: FavoriteFactPending}, nil
}

func favoriteOutboxV2(item FavoriteItem, version int64, operation string, now time.Time) (FavoriteFactOutbox, error) {
	if item.FavoriteId <= 0 || item.FolderId <= 0 || item.UserId <= 0 || item.TargetType == "" || item.TargetId == "" ||
		(version != 1 && version != 2) || (operation != "assert" && operation != "retract") ||
		(item.TargetRevision != nil && (item.TargetType != "article" || *item.TargetRevision == "")) {
		return FavoriteFactOutbox{}, errors.New("invalid favorite fact")
	}
	eventID := fmt.Sprintf("favorite.%d.v%d", item.FavoriteId, version)
	at := now.UTC().Format(time.RFC3339Nano)
	payload := favoriteFactPayloadV2{SchemaVersion: 2, EventID: eventID,
		Subject:    FavoriteSubjectRefV2{Issuer: "rtw.identity", SubjectID: strconv.FormatInt(item.UserId, 10)},
		TargetType: item.TargetType, TargetID: item.TargetId, TargetRevision: item.TargetRevision,
		Operation: operation, SourceRef: fmt.Sprintf("rtw.favorite/%d", item.FavoriteId),
		EventTime: at, AvailableAt: at, FavoriteID: strconv.FormatInt(item.FavoriteId, 10),
		FolderID: strconv.FormatInt(item.FolderId, 10)}
	// The outer v2 schema is explicit as well. There is still exactly one
	// frozen event for each aggregate version and one DC idempotency key.
	event := struct {
		EventID          string                `json:"event_id"`
		EventType        string                `json:"event_type"`
		SchemaVersion    int                   `json:"schema_version"`
		Producer         string                `json:"producer"`
		AggregateID      string                `json:"aggregate_id"`
		AggregateVersion int64                 `json:"aggregate_version"`
		OperationID      string                `json:"operation_id"`
		OccurredAt       string                `json:"occurred_at"`
		Payload          favoriteFactPayloadV2 `json:"payload"`
	}{eventID, "rtw.favorite." + operation, 2, favoriteProducer,
		strconv.FormatInt(item.FavoriteId, 10), version, eventID, at, payload}
	body, err := json.Marshal(event)
	if err != nil {
		return FavoriteFactOutbox{}, err
	}
	return FavoriteFactOutbox{EventID: eventID, FavoriteID: item.FavoriteId,
		AggregateVersion: version, Payload: string(body), Status: FavoriteFactPending}, nil
}

func (m *FavoriteModel) newFavoriteOutbox(item FavoriteItem, version int64, operation string,
	now time.Time, schema int) (FavoriteFactOutbox, error) {
	switch schema {
	case 1:
		return favoriteOutbox(item, version, operation, now)
	case 2:
		return favoriteOutboxV2(item, version, operation, now)
	default:
		return FavoriteFactOutbox{}, errors.New("unsupported favorite fact schema")
	}
}

func favoriteAssertionSchema(tx *gorm.DB, item FavoriteItem) (int, error) {
	var row FavoriteFactOutbox
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("favorite_id = ? AND aggregate_version = 1", item.FavoriteId).Take(&row).Error; err != nil {
		return 0, err
	}
	var event FavoriteWireEvent
	if strictFavoriteJSON([]byte(row.Payload), &event) != nil ||
		event.EventID != row.EventID || event.AggregateVersion != 1 ||
		event.EventType != "rtw.favorite.assert" || event.AggregateID != strconv.FormatInt(item.FavoriteId, 10) ||
		(event.SchemaVersion != 1 && event.SchemaVersion != 2) ||
		!validFavoriteDelivery(row, event) {
		return 0, ErrFavoriteFactUnavailable
	}
	var payload authorityPayload
	if strictFavoriteJSON(event.Payload, &payload) != nil {
		return 0, ErrFavoriteFactUnavailable
	}
	subject, ok := favoriteSubjectV1(event.SchemaVersion, payload.Subject)
	folderID, folderOK := parseAuthorityID(payload.FolderID)
	if !ok || !folderOK || subject.SubjectID != strconv.FormatInt(item.UserId, 10) ||
		folderID != item.FolderId || payload.TargetType != item.TargetType ||
		payload.TargetID != item.TargetId || !sameFavoriteRevision(payload.TargetRevision, item.TargetRevision) {
		return 0, ErrFavoriteFactUnavailable
	}
	return event.SchemaVersion, nil
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
		schema := 1
		if m.subjectRefV2Facts {
			schema = 2
		}
		fact, err := m.newFavoriteOutbox(*item, 1, "assert", time.Now(), schema)
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
		schema, err := favoriteAssertionSchema(tx, item)
		if err != nil {
			return err
		}
		fact, err := m.newFavoriteOutbox(item, 2, "retract", time.Now(), schema)
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
			schema, err := favoriteAssertionSchema(tx, item)
			if err != nil {
				return err
			}
			fact, err := m.newFavoriteOutbox(item, 2, "retract", time.Now(), schema)
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
