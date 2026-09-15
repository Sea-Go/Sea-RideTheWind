package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
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

func TestFavoriteSubjectRefV2PreservesHighRTWUIDInJCS(t *testing.T) {
	const uid int64 = 9007199254740993 // above JCS's exact JSON-number range
	item := FavoriteItem{FavoriteId: 9007199254740995, FolderId: 9007199254740997,
		UserId: uid, TargetType: "article", TargetId: "high-uid-article"}
	row, err := favoriteOutboxV2(item, 1, "assert", time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var event FavoriteWireEvent
	if strictFavoriteJSON([]byte(row.Payload), &event) != nil || !validFavoriteDelivery(row, event) {
		t.Fatal("high UID v2 EventSpec invalid")
	}
	var body authorityPayload
	if strictFavoriteJSON(event.Payload, &body) != nil {
		t.Fatal("high UID v2 payload invalid")
	}
	projected, ok := favoriteSubjectV1(2, body.Subject)
	if !ok || projected.SubjectID != "9007199254740993" ||
		!strings.Contains(string(body.Subject), `"subject_id":"9007199254740993"`) ||
		strings.Contains(string(body.Subject), `"tenant_id"`) {
		t.Fatalf("RTW positive int64 UID lost exact v2 representation: %s", body.Subject)
	}
	if hash, err := favoriteJCSHash([]byte(row.Payload)); err != nil || !favoriteReceiptHash.MatchString(hash) {
		t.Fatalf("high UID JCS hash failed: %s %v", hash, err)
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

func TestFavoriteLegacyBusinessRowWithoutAssertBlocksRetractAndCascade(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	const folderID int64 = 9121
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: folderID, UserId: 1001, Name: "legacy gap"}); err != nil {
		t.Fatal(err)
	}
	legacy := FavoriteItem{FavoriteId: 9122, FolderId: folderID, UserId: 1001,
		TargetType: "article", TargetId: "old-without-assert"}
	if err := store.conn.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, legacy.FavoriteId, legacy.UserId); !errors.Is(err, ErrFavoriteFactMigrationBlocked) {
		t.Fatalf("old owner row was deleted without a frozen assert: %v", err)
	}
	var items, rows int64
	if err := store.conn.Model(&FavoriteItem{}).Where("favorite_id = ?", legacy.FavoriteId).Count(&items).Error; err != nil || items != 1 {
		t.Fatalf("legacy business row disappeared: count=%d err=%v", items, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id = ?", legacy.FavoriteId).Count(&rows).Error; err != nil || rows != 0 {
		t.Fatalf("blocked old row fabricated a fact: count=%d err=%v", rows, err)
	}
	if report, err := store.FavoriteLegacyPreflight(ctx); err != nil || report.Clear() ||
		report.MissingAssert != 1 || report.Blocked != 1 || report.ApprovedLegacy != 0 {
		t.Fatalf("unapproved old row passed migration release gate: %+v %v", report, err)
	}
	// A folder cascade may have staged a valid new retract earlier in the
	// transaction. Encountering any old unmapped item must roll all of it back.
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 9120, FolderId: folderID,
		UserId: 1001, TargetType: "article", TargetId: "new-with-assert"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFolderCascade(ctx, folderID, 1001); !errors.Is(err, ErrFavoriteFactMigrationBlocked) {
		t.Fatalf("folder with missing historical assert was deleted: %v", err)
	}
	if err := store.conn.Model(&FavoriteItem{}).Where("folder_id = ?", folderID).Count(&items).Error; err != nil || items != 2 {
		t.Fatalf("folder cascade partially deleted item: count=%d err=%v", items, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id IN ?", []int64{9120, 9122}).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("folder cascade leaked a retract or old assert: count=%d err=%v", rows, err)
	}
	var folders int64
	if err := store.conn.Model(&FavoriteFolder{}).Where("folder_id = ?", folderID).Count(&folders).Error; err != nil || folders != 1 {
		t.Fatalf("blocked cascade removed its folder: count=%d err=%v", folders, err)
	}
	hash, err := favoriteLegacySnapshotHash(legacy)
	if err != nil {
		t.Fatal(err)
	}
	marker := FavoriteLegacyFactMarker{FavoriteID: legacy.FavoriteId, UserID: legacy.UserId,
		FolderID: legacy.FolderId, TargetType: legacy.TargetType, TargetID: legacy.TargetId,
		TargetRevision: legacy.TargetRevision, SourceSnapshotSHA256: hash,
		ApprovalRef: "test-reviewed-pre-outbox-row/9122"}
	if err := store.conn.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if report, err := store.FavoriteLegacyPreflight(ctx); err != nil || !report.Clear() ||
		report.MissingAssert != 1 || report.ApprovedLegacy != 1 {
		t.Fatalf("source-matched marker failed migration release gate: %+v %v", report, err)
	}
	if err := store.DeleteFolderCascade(ctx, folderID, 1001); err != nil {
		t.Fatalf("explicit reviewed legacy marker failed to restore delete: %v", err)
	}
	if err := store.conn.Model(&FavoriteFolder{}).Where("folder_id = ?", folderID).Count(&folders).Error; err != nil || folders != 0 {
		t.Fatalf("approved cascade retained folder: count=%d err=%v", folders, err)
	}
	if err := store.conn.Model(&FavoriteItem{}).Where("folder_id = ?", folderID).Count(&items).Error; err != nil || items != 0 {
		t.Fatalf("approved cascade retained item: count=%d err=%v", items, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id = ?", legacy.FavoriteId).Count(&rows).Error; err != nil || rows != 0 {
		t.Fatalf("approved legacy delete fabricated an orphan retract: count=%d err=%v", rows, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id = ?", 9120).Count(&rows).Error; err != nil || rows != 2 {
		t.Fatalf("approved cascade omitted valid new retract: count=%d err=%v", rows, err)
	}
}

func TestFavoriteLegacyMarkerCannotApproveDifferentOwnerOrTarget(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	item := FavoriteItem{FavoriteId: 9132, FolderId: 9131, UserId: 1001,
		TargetType: "article", TargetId: "old-article"}
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: item.FolderId, UserId: item.UserId, Name: "wrong marker"}); err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	wrong := item
	wrong.UserId = 1002
	hash, err := favoriteLegacySnapshotHash(wrong)
	if err != nil {
		t.Fatal(err)
	}
	marker := FavoriteLegacyFactMarker{FavoriteID: item.FavoriteId, UserID: wrong.UserId,
		FolderID: item.FolderId, TargetType: item.TargetType, TargetID: item.TargetId,
		SourceSnapshotSHA256: hash, ApprovalRef: "test-wrong-owner"}
	if err := store.conn.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if report, err := store.FavoriteLegacyPreflight(ctx); err != nil || report.Clear() || report.Blocked != 1 {
		t.Fatalf("wrong-UID marker passed release gate: %+v %v", report, err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, item.FavoriteId, item.UserId); !errors.Is(err, ErrFavoriteFactMigrationBlocked) {
		t.Fatalf("wrong UID marker approved old item: %v", err)
	}
	var count int64
	if err := store.conn.Model(&FavoriteItem{}).Where("favorite_id = ?", item.FavoriteId).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("wrong marker deleted old item: count=%d err=%v", count, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id = ?", item.FavoriteId).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("wrong marker fabricated a retract: count=%d err=%v", count, err)
	}
}

func TestFavoriteLegacyPreflightRejectsOrphanRetract(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	item := FavoriteItem{FavoriteId: 9142, FolderId: 9141, UserId: 1001,
		TargetType: "article", TargetId: "historical-orphan"}
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: item.FolderId, UserId: item.UserId, Name: "orphan"}); err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := favoriteLegacySnapshotHash(item)
	if err != nil {
		t.Fatal(err)
	}
	marker := FavoriteLegacyFactMarker{FavoriteID: item.FavoriteId, UserID: item.UserId,
		FolderID: item.FolderId, TargetType: item.TargetType, TargetID: item.TargetId,
		SourceSnapshotSHA256: hash, ApprovalRef: "test-orphan-must-block"}
	if err := store.conn.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	retract, err := favoriteOutbox(item, 2, "retract", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Create(&retract).Error; err != nil {
		t.Fatal(err)
	}
	report, err := store.FavoriteLegacyPreflight(ctx)
	if err != nil || report.Clear() || report.OrphanRetracts != 1 {
		t.Fatalf("existing orphan retract passed migration cutover: %+v %v", report, err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, item.FavoriteId, item.UserId); !errors.Is(err, ErrFavoriteFactMigrationBlocked) {
		t.Fatalf("legacy marker overrode an existing orphan retract: %v", err)
	}
	var count int64
	if err := store.conn.Model(&FavoriteItem{}).Where("favorite_id = ?", item.FavoriteId).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("orphan blocker deleted business row: count=%d err=%v", count, err)
	}
}

func TestFavoriteEstablishedBadAssertCannotUseLegacyMarker(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	item := FavoriteItem{FavoriteId: 9162, FolderId: 9161, UserId: 1001,
		TargetType: "article", TargetId: "invalid-established-source"}
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: item.FolderId, UserId: item.UserId, Name: "bad assert"}); err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := favoriteLegacySnapshotHash(item)
	if err != nil {
		t.Fatal(err)
	}
	marker := FavoriteLegacyFactMarker{FavoriteID: item.FavoriteId, UserID: item.UserId,
		FolderID: item.FolderId, TargetType: item.TargetType, TargetID: item.TargetId,
		SourceSnapshotSHA256: hash, ApprovalRef: "test-marker-must-not-override-assert"}
	if err := store.conn.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	asserted, err := favoriteOutbox(item, 1, "assert", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	asserted.Payload = strings.Replace(asserted.Payload,
		`"subject_id":"1001"`, `"subject_id":"1002"`, 1)
	if !strings.Contains(asserted.Payload, `"subject_id":"1002"`) {
		t.Fatal("bad established Outbox fixture did not change owner")
	}
	if err := store.conn.Create(&asserted).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, item.FavoriteId, item.UserId); !errors.Is(err, ErrFavoriteFactMigrationBlocked) {
		t.Fatalf("valid marker overrode an invalid established source: %v", err)
	}
	var itemCount, outboxCount int64
	if err := store.conn.Model(&FavoriteItem{}).Where("favorite_id = ?", item.FavoriteId).Count(&itemCount).Error; err != nil || itemCount != 1 {
		t.Fatalf("bad source deleted business row: count=%d err=%v", itemCount, err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("favorite_id = ?", item.FavoriteId).Count(&outboxCount).Error; err != nil || outboxCount != 1 {
		t.Fatalf("bad source fabricated a retract: count=%d err=%v", outboxCount, err)
	}
}

func TestFavoriteLegacyPreflightProcessBlocksAndClears(t *testing.T) {
	bin := os.Getenv("FAVORITE_LEGACY_PREFLIGHT_BIN")
	if bin == "" {
		t.Skip("use acceptance-dc.sh for isolated legacy preflight executable")
	}
	store := favoriteFactStore(t)
	ctx := context.Background()
	item := FavoriteItem{FavoriteId: 9152, FolderId: 9151, UserId: 1001,
		TargetType: "article", TargetId: "review-required"}
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: item.FolderId, UserId: item.UserId, Name: "process gate"}); err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := store.conn.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(os.Getenv("FAVORITE_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	run := func() (FavoriteLegacyPreflightReport, int) {
		t.Helper()
		command := exec.Command(bin)
		command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL="+address.String())
		var output, diagnostics bytes.Buffer
		command.Stdout, command.Stderr = &output, &diagnostics
		err := command.Run()
		exit := 0
		if err != nil {
			var failed *exec.ExitError
			if !errors.As(err, &failed) {
				t.Fatalf("preflight process failed to start: %v", err)
			}
			exit = failed.ExitCode()
		}
		var report FavoriteLegacyPreflightReport
		diagnosticLines := bytes.SplitN(bytes.TrimSpace(diagnostics.Bytes()), []byte("\n"), 2)
		if json.Unmarshal(output.Bytes(), &report) != nil ||
			len(diagnosticLines) == 0 || !json.Valid(diagnosticLines[0]) {
			t.Fatalf("preflight process did not provide JSON evidence: report=%s diagnostics=%s", output.Bytes(), diagnostics.Bytes())
		}
		return report, exit
	}
	if report, exit := run(); exit != 2 || report.Clear() || report.Blocked != 1 {
		t.Fatalf("preflight executable let unapproved source through: exit=%d report=%+v", exit, report)
	}
	hash, err := favoriteLegacySnapshotHash(item)
	if err != nil {
		t.Fatal(err)
	}
	marker := FavoriteLegacyFactMarker{FavoriteID: item.FavoriteId, UserID: item.UserId,
		FolderID: item.FolderId, TargetType: item.TargetType, TargetID: item.TargetId,
		SourceSnapshotSHA256: hash, ApprovalRef: "test-reviewed-pre-outbox/9152"}
	if err := store.conn.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if report, exit := run(); exit != 0 || !report.Clear() || report.ApprovedLegacy != 1 {
		t.Fatalf("preflight executable ignored reviewed marker: exit=%d report=%+v", exit, report)
	}
}
