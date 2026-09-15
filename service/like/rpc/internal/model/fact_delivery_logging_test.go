package model

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"sea-try-go/service/common/communityfact"
)

type likeLogSender struct{}

func (likeLogSender) Send(_ context.Context, event communityfact.Event, raw json.RawMessage) (communityfact.TechnicalReceipt, error) {
	hash, err := communityfact.CanonicalHash(raw)
	if err != nil {
		return communityfact.TechnicalReceipt{}, err
	}
	return communityfact.TechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: "receipt-like-log", InputHash: hash,
		Offset: 1, ReceivedAt: "2026-09-15T08:00:00Z"}, nil
}

func TestLikeDeliveryUsesInjectedLogger(t *testing.T) {
	db := factTestDB(t)
	if err := db.Model(&LikeDomainFactOutbox{}).Where("delivery_status IN ?", []int32{
		communityfact.DeliveryPending, communityfact.DeliveryFailed}).Update("delivery_status", communityfact.DeliveryBlocked).Error; err != nil {
		t.Fatal(err)
	}
	payload := likePayload("9901", 1)
	payload.Record.TargetID = "logging"
	if err := NewLikeRecordModel(db).ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{payload}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	if sent, err := NewLikeFactModel(db).DispatchFactOnce(context.Background(), likeLogSender{}, logger); err != nil || !sent {
		t.Fatalf("like delivery rejected: sent=%v err=%v", sent, err)
	}
	log := output.String()
	for _, event := range []string{"like.fact_delivery.started", "like.fact_delivery.accepted"} {
		if !strings.Contains(log, `"event":"`+event+`"`) {
			t.Fatalf("injected logger missing %s: %s", event, log)
		}
	}
	if !strings.Contains(log, `"duration_ms":`) {
		t.Fatalf("like terminal delivery log lacks duration: %s", log)
	}
}
