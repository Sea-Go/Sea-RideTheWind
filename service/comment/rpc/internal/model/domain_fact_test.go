package model

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	kqtypes "sea-try-go/service/comment/rpc/common/types"
)

func factTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("RTW_COMMENT_FACT_TEST_DSN")
	if dsn == "" {
		t.Skip("set RTW_COMMENT_FACT_TEST_DSN for isolated PostgreSQL acceptance")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Subject{}, &CommentContent{}, &CommentIndex{}, &CommentLike{}, &CommentDomainFactOutbox{}, &CommentFactStream{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func countCommentFacts(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&CommentDomainFactOutbox{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func readCommentFact(t *testing.T, db *gorm.DB, id string) commentFact {
	t.Helper()
	var row CommentDomainFactOutbox
	if err := db.First(&row, "event_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	var fact commentFact
	if err := json.Unmarshal([]byte(row.Payload), &fact); err != nil {
		t.Fatal(err)
	}
	return fact
}

func readCommentDelivery(t *testing.T, db *gorm.DB, id string) commentDeliveryEnvelope {
	t.Helper()
	var row CommentDomainFactOutbox
	if err := db.First(&row, "event_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if row.AggregateID == nil || row.FactVersion == nil || row.DeliveryEnvelope == nil {
		t.Fatalf("comment fact has no frozen DC envelope: %+v", row)
	}
	var wire commentDeliveryEnvelope
	if err := json.Unmarshal([]byte(*row.DeliveryEnvelope), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.SchemaVersion != 1 || wire.AggregateVersion != *row.FactVersion ||
		wire.AggregateID != *row.AggregateID || wire.EventID != row.EventID || wire.EventType != row.EventType ||
		wire.Producer != "rtw.comment-rpc" || wire.OperationID == "" || wire.OccurredAt == "" {
		t.Fatalf("invalid comment DC envelope: %+v, row=%+v", wire, row)
	}
	var payload commentFact
	if err := json.Unmarshal(wire.Payload, &payload); err != nil || payload.EventID != row.EventID ||
		payload.SchemaVersion != "rtw.community-fact.v1" || payload.AggregateVersion != nil {
		t.Fatalf("business fact was overwritten by wire version: %+v %v", payload, err)
	}
	return wire
}

func TestCommentFactsCommitRetryAndRetract(t *testing.T) {
	db := factTestDB(t)
	model := NewCommentModel(db)
	ctx := context.Background()
	root := kqtypes.CommentKafkaMsg{CommentId: 1001, UserId: 101, OwnerId: 202,
		TargetType: "article", TargetId: "200", Content: "hello", CreateTime: time.Now().Unix()}
	if err := model.InsertCommentTx(ctx, root, 0); err != nil {
		t.Fatal(err)
	}
	conflictingRoot := root
	conflictingRoot.Content = "different"
	if err := model.InsertCommentTx(ctx, conflictingRoot, 0); err == nil {
		t.Fatal("same comment ID with different content was acknowledged")
	}
	if err := model.InsertCommentTx(ctx, root, 0); err != nil {
		t.Fatal(err)
	}
	created := readCommentFact(t, db, "rtw.comment.1001.created")
	if wire := readCommentDelivery(t, db, "rtw.comment.1001.created"); wire.AggregateID != "1001" || wire.AggregateVersion != 1 {
		t.Fatalf("comment create version: %+v", wire)
	}
	if created.SubjectRef != "rtw.identity/platform/101" || created.TargetRevision != nil ||
		created.RevisionStatus != "unknown" || created.SearchEvidence || created.VisibilityState != 0 ||
		created.Operation != "create" || created.SourceRef != "rtw.comment/1001" {
		t.Fatalf("unexpected create fact: %+v", created)
	}
	if countCommentFacts(t, db) != 1 {
		t.Fatal("duplicate create emitted a fact")
	}

	reply := kqtypes.CommentKafkaMsg{CommentId: 1002, UserId: 303, OwnerId: 202,
		TargetType: "article", TargetId: "200", RootId: 1001, ParentId: 1001,
		Content: "reply", CreateTime: time.Now().Unix()}
	if err := model.InsertCommentTx(ctx, reply, 1); err != nil {
		t.Fatal(err)
	}
	if fact := readCommentFact(t, db, "rtw.comment.1002.created"); fact.ParentCommentID != "1001" || fact.VisibilityState != 1 {
		t.Fatalf("reply fact lost parent or audit state: %+v", fact)
	}
	if _, err := model.DeleteCommentTx(ctx, 1002, 303, "article", "wrong"); !errors.Is(err, ErrorCommentNotFound) {
		t.Fatalf("cross-target delete: %v", err)
	}
	if _, err := model.DeleteCommentTx(ctx, 1002, 404, "article", "200"); !errors.Is(err, ErrorCommentForbidden) {
		t.Fatalf("unauthorized delete: %v", err)
	}
	for i := 0; i < 2; i++ {
		remaining, err := model.DeleteCommentTx(ctx, 1002, 202, "article", "200")
		if err != nil || remaining != 1 {
			t.Fatalf("delete retry %d: count=%d err=%v", i, remaining, err)
		}
	}
	deleted := readCommentFact(t, db, "rtw.comment.1002.deleted")
	if wire := readCommentDelivery(t, db, "rtw.comment.1002.deleted"); wire.AggregateID != "1002" || wire.AggregateVersion != 2 {
		t.Fatalf("comment retract version: %+v", wire)
	}
	if deleted.SubjectRef != "rtw.identity/platform/303" || deleted.OperatorRef != "rtw.identity/platform/202" || deleted.Operation != "retract" {
		t.Fatalf("wrong retract ownership: %+v", deleted)
	}
	var parent CommentIndex
	if err := db.First(&parent, "id = ?", 1001).Error; err != nil || parent.ReplyCount != 0 {
		t.Fatalf("duplicate delete changed parent count: %+v %v", parent, err)
	}
	if countCommentFacts(t, db) != 3 {
		t.Fatal("delete retry emitted duplicate retract")
	}

	for _, action := range []int32{1, 1, 3, 4, 2} {
		if err := model.LikeCommentTx(ctx, 404, 1001, "article", "200", action, 202); err != nil {
			t.Fatal(err)
		}
	}
	var state CommentLike
	if err := db.First(&state, "user_id = ? AND comment_id = ?", 404, 1001).Error; err != nil || state.State != 0 {
		t.Fatalf("comment interaction state: %+v %v", state, err)
	}
	if err := db.First(&parent, "id = ?", 1001).Error; err != nil || parent.LikeCount != 0 || parent.DislikeCount != 0 {
		t.Fatalf("comment counters: %+v %v", parent, err)
	}
	if countCommentFacts(t, db) != 6 {
		t.Fatalf("only three real interaction transitions expected, got %d facts", countCommentFacts(t, db))
	}
	var interactions []CommentDomainFactOutbox
	if err := db.Where("aggregate_id = ? AND event_type = ?", "1001", "community.comment.interaction").
		Order("fact_version").Find(&interactions).Error; err != nil {
		t.Fatal(err)
	}
	if len(interactions) != 3 {
		t.Fatalf("expected three interaction versions, got %d", len(interactions))
	}
	for i, row := range interactions {
		if wire := readCommentDelivery(t, db, row.EventID); wire.AggregateVersion != int64(i+2) {
			t.Fatalf("comment interaction version %d: %+v", i, wire)
		}
	}
}

func TestUnversionedLegacyCommentFactBlocksNewAggregateVersion(t *testing.T) {
	db := factTestDB(t)
	if err := db.Create(&CommentDomainFactOutbox{EventID: "legacy-comment-1501", EventType: "legacy",
		Payload: `{"aggregate_id":"1501"}`}).Error; err != nil {
		t.Fatal(err)
	}
	msg := kqtypes.CommentKafkaMsg{CommentId: 1501, UserId: 101, OwnerId: 202,
		TargetType: "article", TargetId: "501", Content: "legacy", CreateTime: time.Now().Unix()}
	if err := NewCommentModel(db).InsertCommentTx(context.Background(), msg, 0); err == nil {
		t.Fatal("new comment fact followed an unresolved legacy source")
	}
	var count int64
	if err := db.Model(&CommentIndex{}).Where("id = ?", 1501).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy gate did not roll back business row: %d %v", count, err)
	}
}

func TestCommentFactFailureRollsBackBusinessState(t *testing.T) {
	db := factTestDB(t)
	model := NewCommentModel(db)
	ctx := context.Background()
	if err := db.Create(&CommentDomainFactOutbox{EventID: "rtw.comment.1003.created", EventType: "conflict", Payload: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	msg := kqtypes.CommentKafkaMsg{CommentId: 1003, UserId: 101, OwnerId: 202,
		TargetType: "article", TargetId: "201", Content: "rollback", CreateTime: time.Now().Unix()}
	if err := model.InsertCommentTx(ctx, msg, 0); err == nil {
		t.Fatal("fact conflict must roll back comment")
	}
	var count int64
	if err := db.Model(&CommentIndex{}).Where("id = ?", 1003).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("comment survived failed fact: count=%d err=%v", count, err)
	}
	if err := db.Model(&Subject{}).Where("target_type = ? AND target_id = ?", "article", "201").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("subject counter survived failed fact: count=%d err=%v", count, err)
	}
	if err := db.Model(&CommentFactStream{}).Where("aggregate_id = ?", "1003").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("fact stream version survived failed comment transaction: count=%d err=%v", count, err)
	}
	msg.UserId = 0
	if err := model.InsertCommentTx(ctx, msg, 0); err == nil {
		t.Fatal("invalid UID accepted")
	}
}

func TestConcurrentCommentLikeProducesOneTransition(t *testing.T) {
	db := factTestDB(t)
	model := NewCommentModel(db)
	ctx := context.Background()
	msg := kqtypes.CommentKafkaMsg{CommentId: 1101, UserId: 111, OwnerId: 222,
		TargetType: "article", TargetId: "300", Content: "concurrent", CreateTime: time.Now().Unix()}
	if err := model.InsertCommentTx(ctx, msg, 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- model.LikeCommentTx(ctx, 333, 1101, "article", "300", 1, 222)
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var index CommentIndex
	if err := db.First(&index, "id = ?", 1101).Error; err != nil || index.LikeCount != 1 {
		t.Fatalf("concurrent like counted twice: %+v %v", index, err)
	}
	var count int64
	if err := db.Model(&CommentDomainFactOutbox{}).Where("event_type = ? AND payload->>'comment_id' = ?", "community.comment.interaction", "1101").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent like emitted duplicate facts: count=%d err=%v", count, err)
	}
}
