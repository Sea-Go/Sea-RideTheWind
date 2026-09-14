package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func insertAcceptedAuthorityFixture(t *testing.T, store *FavoriteModel, row FavoriteFactOutbox, offset int64) {
	t.Helper()
	hash, err := favoriteJCSHash([]byte(row.Payload))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row.Status = FavoriteFactSent
	row.TechnicalReceiptID = fmt.Sprintf("fixture-receipt-%d", offset)
	row.TechnicalInputHash = hash
	row.TechnicalOffset = offset
	row.TechnicalReceivedAt = &now
	row.DeliveredAt = &now
	if err := store.conn.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func TestFavoriteAuthorityRejectsCrossOwnerAndOldSource(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		id     int64
		change func(*favoriteEvent)
	}{
		{801, func(e *favoriteEvent) { e.Payload.Subject.SubjectID = "1002" }},
		{802, func(e *favoriteEvent) { e.Payload.Subject.AuthorityID = "old.identity" }},
		{803, func(e *favoriteEvent) { e.Payload.SourceRef = "rtw.favorite/old-source" }},
	} {
		item := FavoriteItem{FavoriteId: tc.id, FolderId: 80, UserId: 1001,
			TargetType: "article", TargetId: fmt.Sprintf("article-%d", tc.id)}
		if err := store.conn.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		row, err := favoriteOutbox(item, 1, "assert", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var wire favoriteEvent
		if err := json.Unmarshal([]byte(row.Payload), &wire); err != nil {
			t.Fatal(err)
		}
		tc.change(&wire)
		body, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		row.Payload = string(body)
		insertAcceptedAuthorityFixture(t, store, row, tc.id)
		if _, err := store.AuthoritativeFavoriteFact(ctx, favoriteProducer, row.EventID); !errors.Is(err, ErrFavoriteFactUnavailable) {
			t.Fatalf("forged source %d read as authoritative: %v", tc.id, err)
		}
	}

	// A forged retract cannot move the durable identity to a different UID.
	assertItem := FavoriteItem{FavoriteId: 804, FolderId: 80, UserId: 1001,
		TargetType: "article", TargetId: "article-804"}
	assert, _ := favoriteOutbox(assertItem, 1, "assert", time.Now())
	insertAcceptedAuthorityFixture(t, store, assert, 804)
	retractItem := assertItem
	retractItem.UserId = 1002
	retract, _ := favoriteOutbox(retractItem, 2, "retract", time.Now())
	insertAcceptedAuthorityFixture(t, store, retract, 805)
	if _, err := store.AuthoritativeFavoriteFact(ctx, favoriteProducer, retract.EventID); !errors.Is(err, ErrFavoriteFactUnavailable) {
		t.Fatalf("cross-user retract read as authoritative: %v", err)
	}
}

func TestFavoriteAuthorityLegacyExactIntegerAndUnsafeIDBlock(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	item := FavoriteItem{FavoriteId: 901, FolderId: 90, UserId: 1001,
		TargetType: "article", TargetId: "article-901"}
	if err := store.conn.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	legacy, _ := favoriteOutbox(item, 1, "assert", time.Now())
	legacy.Payload = strings.ReplaceAll(legacy.Payload, `"favorite_id":"901"`, `"favorite_id":901`)
	legacy.Payload = strings.ReplaceAll(legacy.Payload, `"folder_id":"90"`, `"folder_id":90`)
	insertAcceptedAuthorityFixture(t, store, legacy, 901)
	if fact, err := store.AuthoritativeFavoriteFact(ctx, favoriteProducer, legacy.EventID); err != nil ||
		fact.SourceEventHash == "" || fact.SourceEventHash != fact.TechnicalReceipt.InputHash {
		t.Fatalf("already accepted exact legacy integer became unreadable: fact=%+v err=%v", fact, err)
	}

	const highID int64 = 9007199254740993
	high := FavoriteItem{FavoriteId: highID, FolderId: highID, UserId: 1001,
		TargetType: "article", TargetId: "article-unsafe"}
	unsafe, _ := favoriteOutbox(high, 1, "assert", time.Now())
	unsafe.Payload = strings.ReplaceAll(unsafe.Payload, `"favorite_id":"9007199254740993"`, `"favorite_id":9007199254740993`)
	unsafe.Payload = strings.ReplaceAll(unsafe.Payload, `"folder_id":"9007199254740993"`, `"folder_id":9007199254740993`)
	if err := store.conn.Create(&unsafe).Error; err != nil {
		t.Fatal(err)
	}
	var before FavoriteFactOutbox
	if err := store.conn.Where("event_id = ?", unsafe.EventID).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 902, UserId: 1001, Name: "after-blocked"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: highID + 2, FolderId: 902,
		UserId: 1001, TargetType: "article", TargetId: "article-after-blocked"}); err != nil {
		t.Fatal(err)
	}
	called := false
	sender := favoriteSenderFunc(func(_ context.Context, event FavoriteWireEvent, _ json.RawMessage) (FavoriteTechnicalReceipt, error) {
		called = true
		return FavoriteTechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
			TechnicalStatus: "accepted", ReceiptID: "after-blocked", InputHash: strings.Repeat("a", 64),
			Offset: 1, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); sent || !errors.Is(err, ErrFavoriteFactBlocked) || called {
		t.Fatalf("unsafe legacy event was sent or rewritten: sent=%t err=%v called=%t", sent, err, called)
	}
	var unchanged FavoriteFactOutbox
	if err := store.conn.Where("event_id = ?", unsafe.EventID).Take(&unchanged).Error; err != nil ||
		unchanged.Status != FavoriteFactBlocked || unchanged.Payload != before.Payload {
		t.Fatalf("blocked legacy outbox identity changed: row=%+v err=%v", unchanged, err)
	}
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); !sent || err != nil || !called {
		t.Fatalf("blocked legacy head starved next fixed event: sent=%t err=%v called=%t", sent, err, called)
	}
	var next FavoriteFactOutbox
	if err := store.conn.Where("event_id = ?", "favorite.9007199254740995.v1").Take(&next).Error; err != nil ||
		next.Status != FavoriteFactSent || next.TechnicalReceiptID != "after-blocked" {
		t.Fatalf("next event was not technically accepted: row=%+v err=%v", next, err)
	}
}
