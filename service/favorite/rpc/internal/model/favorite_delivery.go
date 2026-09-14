package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var favoriteReceiptHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

var ErrFavoriteFactBlocked = errors.New("favorite fact frozen envelope blocked")

// FavoriteWireEvent is the DC H04 envelope stored verbatim in the RTW business
// transaction. Its payload stays owned by the favorite domain.
type FavoriteWireEvent struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	Producer         string          `json:"producer"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	OperationID      string          `json:"operation_id"`
	OccurredAt       string          `json:"occurred_at"`
	Payload          json.RawMessage `json:"payload"`
}

type FavoriteTechnicalReceipt struct {
	EventID         string `json:"event_id"`
	Producer        string `json:"producer"`
	TechnicalStatus string `json:"technical_status"`
	ReceiptID       string `json:"receipt_id"`
	InputHash       string `json:"input_hash"`
	Offset          int64  `json:"offset"`
	ReceivedAt      string `json:"received_at"`
}

type FavoriteFactSender interface {
	Send(context.Context, FavoriteWireEvent, json.RawMessage) (FavoriteTechnicalReceipt, error)
}

// FavoriteDCEventSender posts only the frozen envelope to the DC eventing
// endpoint. Failed or lost HTTP responses leave the same RTW event retryable.
type FavoriteDCEventSender struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewFavoriteDCEventSender(endpoint, token string, client *http.Client) *FavoriteDCEventSender {
	return &FavoriteDCEventSender{endpoint: endpoint, token: token, client: client}
}

func (s *FavoriteDCEventSender) Send(ctx context.Context, event FavoriteWireEvent, raw json.RawMessage) (FavoriteTechnicalReceipt, error) {
	if s == nil || s.client == nil || s.token == "" {
		return FavoriteTechnicalReceipt{}, errors.New("favorite DC sender is not configured")
	}
	u, err := url.Parse(s.endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return FavoriteTechnicalReceipt{}, errors.New("favorite DC event endpoint is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return FavoriteTechnicalReceipt{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", event.EventID)
	request.Header.Set("Authorization", "Bearer "+s.token)
	response, err := s.client.Do(request)
	if err != nil {
		return FavoriteTechnicalReceipt{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return FavoriteTechnicalReceipt{}, fmt.Errorf("DC event acceptance status %d", response.StatusCode)
	}
	var receipt FavoriteTechnicalReceipt
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&receipt); err != nil {
		return FavoriteTechnicalReceipt{}, fmt.Errorf("decode DC event receipt: %w", err)
	}
	return receipt, nil
}

func validFavoriteDelivery(row FavoriteFactOutbox, event FavoriteWireEvent) bool {
	if event.EventID != row.EventID || event.Producer != "rtw.community.favorite" ||
		(event.EventType != "rtw.favorite.assert" && event.EventType != "rtw.favorite.retract") ||
		event.SchemaVersion != 1 || event.AggregateID != fmt.Sprint(row.FavoriteID) ||
		event.AggregateVersion != row.AggregateVersion || event.OperationID != row.EventID ||
		!json.Valid(event.Payload) || len(event.Payload) == 0 || event.Payload[0] != '{' {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if err != nil {
		return false
	}
	var payload authorityPayload
	if strictFavoriteJSON(event.Payload, &payload) != nil {
		return false
	}
	favoriteID, favoriteOK := parseAuthorityID(payload.FavoriteID)
	_, folderOK := parseAuthorityID(payload.FolderID)
	return favoriteOK && folderOK && favoriteID == row.FavoriteID
}

func validFavoriteReceipt(event FavoriteWireEvent, receipt FavoriteTechnicalReceipt) (time.Time, bool) {
	if receipt.EventID != event.EventID || receipt.Producer != event.Producer ||
		receipt.TechnicalStatus != "accepted" || receipt.ReceiptID == "" ||
		!favoriteReceiptHash.MatchString(receipt.InputHash) || receipt.Offset < 1 {
		return time.Time{}, false
	}
	received, err := time.Parse(time.RFC3339Nano, receipt.ReceivedAt)
	return received.UTC(), err == nil
}

// DispatchFavoriteFactOnce keeps one outbox row locked until DC has returned
// its matching durable technical receipt. Multiple dispatchers skip the locked
// row; an uncertain HTTP result retries the identical event ID and payload.
func (m *FavoriteModel) DispatchFavoriteFactOnce(ctx context.Context, sender FavoriteFactSender) (bool, error) {
	if m == nil || m.conn == nil || sender == nil {
		return false, errors.New("favorite fact dispatcher is not configured")
	}
	tx := m.conn.WithContext(ctx).Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	defer tx.Rollback()
	var row FavoriteFactOutbox
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status IN ?", []int32{FavoriteFactPending, FavoriteFactFailed}).
		Where(`aggregate_version=1 OR EXISTS (
			SELECT 1 FROM favorite_fact_outbox predecessor
			WHERE predecessor.favorite_id=favorite_fact_outbox.favorite_id
				AND predecessor.aggregate_version=1 AND predecessor.status=1)`).
		Order("created_at,event_id").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var event FavoriteWireEvent
	if err := json.Unmarshal([]byte(row.Payload), &event); err != nil || !validFavoriteDelivery(row, event) {
		if updateErr := tx.Model(&FavoriteFactOutbox{}).Where("event_id = ?", row.EventID).
			Update("status", FavoriteFactBlocked).Error; updateErr != nil {
			return false, updateErr
		}
		if commitErr := tx.Commit().Error; commitErr != nil {
			return false, commitErr
		}
		slog.WarnContext(ctx, "favorite fact frozen envelope blocked", "event", "favorite.delivery.blocked",
			"event_id", row.EventID, "producer", favoriteProducer, "outcome", "manual_migration_required",
			"error_code", "INVALID_FROZEN_ENVELOPE")
		return false, ErrFavoriteFactBlocked
	}
	slog.InfoContext(ctx, "favorite fact delivery started", "event", "favorite.delivery.started",
		"event_id", event.EventID, "producer", event.Producer, "aggregate_version", event.AggregateVersion)
	receipt, sendErr := sender.Send(ctx, event, json.RawMessage(row.Payload))
	received, receiptOK := validFavoriteReceipt(event, receipt)
	if sendErr != nil || !receiptOK {
		if sendErr == nil {
			sendErr = errors.New("DC technical receipt does not match favorite event")
		}
		if err := tx.Model(&FavoriteFactOutbox{}).Where("event_id = ?", row.EventID).
			Updates(map[string]any{"status": FavoriteFactFailed, "retry_count": gorm.Expr("retry_count + 1")}).Error; err != nil {
			return false, err
		}
		if err := tx.Commit().Error; err != nil {
			return false, err
		}
		slog.WarnContext(ctx, "favorite fact delivery deferred", "event", "favorite.delivery.deferred",
			"event_id", event.EventID, "producer", event.Producer, "outcome", "retryable",
			"retry_count", row.RetryCount+1)
		return false, sendErr
	}
	deliveredAt := time.Now().UTC()
	if err := tx.Model(&FavoriteFactOutbox{}).Where("event_id = ?", row.EventID).
		Updates(map[string]any{"status": FavoriteFactSent, "technical_receipt_id": receipt.ReceiptID,
			"technical_input_hash": receipt.InputHash, "technical_offset": receipt.Offset,
			"technical_received_at": received, "delivered_at": deliveredAt}).Error; err != nil {
		return false, err
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	slog.InfoContext(ctx, "favorite fact technical receipt committed", "event", "favorite.delivery.accepted",
		"event_id", event.EventID, "producer", event.Producer, "outcome", "succeeded",
		"receipt_id", receipt.ReceiptID, "offset", receipt.Offset)
	return true, nil
}
