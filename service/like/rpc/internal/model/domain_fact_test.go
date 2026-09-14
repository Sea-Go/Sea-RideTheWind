package model

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func factTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("RTW_LIKE_FACT_TEST_DSN")
	if dsn == "" {
		t.Skip("set RTW_LIKE_FACT_TEST_DSN for isolated PostgreSQL acceptance")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&LikeRecord{}, &LikeConsumeInbox{}, &LikeOutboxEvent{}, &LikeDomainFactOutbox{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func likePayload(id string, action int32) *LikeProcessPayload {
	return &LikeProcessPayload{
		Inbox:      &LikeConsumeInbox{MsgId: id, Topic: "like-topic", Consumer: "like_update_service"},
		Record:     &LikeRecord{UserID: 101, TargetType: "article", TargetID: "200", AuthorID: 202, State: action},
		OccurredAt: 1726300000,
	}
}

func readLikeFacts(t *testing.T, db *gorm.DB, targetID string) []likeFact {
	t.Helper()
	var rows []LikeDomainFactOutbox
	if err := db.Where("payload->>'target_id' = ?", targetID).Order("event_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	facts := make([]likeFact, 0, len(rows))
	for _, row := range rows {
		var fact likeFact
		if err := json.Unmarshal([]byte(row.Payload), &fact); err != nil {
			t.Fatal(err)
		}
		facts = append(facts, fact)
	}
	return facts
}

func TestLikeFactsStateTransitionRetryAndReorder(t *testing.T) {
	db := factTestDB(t)
	model := NewLikeRecordModel(db)
	ctx := context.Background()
	first := likePayload("100", 1)
	first.Outbox = &LikeOutboxEvent{EventID: "hot-100", EventKey: "first_like:101:article:200",
		EventType: "article_hot_like", AggregateID: "200", Payload: `{}`}
	first.IsFirst = true
	if err := model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{first, first}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		id    string
		state int32
	}{
		{"101", 2}, // unlike
		{"102", 2}, // no-op unlike
		{"103", 3}, // dislike
		{"104", 2}, // unlike does not retract dislike
		{"105", 4}, // undislike
		{"99", 1},  // stale Kafka delivery
	} {
		if err := model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{likePayload(action.id, action.state)}); err != nil {
			t.Fatal(err)
		}
	}
	var record LikeRecord
	if err := db.First(&record, "user_id = ? AND target_type = ? AND target_id = ?", 101, "article", "200").Error; err != nil || record.State != 0 || record.LastOperationID != 105 {
		t.Fatalf("state after replay: %+v %v", record, err)
	}
	facts := readLikeFacts(t, db, "200")
	if len(facts) != 4 {
		t.Fatalf("expected four accepted transitions, got %d: %+v", len(facts), facts)
	}
	for _, fact := range facts {
		if fact.SubjectRef != "rtw.identity/platform/101" || fact.TargetRevision != nil ||
			fact.RevisionStatus != "unknown" || fact.SourceRef != fact.EventID {
			t.Fatalf("invalid H09.a fields: %+v", fact)
		}
	}
	if facts[0].OldState != 0 || facts[0].NewState != 1 || facts[0].Operation != "like" ||
		facts[1].OldState != 1 || facts[1].NewState != 0 || facts[1].Operation != "unlike" ||
		facts[2].OldState != 0 || facts[2].NewState != 2 || facts[2].Operation != "dislike" ||
		facts[3].OldState != 2 || facts[3].NewState != 0 || facts[3].Operation != "undislike" {
		t.Fatalf("wrong retract/state semantics: %+v", facts)
	}
	var hotCount int64
	if err := db.Model(&LikeOutboxEvent{}).Count(&hotCount).Error; err != nil || hotCount != 1 {
		t.Fatalf("legacy hot event duplicated: count=%d err=%v", hotCount, err)
	}
	// Same transaction applies two operations on one aggregate in source order.
	if err := model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{likePayload("106", 1), likePayload("107", 2)}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&record, "user_id = ? AND target_type = ? AND target_id = ?", 101, "article", "200").Error; err != nil || record.State != 0 || record.LastOperationID != 107 {
		t.Fatalf("batch order lost: %+v %v", record, err)
	}
	if len(readLikeFacts(t, db, "200")) != 6 {
		t.Fatal("ordered batch did not record both transitions")
	}
	conflict := likePayload("100", 3)
	if err := model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{conflict}); err == nil {
		t.Fatal("same message ID with different action was acknowledged")
	}
}

func TestLikeFactFailureRollsBackInboxAndState(t *testing.T) {
	db := factTestDB(t)
	model := NewLikeRecordModel(db)
	if err := db.Create(&LikeDomainFactOutbox{EventID: "rtw.like/108", EventType: "conflict", Payload: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	conflicting := likePayload("108", 1)
	conflicting.Record.TargetID = "201"
	if err := model.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{conflicting}); err == nil {
		t.Fatal("fact conflict must roll back inbox and state")
	}
	var inboxCount int64
	if err := db.Model(&LikeConsumeInbox{}).Where("msg_id = ?", "108").Count(&inboxCount).Error; err != nil || inboxCount != 0 {
		t.Fatalf("inbox survived failed fact: %d %v", inboxCount, err)
	}
	var recordCount int64
	if err := db.Model(&LikeRecord{}).Where("user_id = ? AND target_type = ? AND target_id = ?", 101, "article", "201").Count(&recordCount).Error; err != nil || recordCount != 0 {
		t.Fatalf("record changed after failed fact: count=%d err=%v", recordCount, err)
	}
	bad := likePayload("109", 1)
	bad.Record.UserID = 0
	if err := model.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{bad}); err == nil {
		t.Fatal("invalid UID accepted")
	}
}

func TestConcurrentLikeMessageIdempotency(t *testing.T) {
	db := factTestDB(t)
	model := NewLikeRecordModel(db)
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := likePayload("200", 1)
			payload.Record.TargetID = "300"
			errors <- model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{payload})
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&LikeDomainFactOutbox{}).Where("event_id = ?", "rtw.like/200").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent duplicate emitted wrong fact count=%d err=%v", count, err)
	}
}

func TestAmbiguousLegacyLikeStateDoesNotEmitFact(t *testing.T) {
	db := factTestDB(t)
	model := NewLikeRecordModel(db)
	if err := db.Create(&LikeRecord{UserID: 101, TargetType: "article", TargetID: "400", AuthorID: 202, State: 2}).Error; err != nil {
		t.Fatal(err)
	}
	message := likePayload("300", 4)
	message.Record.TargetID = "400"
	if err := model.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{message}); err == nil {
		t.Fatal("ambiguous legacy state was interpreted as a new accepted fact")
	}
	var count int64
	if err := db.Model(&LikeDomainFactOutbox{}).Where("event_id = ?", "rtw.like/300").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy state emitted fact count=%d err=%v", count, err)
	}
}
