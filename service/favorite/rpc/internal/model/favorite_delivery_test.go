package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type losingFavoriteSender struct {
	mu      sync.Mutex
	calls   int
	eventID string
}

type favoriteSenderFunc func(context.Context, FavoriteWireEvent, json.RawMessage) (FavoriteTechnicalReceipt, error)

func (fn favoriteSenderFunc) Send(ctx context.Context, event FavoriteWireEvent, raw json.RawMessage) (FavoriteTechnicalReceipt, error) {
	return fn(ctx, event, raw)
}

func (s *losingFavoriteSender) Send(_ context.Context, event FavoriteWireEvent, _ json.RawMessage) (FavoriteTechnicalReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.eventID != "" && s.eventID != event.EventID {
		return FavoriteTechnicalReceipt{}, errors.New("different event")
	}
	s.eventID = event.EventID
	if s.calls == 1 {
		return FavoriteTechnicalReceipt{}, errors.New("receiver committed but response was lost")
	}
	return FavoriteTechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: "receipt-1", InputHash: strings.Repeat("a", 64),
		Offset: 1, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func TestFavoriteDeliveryRetriesFixedEventAndSerializesDispatchers(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 651, UserId: 1001, Name: "delivery"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 652, FolderId: 651,
		UserId: 1001, TargetType: "article", TargetId: "article-77"}); err != nil {
		t.Fatal(err)
	}
	sender := &losingFavoriteSender{}
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); err == nil || sent {
		t.Fatalf("lost response was acknowledged: sent=%t err=%v", sent, err)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 1 || rows[0].Status != FavoriteFactFailed || rows[0].RetryCount != 1 {
		t.Fatalf("failed delivery state: %+v", rows)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sent, err := store.DispatchFavoriteFactOnce(ctx, sender)
			if err != nil {
				t.Errorf("retry: %v", err)
			}
			results <- sent
		}()
	}
	wg.Wait()
	close(results)
	var committed int
	for sent := range results {
		if sent {
			committed++
		}
	}
	rows = favoriteOutboxRows(t, store)
	if committed != 1 || sender.calls != 2 || len(rows) != 1 || rows[0].Status != FavoriteFactSent ||
		rows[0].TechnicalReceiptID != "receipt-1" || rows[0].TechnicalOffset != 1 || rows[0].DeliveredAt == nil {
		t.Fatalf("retry lost exactly-once local confirmation: committed=%d calls=%d rows=%+v", committed, sender.calls, rows)
	}
}

func TestFavoriteDeliveryRejectsMismatchedTechnicalReceipt(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 655, UserId: 1001, Name: "receipt"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 656, FolderId: 655,
		UserId: 1001, TargetType: "article", TargetId: "article-77"}); err != nil {
		t.Fatal(err)
	}
	wrong := favoriteSenderFunc(func(_ context.Context, event FavoriteWireEvent, _ json.RawMessage) (FavoriteTechnicalReceipt, error) {
		return FavoriteTechnicalReceipt{EventID: "another-event", Producer: event.Producer,
			TechnicalStatus: "accepted", ReceiptID: "receipt-2", InputHash: strings.Repeat("b", 64),
			Offset: 1, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	if sent, err := store.DispatchFavoriteFactOnce(ctx, wrong); sent || err == nil {
		t.Fatalf("different event receipt marked delivery: sent=%t err=%v", sent, err)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 1 || rows[0].Status != FavoriteFactFailed || rows[0].DeliveredAt != nil ||
		rows[0].TechnicalReceiptID != "" || rows[0].RetryCount != 1 {
		t.Fatalf("mismatched receipt changed source confirmation: %+v", rows)
	}
}

func TestFavoriteDeliveryCannotSkipLockedAssertToRetract(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 657, UserId: 1001, Name: "ordered"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 658, FolderId: 657,
		UserId: 1001, TargetType: "article", TargetId: "article-77"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 658, 1001); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	sender := favoriteSenderFunc(func(_ context.Context, event FavoriteWireEvent, _ json.RawMessage) (FavoriteTechnicalReceipt, error) {
		if event.AggregateVersion == 1 {
			close(entered)
			<-release
		}
		return FavoriteTechnicalReceipt{EventID: event.EventID, Producer: event.Producer,
			TechnicalStatus: "accepted", ReceiptID: event.EventID, InputHash: strings.Repeat("a", 64),
			Offset: event.AggregateVersion, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	first := make(chan error, 1)
	go func() {
		sent, err := store.DispatchFavoriteFactOnce(ctx, sender)
		if err == nil && !sent {
			err = errors.New("first dispatcher did not send assert")
		}
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first dispatcher did not lock assert")
	}
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); sent || err != nil {
		t.Fatalf("retract bypassed locked assert: sent=%t err=%v", sent, err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if sent, err := store.DispatchFavoriteFactOnce(ctx, sender); err != nil || !sent {
		t.Fatalf("retract not dispatched after assert receipt: sent=%t err=%v", sent, err)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 2 || rows[0].TechnicalOffset != 1 || rows[1].TechnicalOffset != 2 {
		t.Fatalf("assert/retract technical order: %+v", rows)
	}
}

func TestFavoriteDeliveryRealDataCenterTechnicalReceipt(t *testing.T) {
	baseURL, token := os.Getenv("FAVORITE_DC_URL"), os.Getenv("FAVORITE_DC_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set FAVORITE_DC_URL/TOKEN via acceptance.sh for real cmd/platform")
	}
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 661, UserId: 1001, Name: "dc"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 662, FolderId: 661,
		UserId: 1001, TargetType: "article", TargetId: "article-77"}); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	var once sync.Once
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL+r.URL.Path, r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		req.Header = r.Header.Clone()
		response, err := client.Do(req)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		lost := false
		once.Do(func() { lost = true })
		if lost && response.StatusCode == http.StatusCreated {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	if err := runFavoriteDispatchProcess(t, store, proxy.URL+"/v1/events", token); err == nil {
		t.Fatal("proxy lost receipt but dispatcher succeeded")
	}
	firstReceipt := readFavoriteDCReceipt(t, client, baseURL, token, "favorite.662.v1")
	if firstReceipt.Offset != 1 {
		t.Fatalf("DC did not durably accept before response loss: %+v", firstReceipt)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 1 || rows[0].Status != FavoriteFactFailed || rows[0].DeliveredAt != nil {
		t.Fatalf("RTW marked an uncertain response as delivered: %+v", rows)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("event_id = ?", "favorite.662.v1").
		Update("payload", `{"forged":true}`).Error; err == nil {
		t.Fatal("RTW outbox envelope was mutable after uncertain DC acceptance")
	}
	var altered map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &altered); err != nil {
		t.Fatal(err)
	}
	altered["payload"].(map[string]any)["target_id"] = "changed-after-commit"
	badBody, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	badRequest, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/events", strings.NewReader(string(badBody)))
	badRequest.Header.Set("Authorization", "Bearer "+token)
	badRequest.Header.Set("Content-Type", "application/json")
	badRequest.Header.Set("Idempotency-Key", "favorite.662.v1")
	badResponse, err := client.Do(badRequest)
	if err != nil {
		t.Fatal(err)
	}
	badResponse.Body.Close()
	if badResponse.StatusCode != http.StatusConflict {
		t.Fatalf("DC accepted same event ID with different payload: %d", badResponse.StatusCode)
	}
	if got := readFavoriteDCReceipt(t, client, baseURL, token, "favorite.662.v1"); got.InputHash != firstReceipt.InputHash || got.Offset != 1 {
		t.Fatalf("conflicting replay changed DC receipt: before=%+v after=%+v", firstReceipt, got)
	}
	if err := runFavoriteDispatchProcess(t, store, baseURL+"/v1/events", token); err != nil {
		t.Fatalf("fixed event replay failed: %v", err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 662, 1001); err != nil {
		t.Fatal(err)
	}
	if err := runFavoriteDispatchProcess(t, store, baseURL+"/v1/events", token); err != nil {
		t.Fatalf("retract delivery failed: %v", err)
	}
	rows = favoriteOutboxRows(t, store)
	if len(rows) != 2 || rows[0].TechnicalOffset != 1 || rows[1].TechnicalOffset != 2 ||
		rows[0].TechnicalReceiptID == rows[1].TechnicalReceiptID || rows[1].Status != FavoriteFactSent {
		t.Fatalf("DC receipts and local outbox diverged: %+v", rows)
	}
	request, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/event-consumers/favorite-test/events?producer=rtw.community.favorite&limit=10", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var batch struct {
		FromOffset int64             `json:"from_offset"`
		ToOffset   int64             `json:"to_offset"`
		Events     []json.RawMessage `json:"events"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&batch) != nil ||
		batch.FromOffset != 1 || batch.ToOffset != 2 || len(batch.Events) != 2 {
		t.Fatalf("DC batch after source replay: status=%d batch=%+v", response.StatusCode, batch)
	}
}

func runFavoriteDispatchProcess(t *testing.T, store *FavoriteModel, endpoint, token string) error {
	t.Helper()
	bin := os.Getenv("FAVORITE_DISPATCH_BIN")
	if bin == "" {
		t.Fatal("FAVORITE_DISPATCH_BIN required for real DC acceptance")
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
	command := exec.Command(bin, "-once")
	command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL="+address.String(),
		"DC_PLATFORM_EVENT_URL="+endpoint, "DC_PLATFORM_SERVICE_TOKEN="+token)
	output, err := command.CombinedOutput()
	if err != nil && len(output) > 0 {
		t.Logf("favorite dispatch process failed: %s", output)
	}
	return err
}

func readFavoriteDCReceipt(t *testing.T, client *http.Client, baseURL, token, eventID string) FavoriteTechnicalReceipt {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/events/rtw.community.favorite/"+eventID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var receipt FavoriteTechnicalReceipt
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&receipt) != nil || receipt.EventID != eventID {
		t.Fatalf("DC receipt status=%d receipt=%+v", response.StatusCode, receipt)
	}
	return receipt
}
