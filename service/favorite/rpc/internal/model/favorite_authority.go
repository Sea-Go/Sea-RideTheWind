package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const favoriteProducer = "rtw.community.favorite"
const maxExactJSONInteger int64 = 9007199254740991

var ErrFavoriteFactUnavailable = errors.New("favorite fact unavailable")

// FavoriteAuthorityFact is source-owned evidence for a downstream binder. A DC
// batch alone cannot authorize a subject: the immutable RTW outbox and the
// source-recorded DC technical receipt must both agree with its input hash.
type FavoriteAuthorityFact struct {
	Event              FavoriteWireEvent        `json:"event"`
	SubjectRef         FavoriteSubjectRef       `json:"subject_ref"`
	PredecessorEventID string                   `json:"predecessor_event_id,omitempty"`
	TechnicalReceipt   FavoriteTechnicalReceipt `json:"technical_receipt"`
	SourceEventHash    string                   `json:"source_event_hash"`
}

// FavoriteAuthorityFactV2 is returned only by the versioned private reader.
// The source EventSpec and its DC receipt/hash are still the original evidence.
type FavoriteAuthorityFactV2 struct {
	Event              FavoriteWireEvent        `json:"event"`
	SubjectRef         FavoriteSubjectRefV2     `json:"subject_ref"`
	PredecessorEventID string                   `json:"predecessor_event_id,omitempty"`
	TechnicalReceipt   FavoriteTechnicalReceipt `json:"technical_receipt"`
	SourceEventHash    string                   `json:"source_event_hash"`
}

type authorityPayload struct {
	SchemaVersion  int             `json:"schema_version"`
	EventID        string          `json:"event_id"`
	Subject        json.RawMessage `json:"subject_ref"`
	TargetType     string          `json:"target_type"`
	TargetID       string          `json:"target_id"`
	TargetRevision *string         `json:"target_revision"`
	Operation      string          `json:"operation"`
	SourceRef      string          `json:"source_ref"`
	EventTime      string          `json:"event_time"`
	AvailableAt    string          `json:"available_at"`
	FavoriteID     json.RawMessage `json:"favorite_id"`
	FolderID       json.RawMessage `json:"folder_id"`
}

func strictFavoriteJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

// parseAuthorityID permits exact decimal strings and already delivered legacy
// small JSON integers. Unsafe legacy integers are never rounded or rewritten.
func parseAuthorityID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var text string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return 0, false
		}
	} else {
		text = string(raw)
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
		return 0, false
	}
	if raw[0] != '"' && id > maxExactJSONInteger {
		return 0, false
	}
	return id, true
}

func sameFavoriteRevision(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// favoriteSubjectV1 normalizes the two exact source-owned shapes for internal
// business checks. It never changes the source EventSpec or its JCS hash.
func favoriteSubjectV1(schema int, raw json.RawMessage) (FavoriteSubjectRef, bool) {
	var legacy FavoriteSubjectRef
	switch schema {
	case 1:
		if strictFavoriteJSON(raw, &legacy) != nil || legacy.AuthorityID != "rtw.identity" ||
			legacy.TenantID != "platform" {
			return FavoriteSubjectRef{}, false
		}
	case 2:
		var v2 FavoriteSubjectRefV2
		if strictFavoriteJSON(raw, &v2) != nil || v2.Issuer != "rtw.identity" {
			return FavoriteSubjectRef{}, false
		}
		legacy = FavoriteSubjectRef{AuthorityID: v2.Issuer, TenantID: "platform", SubjectID: v2.SubjectID}
	default:
		return FavoriteSubjectRef{}, false
	}
	uid, err := strconv.ParseInt(legacy.SubjectID, 10, 64)
	return legacy, err == nil && uid > 0 && strconv.FormatInt(uid, 10) == legacy.SubjectID
}

func authoritativeFavoriteRow(row FavoriteFactOutbox) (FavoriteAuthorityFact, authorityPayload, bool) {
	if row.Status != FavoriteFactSent || row.DeliveredAt == nil || row.TechnicalReceivedAt == nil ||
		row.TechnicalReceiptID == "" || row.TechnicalOffset < 1 || !favoriteReceiptHash.MatchString(row.TechnicalInputHash) {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	var event FavoriteWireEvent
	if strictFavoriteJSON([]byte(row.Payload), &event) != nil || !validFavoriteDelivery(row, event) {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	var payload authorityPayload
	if strictFavoriteJSON(event.Payload, &payload) != nil {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	favoriteID, favoriteOK := parseAuthorityID(payload.FavoriteID)
	folderID, folderOK := parseAuthorityID(payload.FolderID)
	subject, subjectOK := favoriteSubjectV1(event.SchemaVersion, payload.Subject)
	if !favoriteOK || !folderOK || favoriteID != row.FavoriteID || folderID <= 0 ||
		!subjectOK || payload.SchemaVersion != event.SchemaVersion || payload.EventID != event.EventID ||
		payload.TargetType == "" || payload.TargetID == "" ||
		payload.SourceRef != fmt.Sprintf("rtw.favorite/%d", row.FavoriteID) ||
		payload.EventTime != event.OccurredAt || payload.AvailableAt != event.OccurredAt {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	operation := "assert"
	if row.AggregateVersion == 2 {
		operation = "retract"
	}
	if payload.Operation != operation || event.EventType != "rtw.favorite."+operation ||
		event.EventID != fmt.Sprintf("favorite.%d.v%d", row.FavoriteID, row.AggregateVersion) {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.AvailableAt); err != nil {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	hash, err := favoriteJCSHash([]byte(row.Payload))
	if err != nil {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	if hash != row.TechnicalInputHash {
		return FavoriteAuthorityFact{}, authorityPayload{}, false
	}
	receipt := FavoriteTechnicalReceipt{EventID: row.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: row.TechnicalReceiptID,
		InputHash: row.TechnicalInputHash, Offset: row.TechnicalOffset,
		ReceivedAt: row.TechnicalReceivedAt.UTC().Format(time.RFC3339Nano)}
	result := FavoriteAuthorityFact{Event: event, SubjectRef: subject,
		TechnicalReceipt: receipt, SourceEventHash: hash}
	if row.AggregateVersion == 2 {
		result.PredecessorEventID = fmt.Sprintf("favorite.%d.v1", row.FavoriteID)
	}
	return result, payload, true
}

func favoriteJCSHash(raw []byte) (string, error) {
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// AuthoritativeFavoriteFact accepts only a fixed producer/event key. The
// caller cannot supply or override a subject. Retractions must match the
// source-owned, DC-accepted assertion for the same owner and target.
func (m *FavoriteModel) AuthoritativeFavoriteFact(ctx context.Context, producer, eventID string) (FavoriteAuthorityFact, error) {
	return m.authoritativeFavoriteFact(ctx, producer, eventID, 1)
}

func (m *FavoriteModel) AuthoritativeFavoriteFactV2(ctx context.Context, producer, eventID string) (FavoriteAuthorityFactV2, error) {
	fact, err := m.authoritativeFavoriteFact(ctx, producer, eventID, 2)
	if err != nil {
		return FavoriteAuthorityFactV2{}, err
	}
	return FavoriteAuthorityFactV2{Event: fact.Event,
		SubjectRef:         FavoriteSubjectRefV2{Issuer: fact.SubjectRef.AuthorityID, SubjectID: fact.SubjectRef.SubjectID},
		PredecessorEventID: fact.PredecessorEventID, TechnicalReceipt: fact.TechnicalReceipt,
		SourceEventHash: fact.SourceEventHash}, nil
}

func (m *FavoriteModel) authoritativeFavoriteFact(ctx context.Context, producer, eventID string,
	expectedSchema int) (FavoriteAuthorityFact, error) {
	if m == nil || m.conn == nil || producer != favoriteProducer || eventID == "" ||
		len(eventID) > 128 || strings.ContainsAny(eventID, "/\\?&#") {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	var row FavoriteFactOutbox
	query := m.conn.WithContext(ctx).Where("event_id = ?", eventID).Limit(1).Find(&row)
	if query.Error != nil {
		return FavoriteAuthorityFact{}, query.Error
	}
	if query.RowsAffected != 1 {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	fact, payload, ok := authoritativeFavoriteRow(row)
	if !ok || fact.Event.SchemaVersion != expectedSchema {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	if row.AggregateVersion != 2 {
		// While the favorite still exists, independently check the business
		// owner/target rather than trusting even the frozen event alone. After
		// deletion, the matching retract outbox is its durable tombstone.
		var item FavoriteItem
		query := m.conn.WithContext(ctx).Where("favorite_id = ?", row.FavoriteID).Limit(1).Find(&item)
		if query.Error != nil {
			return FavoriteAuthorityFact{}, query.Error
		}
		if query.RowsAffected == 1 {
			uid, _ := strconv.ParseInt(fact.SubjectRef.SubjectID, 10, 64)
			folderID, _ := parseAuthorityID(payload.FolderID)
			if item.UserId != uid || item.FolderId != folderID ||
				item.TargetType != payload.TargetType || item.TargetId != payload.TargetID ||
				!sameFavoriteRevision(item.TargetRevision, payload.TargetRevision) {
				return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
			}
		} else {
			var count int64
			if err := m.conn.WithContext(ctx).Model(&FavoriteFactOutbox{}).
				Where("favorite_id = ? AND aggregate_version = 2", row.FavoriteID).Count(&count).Error; err != nil {
				return FavoriteAuthorityFact{}, err
			}
			if count != 1 {
				return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
			}
		}
		return fact, nil
	}
	var liveCount int64
	if err := m.conn.WithContext(ctx).Model(&FavoriteItem{}).
		Where("favorite_id = ?", row.FavoriteID).Count(&liveCount).Error; err != nil {
		return FavoriteAuthorityFact{}, err
	}
	if liveCount != 0 {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	var predecessor FavoriteFactOutbox
	query = m.conn.WithContext(ctx).Where("event_id = ? AND favorite_id = ? AND aggregate_version = 1",
		fact.PredecessorEventID, row.FavoriteID).Limit(1).Find(&predecessor)
	if query.Error != nil {
		return FavoriteAuthorityFact{}, query.Error
	}
	if query.RowsAffected != 1 {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	prior, priorPayload, ok := authoritativeFavoriteRow(predecessor)
	priorFolderID, priorFolderOK := parseAuthorityID(priorPayload.FolderID)
	folderID, folderOK := parseAuthorityID(payload.FolderID)
	if !ok || prior.Event.SchemaVersion != expectedSchema ||
		prior.TechnicalReceipt.Offset >= fact.TechnicalReceipt.Offset ||
		prior.SubjectRef != fact.SubjectRef || priorPayload.TargetType != payload.TargetType ||
		priorPayload.TargetID != payload.TargetID || priorPayload.SourceRef != payload.SourceRef ||
		!priorFolderOK || !folderOK || priorFolderID != folderID ||
		!sameFavoriteRevision(priorPayload.TargetRevision, payload.TargetRevision) {
		return FavoriteAuthorityFact{}, ErrFavoriteFactUnavailable
	}
	return fact, nil
}
