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
	if err := db.AutoMigrate(&LikeRecord{}, &LikeConsumeInbox{}, &LikeOutboxEvent{}, &LikeDomainFactOutbox{}, &LikeFactStream{}); err != nil {
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

func readLikeDelivery(t *testing.T, db *gorm.DB, id string) likeDeliveryEnvelope {
	t.Helper()
	var row LikeDomainFactOutbox
	if err := db.First(&row, "event_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if row.AggregateID == nil || row.FactVersion == nil || row.DeliveryEnvelope == nil {
		t.Fatalf("like fact has no frozen DC envelope: %+v", row)
	}
	var wire likeDeliveryEnvelope
	if err := json.Unmarshal([]byte(*row.DeliveryEnvelope), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.SchemaVersion != 1 || wire.AggregateVersion != *row.FactVersion ||
		wire.AggregateID != *row.AggregateID || wire.EventID != row.EventID || wire.EventType != row.EventType ||
		wire.Producer != "rtw.like-mq" || wire.OperationID == "" || wire.OccurredAt == "" {
		t.Fatalf("invalid like DC envelope: %+v, row=%+v", wire, row)
	}
	var payload likeFact
	if err := json.Unmarshal(wire.Payload, &payload); err != nil || payload.EventID != row.EventID ||
		payload.SchemaVersion != "rtw.community-fact.v1" || payload.AggregateVersion != nil {
		t.Fatalf("business fact was overwritten by wire version: %+v %v", payload, err)
	}
	return wire
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
	for i, id := range []string{"100", "101", "103", "105"} {
		wire := readLikeDelivery(t, db, "rtw.like/"+id)
		if wire.AggregateID != "like-state/101/article/200" || wire.AggregateVersion != int64(i+1) {
			t.Fatalf("like transition version %d: %+v", i, wire)
		}
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
	if readLikeDelivery(t, db, "rtw.like/106").AggregateVersion != 5 ||
		readLikeDelivery(t, db, "rtw.like/107").AggregateVersion != 6 {
		t.Fatal("like batch did not allocate contiguous transactional fact versions")
	}
	conflict := likePayload("100", 3)
	if err := model.ProcessLikeMessageBatch(ctx, []*LikeProcessPayload{conflict}); err == nil {
		t.Fatal("same message ID with different action was acknowledged")
	}
}

func TestUnversionedLegacyLikeFactBlocksNewAggregateVersion(t *testing.T) {
	db := factTestDB(t)
	if err := db.Create(&LikeDomainFactOutbox{EventID: "legacy-like-401", EventType: "legacy",
		Payload: `{"subject_ref":"rtw.identity/platform/101","target_type":"article","target_id":"401"}`}).Error; err != nil {
		t.Fatal(err)
	}
	p := likePayload("401", 1)
	p.Record.TargetID = "401"
	if err := NewLikeRecordModel(db).ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{p}); err == nil {
		t.Fatal("new like fact followed an unresolved legacy source")
	}
	var count int64
	if err := db.Model(&LikeRecord{}).Where("user_id = ? AND target_id = ?", 101, "401").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy gate did not roll back like state: %d %v", count, err)
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
	if err := db.Model(&LikeFactStream{}).Where("aggregate_id = ?", "like-state/101/article/201").Count(&recordCount).Error; err != nil || recordCount != 0 {
		t.Fatalf("fact stream version survived failed like transaction: count=%d err=%v", recordCount, err)
	}
	bad := likePayload("109", 1)
	bad.Record.UserID = 0
	if err := model.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{bad}); err == nil {
		t.Fatal("invalid UID accepted")
	}
}

func TestLikeFactVersionsArePerUserTarget(t *testing.T) {
	db := factTestDB(t)
	model := NewLikeRecordModel(db)
	first := likePayload("501", 1)
	first.Record.TargetID = "501"
	second := likePayload("502", 1)
	second.Record.TargetID = "501"
	second.Record.UserID = 102
	for _, p := range []*LikeProcessPayload{first, second} {
		if err := model.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{p}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ id, aggregate string }{
		{"rtw.like/501", "like-state/101/article/501"},
		{"rtw.like/502", "like-state/102/article/501"},
	} {
		if wire := readLikeDelivery(t, db, tc.id); wire.AggregateID != tc.aggregate || wire.AggregateVersion != 1 {
			t.Fatalf("independent like state stream: %+v", wire)
		}
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
