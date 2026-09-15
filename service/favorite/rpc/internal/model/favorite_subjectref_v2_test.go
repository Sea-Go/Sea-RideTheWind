package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFavoriteSubjectRefV2NewFactsAndRetractInheritFrozenSchema(t *testing.T) {
	store := favoriteFactStore(t)
	v2 := NewFavoriteModel(store.conn, WithSubjectRefV2Facts())
	ctx := context.Background()
	for _, folder := range []FavoriteFolder{
		{FolderId: 9101, UserId: 1001, Name: "v2 owner"},
		{FolderId: 9102, UserId: 1002, Name: "v1 owner"},
	} {
		if err := store.InsertFolder(ctx, &folder); err != nil {
			t.Fatal(err)
		}
	}
	if err := v2.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 9007199254743993,
		FolderId: 9101, UserId: 1001, TargetType: "article", TargetId: "article-v2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 9007199254743995,
		FolderId: 9102, UserId: 1002, TargetType: "article", TargetId: "article-v1"}); err != nil {
		t.Fatal(err)
	}
	// Reverse the option at delete time: each retract must follow the schema
	// locked in its own assertion, never the current process switch.
	if err := store.DeleteFavoriteByFavoriteId(ctx, 9007199254743993, 1001); err != nil {
		t.Fatal(err)
	}
	if err := v2.DeleteFavoriteByFavoriteId(ctx, 9007199254743995, 1002); err != nil {
		t.Fatal(err)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 4 {
		t.Fatalf("one assert/retract per favorite: %d", len(rows))
	}
	for _, row := range rows {
		var event FavoriteWireEvent
		if err := strictFavoriteJSON([]byte(row.Payload), &event); err != nil || !validFavoriteDelivery(row, event) {
			t.Fatalf("invalid frozen event %s: %v", row.EventID, err)
		}
		var payload authorityPayload
		if err := strictFavoriteJSON(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		uid := "1001"
		schema := 2
		if row.FavoriteID == 9007199254743995 {
			uid, schema = "1002", 1
		}
		subject, ok := favoriteSubjectV1(schema, payload.Subject)
		if !ok || subject.SubjectID != uid || event.SchemaVersion != schema ||
			payload.SchemaVersion != schema || row.EventID != event.EventID ||
			(strings.Contains(row.EventID, ".v2") != (row.AggregateVersion == 2)) {
			t.Fatalf("owner/schema/version mismatch %s: %+v %+v", row.EventID, event, subject)
		}
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(payload.Subject, &shape); err != nil {
			t.Fatal(err)
		}
		if schema == 2 {
			if len(shape) != 2 || string(shape["issuer"]) != `"rtw.identity"` ||
				string(shape["subject_id"]) != `"1001"` ||
				shape["tenant_id"] != nil || shape["authority_id"] != nil {
				t.Fatalf("v2 subject shape in %s: %s", row.EventID, payload.Subject)
			}
		} else if len(shape) != 3 || string(shape["tenant_id"]) != `"platform"` {
			t.Fatalf("v1 source changed shape in %s: %s", row.EventID, payload.Subject)
		}
	}
	// The PG identity trigger must reject editing a frozen event in place.
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("event_id = ?", rows[0].EventID).
		Update("payload", `{"schema_version":1}`).Error; err == nil {
		t.Fatal("same EventID could be repurposed from v2 to v1")
	}
}

func TestFavoriteSubjectRefV2AuthorityAndBadFrozenVersions(t *testing.T) {
	store := favoriteFactStore(t)
	v2 := NewFavoriteModel(store.conn, WithSubjectRefV2Facts())
	ctx := context.Background()
	if err := v2.InsertFolder(ctx, &FavoriteFolder{FolderId: 9111, UserId: 1001, Name: "authority"}); err != nil {
		t.Fatal(err)
	}
	if err := v2.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 9112, FolderId: 9111,
		UserId: 1001, TargetType: "article", TargetId: "article-r1"}); err != nil {
		t.Fatal(err)
	}
	offset := int64(0)
	sender := favoriteSenderFunc(func(_ context.Context, event FavoriteWireEvent, raw json.RawMessage) (FavoriteTechnicalReceipt, error) {
		offset++
		hash, err := favoriteJCSHash(raw)
		if err != nil {
			return FavoriteTechnicalReceipt{}, err
		}
		return FavoriteTechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
			TechnicalStatus: "accepted", ReceiptID: "receipt-" + event.EventID,
			InputHash: hash, Offset: offset, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); err != nil || !sent {
		t.Fatalf("assert delivery: %t %v", sent, err)
	}
	if _, err := store.AuthoritativeFavoriteFact(ctx, favoriteProducer, "favorite.9112.v1"); !errors.Is(err, ErrFavoriteFactUnavailable) {
		t.Fatalf("v1 reader exposed v2 event: %v", err)
	}
	first, err := store.AuthoritativeFavoriteFactV2(ctx, favoriteProducer, "favorite.9112.v1")
	if err != nil || first.SubjectRef != (FavoriteSubjectRefV2{"rtw.identity", "1001"}) ||
		first.Event.SchemaVersion != 2 || first.SourceEventHash != first.TechnicalReceipt.InputHash {
		t.Fatalf("v2 source assert: %+v %v", first, err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 9112, 1001); err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); err != nil || !sent {
		t.Fatalf("retract delivery: %t %v", sent, err)
	}
	second, err := store.AuthoritativeFavoriteFactV2(ctx, favoriteProducer, "favorite.9112.v2")
	if err != nil || second.PredecessorEventID != first.Event.EventID ||
		second.TechnicalReceipt.Offset != first.TechnicalReceipt.Offset+1 || second.SubjectRef != first.SubjectRef {
		t.Fatalf("v2 source retract: %+v %v", second, err)
	}
	for _, mutate := range []func(map[string]any){
		func(body map[string]any) { body["schema_version"] = float64(3) },
		func(body map[string]any) { body["subject_ref"].(map[string]any)["issuer"] = "old.identity" },
		func(body map[string]any) { body["subject_ref"].(map[string]any)["subject_id"] = "0" },
		func(body map[string]any) { body["subject_ref"].(map[string]any)["tenant_id"] = "platform" },
	} {
		var body map[string]any
		if err := json.Unmarshal(first.Event.Payload, &body); err != nil {
			t.Fatal(err)
		}
		mutate(body)
		bad, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		var payload authorityPayload
		if strictFavoriteJSON(bad, &payload) != nil {
			continue
		}
		_, ok := favoriteSubjectV1(2, payload.Subject)
		if payload.SchemaVersion == 2 && ok {
			t.Fatalf("bad v2 payload passed source checks: %s", bad)
		}
	}
}
