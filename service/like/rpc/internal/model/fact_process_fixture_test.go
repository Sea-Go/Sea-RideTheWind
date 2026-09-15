package model

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/common/communityfact"
)

// TestLikeFactProcessFixture owns the source database used by the standalone
// dispatcher/authority and downstream worker process acceptance.
func TestLikeFactProcessFixture(t *testing.T) {
	ready := os.Getenv("RTW_LIKE_FACT_SHARED_READY_FILE")
	release := os.Getenv("RTW_LIKE_FACT_SHARED_RELEASE_FILE")
	if ready == "" || release == "" {
		t.Skip("run through community_fact_acceptance.sh")
	}
	db := factTestDB(t)
	store := NewLikeRecordModel(db)
	first := likePayload("9201", 1)
	first.Record.UserID, first.Record.TargetID = 1401, "article-community"
	second := likePayload("9202", 2)
	second.Record.UserID, second.Record.TargetID = 1401, "article-community"
	if err := store.ProcessLikeMessageBatch(context.Background(), []*LikeProcessPayload{first, second}); err != nil {
		t.Fatal(err)
	}
	var rows []LikeDomainFactOutbox
	if err := db.Order("fact_version,event_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("like fixture facts=%d", len(rows))
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		if row.FactVersion == nil || *row.FactVersion != int64(i+1) {
			t.Fatalf("like fixture version %d: %+v", i, row)
		}
		ids[i] = row.EventID
	}
	body, err := json.Marshal(map[string]any{"producer": likeFactProducer, "event_ids": ids, "subject_id": "1401"})
	if err != nil || os.WriteFile(ready, append(body, '\n'), 0600) != nil {
		t.Fatalf("write like fixture readiness: %v", err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(release); err != nil {
		t.Fatal("like fact fixture release timed out")
	}
	if err := db.Order("fact_version,event_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if row.DeliveryStatus != communityfact.DeliveryAccepted || row.DeliveredAt == nil ||
			row.TechnicalReceivedAt == nil || row.TechnicalOffset != int64(i+1) || row.TechnicalReceiptID == "" ||
			!communityfact.ValidHash(row.TechnicalInputHash) {
			t.Fatalf("like fact lacks real DC receipt at %d: %+v", i, row)
		}
		var fact likeFact
		if err := communityfact.StrictDecode(strings.NewReader(row.Payload), &fact); err != nil ||
			fact.TargetRevision != nil || fact.RevisionStatus != "unknown" {
			t.Fatalf("like source precision changed: %+v %v", fact, err)
		}
	}
}
