package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	kqtypes "sea-try-go/service/comment/rpc/common/types"
	"sea-try-go/service/common/communityfact"
)

type commentLogSender struct{ fail bool }

func (s *commentLogSender) Send(_ context.Context, event communityfact.Event, raw json.RawMessage) (communityfact.TechnicalReceipt, error) {
	if s.fail {
		s.fail = false
		return communityfact.TechnicalReceipt{}, errors.New("temporary DataCenter failure")
	}
	hash, err := communityfact.CanonicalHash(raw)
	if err != nil {
		return communityfact.TechnicalReceipt{}, err
	}
	return communityfact.TechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: "receipt-comment-log", InputHash: hash,
		Offset: 1, ReceivedAt: "2026-09-15T08:00:00Z"}, nil
}

func TestCommentDeliveryUsesInjectedLoggerForAllOutcomes(t *testing.T) {
	db := factTestDB(t)
	if err := db.Model(&CommentDomainFactOutbox{}).Where("delivery_status IN ?", []int32{
		communityfact.DeliveryPending, communityfact.DeliveryFailed}).Update("delivery_status", communityfact.DeliveryBlocked).Error; err != nil {
		t.Fatal(err)
	}
	store := NewCommentModel(db)
	message := kqtypes.CommentKafkaMsg{CommentId: 9701, UserId: 101, OwnerId: 202,
		TargetType: "article", TargetId: "logging", Content: "logging", CreateTime: time.Now().Unix()}
	if err := store.InsertCommentTx(context.Background(), message, 0); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	sender := &commentLogSender{fail: true}
	if sent, err := store.DispatchFactOnce(context.Background(), sender, logger); err == nil || sent {
		t.Fatalf("failed delivery accepted: sent=%v err=%v", sent, err)
	}
	if sent, err := store.DispatchFactOnce(context.Background(), sender, logger); err != nil || !sent {
		t.Fatalf("fixed delivery rejected: sent=%v err=%v", sent, err)
	}
	aggregate, version, envelope := "9801", int64(1), `{}`
	if err := db.Create(&CommentDomainFactOutbox{EventID: "rtw.comment.9801.created",
		EventType: "community.comment.created", Payload: `{}`, AggregateID: &aggregate,
		FactVersion: &version, DeliveryEnvelope: &envelope}).Error; err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchFactOnce(context.Background(), sender, logger); !errors.Is(err, communityfact.ErrFrozenEnvelopeBlocked) || sent {
		t.Fatalf("invalid envelope escaped block: sent=%v err=%v", sent, err)
	}
	log := output.String()
	for _, event := range []string{"comment.fact_delivery.started", "comment.fact_delivery.deferred",
		"comment.fact_delivery.accepted", "comment.fact_delivery.blocked"} {
		if !strings.Contains(log, `"event":"`+event+`"`) {
			t.Fatalf("injected logger missing %s: %s", event, log)
		}
	}
	if strings.Count(log, `"duration_ms":`) < 3 {
		t.Fatalf("terminal delivery logs lack duration: %s", log)
	}
}
