package mqs

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/like/rpc/internal/mq/internal/svc"
	"sea-try-go/service/like/rpc/internal/types"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConsumerAcknowledgesOnlyCommittedDomainFact(t *testing.T) {
	logger.Init("like-mq-fact-test")
	dsn := os.Getenv("RTW_LIKE_CONSUMER_FACT_TEST_DSN")
	if dsn == "" {
		t.Skip("set RTW_LIKE_CONSUMER_FACT_TEST_DSN for isolated PostgreSQL acceptance")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.LikeRecord{}, &model.LikeConsumeInbox{}, &model.LikeOutboxEvent{}, &model.LikeDomainFactOutbox{}); err != nil {
		t.Fatal(err)
	}
	service := NewLikeUpdateService(context.Background(), &svc.ServiceContext{
		LikeRecordModel: model.NewLikeRecordModel(db), LikeConsumeInboxModel: model.NewLikeConsumeInboxModel(db),
	})
	msg := types.KafkaLikeMsg{MsgID: "1000", UserId: 501, TargetType: "article", TargetId: "901",
		AuthorID: 601, State: 1, IsFirst: false, CreatedAt: 1726300000}
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.LikeDomainFactOutbox{EventID: "rtw.like/1000", EventType: "conflict", Payload: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Consume(context.Background(), "501", string(encoded)); err == nil {
		t.Fatal("consumer acknowledged a fact transaction that failed")
	}
	var count int64
	if err := db.Model(&model.LikeConsumeInbox{}).Where("msg_id = ?", msg.MsgID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed consume left inbox: count=%d err=%v", count, err)
	}
	if err := db.Model(&model.LikeRecord{}).Where("user_id = ? AND target_id = ?", msg.UserId, msg.TargetId).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed consume left state: count=%d err=%v", count, err)
	}
	if err := db.Delete(&model.LikeDomainFactOutbox{}, "event_id = ?", "rtw.like/1000").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := service.Consume(context.Background(), "501", string(encoded)); err != nil {
			t.Fatalf("consume retry %d: %v", i, err)
		}
	}
	if err := db.Model(&model.LikeDomainFactOutbox{}).Where("event_id = ?", "rtw.like/1000").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retry did not commit exactly one fact: count=%d err=%v", count, err)
	}
	var inbox model.LikeConsumeInbox
	if err := db.First(&inbox, "msg_id = ?", msg.MsgID).Error; err != nil || inbox.Status != 1 {
		t.Fatalf("inbox not completed: %+v %v", inbox, err)
	}
}
