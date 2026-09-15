package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// This test runs only with the isolated PG16 and real DataCenter cmd/platform
// supplied by acceptance-dc.sh. It proves the default v1 and explicitly
// enabled v2 share one producer offset sequence without emitting both wires
// for either business version.
func TestFavoriteSubjectRefV2RealDataCenterMixedProducer(t *testing.T) {
	baseURL, token := os.Getenv("FAVORITE_DC_URL"), os.Getenv("FAVORITE_DC_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("use acceptance-dc.sh with isolated real DataCenter")
	}
	store := favoriteFactStore(t)
	v2 := NewFavoriteModel(store.conn, WithSubjectRefV2Facts())
	ctx := context.Background()
	const u1ID, u2ID int64 = 9007199254744993, 9007199254744995
	for _, folder := range []FavoriteFolder{
		{FolderId: 9007199254744991, UserId: 1001, Name: "dc-v2-owner"},
		{FolderId: 9007199254744992, UserId: 1002, Name: "dc-v1-owner"},
	} {
		if err := store.InsertFolder(ctx, &folder); err != nil {
			t.Fatal(err)
		}
	}
	if err := v2.InsertFavorite(ctx, &FavoriteItem{FavoriteId: u1ID, FolderId: 9007199254744991,
		UserId: 1001, TargetType: "article", TargetId: "article-v2-dc"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: u2ID, FolderId: 9007199254744992,
		UserId: 1002, TargetType: "article", TargetId: "article-v1-dc"}); err != nil {
		t.Fatal(err)
	}
	authorityURL, authorityToken := startFavoriteAuthorityProcess(t, store)
	endpoint := baseURL + "/v1/events"
	for i := 0; i < 2; i++ {
		if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
			t.Fatalf("mixed producer dispatch %d: %v", i, err)
		}
	}
	u1AssertID := fmt.Sprintf("favorite.%d.v1", u1ID)
	u2AssertID := fmt.Sprintf("favorite.%d.v1", u2ID)
	client := &http.Client{Timeout: 3 * time.Second}
	u1Receipt := readFavoriteDCReceipt(t, client, baseURL, token, u1AssertID)
	u2Receipt := readFavoriteDCReceipt(t, client, baseURL, token, u2AssertID)
	if u2Receipt.Offset != u1Receipt.Offset+1 || u1Receipt.ReceiptID == u2Receipt.ReceiptID {
		t.Fatalf("two business assertions did not have distinct continuous DC receipts: %+v %+v", u1Receipt, u2Receipt)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, favoriteProducer, u1AssertID); status != 404 {
		t.Fatalf("v1 reader projected new v2 EventSpec: %d", status)
	}
	status, u1Source := readFavoriteAuthorityV2(t, authorityURL, authorityToken, favoriteProducer, u1AssertID)
	if status != 200 || u1Source.SubjectRef != (FavoriteSubjectRefV2{"rtw.identity", "1001"}) ||
		u1Source.Event.SchemaVersion != 2 || u1Source.SourceEventHash != u1Receipt.InputHash ||
		u1Source.TechnicalReceipt.ReceiptID != u1Receipt.ReceiptID ||
		u1Source.TechnicalReceipt.Offset != u1Receipt.Offset {
		t.Fatalf("RTW v2 authority differs from DC acceptance: status=%d source=%+v dc=%+v", status, u1Source, u1Receipt)
	}
	if status, _ := readFavoriteAuthorityV2(t, authorityURL, authorityToken, favoriteProducer, u2AssertID); status != 404 {
		t.Fatalf("v2 reader projected old v1 EventSpec: %d", status)
	}
	if status, u2Source := readFavoriteAuthority(t, authorityURL, authorityToken, favoriteProducer, u2AssertID); status != 200 || u2Source.SubjectRef.SubjectID != "1002" || u2Source.Event.SchemaVersion != 1 ||
		u2Source.SourceEventHash != u2Receipt.InputHash {
		t.Fatalf("old v1 source regressed: %d %+v", status, u2Source)
	}
	// The delete process is now back at its default v1 switch. It must emit
	// one v2 retract for the original v2 favorite, never a second v1 body.
	if err := store.DeleteFavoriteByFavoriteId(ctx, u1ID, 1001); err != nil {
		t.Fatal(err)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
		t.Fatalf("v2 retract real DC dispatch: %v", err)
	}
	u1RetractID := fmt.Sprintf("favorite.%d.v2", u1ID)
	u1RetractReceipt := readFavoriteDCReceipt(t, client, baseURL, token, u1RetractID)
	status, retract := readFavoriteAuthorityV2(t, authorityURL, authorityToken, favoriteProducer, u1RetractID)
	if status != 200 || retract.PredecessorEventID != u1AssertID || retract.SubjectRef != u1Source.SubjectRef ||
		retract.Event.SchemaVersion != 2 || retract.TechnicalReceipt.Offset != u1RetractReceipt.Offset ||
		u1RetractReceipt.Offset != u2Receipt.Offset+1 || retract.SourceEventHash != u1RetractReceipt.InputHash {
		t.Fatalf("v2 retract/order/hash mismatched: status=%d source=%+v dc=%+v", status, retract, u1RetractReceipt)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 3 {
		t.Fatalf("one fact per business version, no v1/v2 double send: %d", len(rows))
	}
	// A different body under the already accepted business key must be 409;
	// DC's receipt and offset stay exactly the same.
	var changed map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &changed); err != nil {
		t.Fatal(err)
	}
	changed["payload"].(map[string]any)["target_id"] = "forged"
	body, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", u1AssertID)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("DC accepted same Favorite key with different v2 body: %d", response.StatusCode)
	}
	if replay := readFavoriteDCReceipt(t, client, baseURL, token, u1AssertID); replay.Offset != u1Receipt.Offset || replay.InputHash != u1Receipt.InputHash {
		t.Fatalf("DC changed fixed receipt after conflict: %+v %+v", u1Receipt, replay)
	}
}

func readFavoriteAuthorityV2(t *testing.T, baseURL, token, producer, eventID string) (int, FavoriteAuthorityFactV2) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet,
		baseURL+"/internal/v2/favorite/facts/"+producer+"/"+eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var fact FavoriteAuthorityFactV2
	if response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(&fact); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, fact
}
