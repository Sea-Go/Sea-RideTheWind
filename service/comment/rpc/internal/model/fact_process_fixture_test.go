package model

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	kqtypes "sea-try-go/service/comment/rpc/common/types"
	"sea-try-go/service/common/communityfact"
)

// TestCommentFactProcessFixture holds a real RTW database open while the
// acceptance script runs the standalone dispatcher, authority, and BTW worker.
func TestCommentFactProcessFixture(t *testing.T) {
	ready := os.Getenv("RTW_COMMENT_FACT_SHARED_READY_FILE")
	release := os.Getenv("RTW_COMMENT_FACT_SHARED_RELEASE_FILE")
	if ready == "" || release == "" {
		t.Skip("run through community_fact_acceptance.sh")
	}
	db := factTestDB(t)
	store := NewCommentModel(db)
	ctx := context.Background()
	createdAt := time.Now().UTC().Add(-time.Minute).Unix()
	message := kqtypes.CommentKafkaMsg{CommentId: 9101, UserId: 1101, OwnerId: 1201,
		TargetType: "article", TargetId: "article-community", Content: "community fixture", CreateTime: createdAt}
	if err := store.InsertCommentTx(ctx, message, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.LikeCommentTx(ctx, 1301, message.CommentId, message.TargetType, message.TargetId, 1, message.OwnerId); err != nil {
		t.Fatal(err)
	}
	if err := store.LikeCommentTx(ctx, 1301, message.CommentId, message.TargetType, message.TargetId, 2, message.OwnerId); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteCommentTx(ctx, message.CommentId, message.OwnerId, message.TargetType, message.TargetId); err != nil {
		t.Fatal(err)
	}
	var rows []CommentDomainFactOutbox
	if err := db.Order("fact_version,event_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("comment fixture facts=%d", len(rows))
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		if row.FactVersion == nil || *row.FactVersion != int64(i+1) {
			t.Fatalf("comment fixture version %d: %+v", i, row)
		}
		ids[i] = row.EventID
	}
	body, err := json.Marshal(map[string]any{"producer": commentFactProducer, "event_ids": ids,
		"creator_subject_id": "1101", "interaction_subject_id": "1301"})
	if err != nil || os.WriteFile(ready, append(body, '\n'), 0600) != nil {
		t.Fatalf("write comment fixture readiness: %v", err)
	}
	waitForFixtureRelease(t, release)
	if err := db.Order("fact_version,event_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if row.DeliveryStatus != communityfact.DeliveryAccepted || row.DeliveredAt == nil ||
			row.TechnicalReceivedAt == nil || row.TechnicalOffset != int64(i+1) || row.TechnicalReceiptID == "" ||
			!communityfact.ValidHash(row.TechnicalInputHash) {
			t.Fatalf("comment fact lacks real DC receipt at %d: %+v", i, row)
		}
		var fact commentFact
		if err := communityfact.StrictDecode(strings.NewReader(row.Payload), &fact); err != nil ||
			fact.TargetRevision != nil || fact.RevisionStatus != "unknown" || fact.SearchEvidence {
			t.Fatalf("comment source precision changed: %+v %v", fact, err)
		}
	}
}
func waitForFixtureRelease(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("comment fact fixture release timed out")
}
