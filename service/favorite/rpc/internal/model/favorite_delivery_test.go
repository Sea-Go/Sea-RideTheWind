package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

func TestFavoriteDeliveryRealDataCenterSnowflakeIDs(t *testing.T) {
	baseURL, token := os.Getenv("FAVORITE_DC_URL"), os.Getenv("FAVORITE_DC_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set FAVORITE_DC_URL/TOKEN via acceptance-dc.sh for real cmd/platform")
	}
	store := favoriteFactStore(t)
	ctx := context.Background()
	const folderID, favoriteID int64 = 9007199254740993, 9007199254740995
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: folderID, UserId: 1001, Name: "snowflake"}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: favoriteID, FolderId: folderID,
		UserId: 1001, TargetType: "article", TargetId: "article-snowflake"}); err != nil {
		t.Fatal(err)
	}
	rows := favoriteOutboxRows(t, store)
	var frozen favoriteEvent
	if len(rows) != 1 || json.Unmarshal([]byte(rows[0].Payload), &frozen) != nil ||
		frozen.Payload.FavoriteID != "9007199254740995" || frozen.Payload.FolderID != "9007199254740993" ||
		frozen.AggregateID != frozen.Payload.FavoriteID || frozen.Payload.SourceRef != "rtw.favorite/9007199254740995" {
		t.Fatalf("Snowflake ID lost exact decimal form: rows=%+v event=%+v", rows, frozen)
	}
	endpoint := baseURL + "/v1/events"
	authorityURL, authorityToken := startFavoriteAuthorityProcess(t, store)
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, "rtw.community.favorite", "favorite.9007199254740995.v1"); status != 404 {
		t.Fatalf("undelivered source became authoritative: %d", status)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
		t.Fatalf("real DC rejected assert with Snowflake-range IDs: %v", err)
	}
	first := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, baseURL, token,
		"favorite.9007199254740995.v1")
	if first.Offset < 1 {
		t.Fatalf("invalid assert receipt: %+v", first)
	}
	status, acceptedAssert := readFavoriteAuthority(t, authorityURL, authorityToken,
		"rtw.community.favorite", "favorite.9007199254740995.v1")
	if status != 200 || acceptedAssert.SubjectRef.SubjectID != "1001" ||
		acceptedAssert.Event.EventID != first.EventID || acceptedAssert.SourceEventHash != first.InputHash ||
		acceptedAssert.TechnicalReceipt.InputHash != first.InputHash ||
		acceptedAssert.TechnicalReceipt.ReceiptID != first.ReceiptID ||
		acceptedAssert.TechnicalReceipt.Offset != first.Offset ||
		acceptedAssert.TechnicalReceipt.ReceivedAt != first.ReceivedAt {
		t.Fatalf("source and DC assert receipt diverged: status=%d source=%+v DC=%+v", status, acceptedAssert, first)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, "wrong-token", "rtw.community.favorite", first.EventID); status != 401 {
		t.Fatalf("untrusted caller read source: %d", status)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, "other.producer", first.EventID); status != 404 {
		t.Fatalf("wrong producer read source: %d", status)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, "rtw.community.favorite", "favorite.unknown.v1"); status != 404 {
		t.Fatalf("unknown event read source: %d", status)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, favoriteID, 1001); err != nil {
		t.Fatal(err)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, "rtw.community.favorite", "favorite.9007199254740995.v2"); status != 404 {
		t.Fatalf("undelivered retract became authoritative: %d", status)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
		t.Fatalf("real DC rejected retract with Snowflake-range IDs: %v", err)
	}
	second := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, baseURL, token,
		"favorite.9007199254740995.v2")
	if second.Offset != first.Offset+1 || second.InputHash == first.InputHash {
		t.Fatalf("retract receipt not a new fixed event: assert=%+v retract=%+v", first, second)
	}
	status, acceptedRetract := readFavoriteAuthority(t, authorityURL, authorityToken,
		"rtw.community.favorite", "favorite.9007199254740995.v2")
	if status != 200 || acceptedRetract.PredecessorEventID != first.EventID ||
		acceptedRetract.SubjectRef != acceptedAssert.SubjectRef ||
		acceptedRetract.SourceEventHash != second.InputHash ||
		acceptedRetract.TechnicalReceipt.Offset != second.Offset ||
		acceptedRetract.TechnicalReceipt.ReceivedAt != second.ReceivedAt {
		t.Fatalf("source and DC retract receipt diverged: status=%d source=%+v DC=%+v", status, acceptedRetract, second)
	}
	if replayStatus, replay := readFavoriteAuthority(t, authorityURL, authorityToken,
		"rtw.community.favorite", second.EventID); replayStatus != 200 || replay.SourceEventHash != acceptedRetract.SourceEventHash ||
		replay.TechnicalReceipt.ReceiptID != acceptedRetract.TechnicalReceipt.ReceiptID {
		t.Fatalf("authority read replay changed: status=%d replay=%+v", replayStatus, replay)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
		t.Fatalf("fixed replay failed: %v", err)
	}
	if err := store.conn.Model(&FavoriteFactOutbox{}).Where("event_id = ?", second.EventID).
		Update("technical_input_hash", strings.Repeat("b", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken, "rtw.community.favorite", second.EventID); status != 404 {
		t.Fatalf("tampered receipt hash read source: %d", status)
	}
	for _, tc := range []struct {
		id     int64
		change func(*favoriteEvent)
	}{
		{9007199254741011, func(event *favoriteEvent) { event.Payload.Subject.SubjectID = "1002" }},
		{9007199254741012, func(event *favoriteEvent) { event.Payload.Subject.AuthorityID = "old.identity" }},
	} {
		item := FavoriteItem{FavoriteId: tc.id, FolderId: folderID, UserId: 1001,
			TargetType: "article", TargetId: "article-invalid-source-" + fmt.Sprint(tc.id)}
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
		// Deliberately inconsistent source fixture: a hash-shaped receipt
		// alone must not authorize a different business owner or authority.
		insertAcceptedAuthorityFixture(t, store, row, tc.id)
		if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken,
			"rtw.community.favorite", row.EventID); status != 404 {
			t.Fatalf("cross-user or old-source fixture became authoritative: event=%s status=%d", row.EventID, status)
		}
	}
	// A pre-existing unsafe numeric Outbox row is quarantined without
	// changing its event ID/body, so a later valid event can reach real DC.
	const blockedID, followingID int64 = 9007199254741023, 9007199254741025
	blockedItem := FavoriteItem{FavoriteId: blockedID, FolderId: folderID, UserId: 1001,
		TargetType: "article", TargetId: "article-legacy-number"}
	if err := store.conn.Create(&blockedItem).Error; err != nil {
		t.Fatal(err)
	}
	blocked, err := favoriteOutbox(blockedItem, 1, "assert", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	blocked.Payload = strings.ReplaceAll(blocked.Payload,
		`"favorite_id":"9007199254741023"`, `"favorite_id":9007199254741023`)
	blocked.Payload = strings.ReplaceAll(blocked.Payload,
		`"folder_id":"9007199254740993"`, `"folder_id":9007199254740993`)
	if err := store.conn.Create(&blocked).Error; err != nil {
		t.Fatal(err)
	}
	var frozenBlocked FavoriteFactOutbox
	if err := store.conn.Where("event_id = ?", blocked.EventID).Take(&frozenBlocked).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: followingID, FolderId: folderID,
		UserId: 1001, TargetType: "article", TargetId: "article-after-legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err == nil {
		t.Fatal("unsafe legacy event was dispatched to DC")
	}
	var quarantined FavoriteFactOutbox
	if err := store.conn.Where("event_id = ?", blocked.EventID).Take(&quarantined).Error; err != nil ||
		quarantined.Status != FavoriteFactBlocked || quarantined.Payload != frozenBlocked.Payload {
		t.Fatalf("unsafe frozen event was not preserved and blocked: row=%+v err=%v", quarantined, err)
	}
	if status, _ := readFavoriteAuthority(t, authorityURL, authorityToken,
		"rtw.community.favorite", blocked.EventID); status != 404 {
		t.Fatalf("blocked legacy source was published: %d", status)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, token); err != nil {
		t.Fatalf("blocked legacy row starved valid DC successor: %v", err)
	}
	following := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, baseURL, token,
		"favorite.9007199254741025.v1")
	if following.Offset != second.Offset+1 {
		t.Fatalf("valid DC successor offset after blocked row: %+v", following)
	}
}

func TestFavoriteAuthorityProcessDefaultClosed(t *testing.T) {
	bin := os.Getenv("FAVORITE_AUTHORITY_BIN")
	if bin == "" {
		t.Skip("build fact-authority via acceptance-dc.sh")
	}
	command := exec.Command(bin)
	command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL=",
		"FAVORITE_AUTHORITY_LISTEN=", "FAVORITE_AUTHORITY_TOKEN=")
	if err := command.Run(); err == nil {
		t.Fatal("unconfigured source authority process stayed available")
	}
}

// TestFavoriteDeliverySharedAuthorityFixture is a test-only rendezvous for a
// separate BTW process. The ready file is mode 0600 and is never logged; the
// source HTTP process, DC, and isolated PG schema remain alive until release.
func TestFavoriteDeliverySharedAuthorityFixture(t *testing.T) {
	readyPath, releasePath := os.Getenv("FAVORITE_SHARED_READY_FILE"), os.Getenv("FAVORITE_SHARED_RELEASE_FILE")
	if readyPath == "" && releasePath == "" {
		t.Skip("set both FAVORITE_SHARED_READY_FILE and FAVORITE_SHARED_RELEASE_FILE for cross-repo test")
	}
	if !filepath.IsAbs(readyPath) || !filepath.IsAbs(releasePath) || readyPath == releasePath ||
		os.Getenv("FAVORITE_DC_URL") == "" || os.Getenv("FAVORITE_DC_TOKEN") == "" ||
		os.Getenv("FAVORITE_AUTHORITY_BIN") == "" || os.Getenv("FAVORITE_DISPATCH_BIN") == "" {
		t.Fatal("shared favorite acceptance configuration incomplete")
	}
	for _, path := range []string{readyPath, releasePath} {
		if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
			t.Fatalf("shared favorite handoff path must be new: %s", path)
		}
	}
	if os.Getenv("FAVORITE_SHARED_FULL_CHAIN") == "1" {
		// The existing BTW script starts this package's shared window. Route
		// only the explicit test mode to the server-package fixture so its
		// business event comes from real Article and Favorite network RPCs.
		root, err := filepath.Abs("../../../../..")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, "go", "test", "-mod=readonly", "-race", "-count=1", "-v",
			"-run", "^TestFavoriteArticleWorkerSharedFixture$", "./service/favorite/rpc/internal/server")
		command.Dir = root
		command.Env = os.Environ()
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("full favorite shared fixture: %v\n%s", err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, "full favorite source ready:") ||
				strings.Contains(line, "PASS: TestFavoriteArticleWorkerSharedFixture") {
				t.Log(line)
			}
		}
		return
	}
	store := favoriteFactStore(t)
	authorityURL, authorityToken := startFavoriteAuthorityProcess(t, store)
	const folderID, favoriteID int64 = 9007199254741991, 9007199254741993
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: folderID, UserId: 1001, Name: "shared-authority"}); err != nil {
		t.Fatal(err)
	}
	approvedRevision := "article-shared-authority:r1"
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: favoriteID, FolderId: folderID,
		UserId: 1001, TargetType: "article", TargetId: "article-shared-authority", TargetRevision: &approvedRevision}); err != nil {
		t.Fatal(err)
	}
	endpoint, dcURL, dcToken := os.Getenv("FAVORITE_DC_URL")+"/v1/events", os.Getenv("FAVORITE_DC_URL"), os.Getenv("FAVORITE_DC_TOKEN")
	if err := runFavoriteDispatchProcess(t, store, endpoint, dcToken); err != nil {
		t.Fatalf("shared assert dispatch: %v", err)
	}
	assertID := fmt.Sprintf("favorite.%d.v1", favoriteID)
	assertReceipt := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, dcURL, dcToken, assertID)
	if status, fact := readFavoriteAuthority(t, authorityURL, authorityToken, favoriteProducer, assertID); status != 200 || fact.SourceEventHash != assertReceipt.InputHash ||
		fact.TechnicalReceipt.ReceiptID != assertReceipt.ReceiptID {
		t.Fatalf("shared assert source not ready: status=%d fact=%+v", status, fact)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, favoriteID, 1001); err != nil {
		t.Fatal(err)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, dcToken); err != nil {
		t.Fatalf("shared retract dispatch: %v", err)
	}
	retractID := fmt.Sprintf("favorite.%d.v2", favoriteID)
	retractReceipt := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, dcURL, dcToken, retractID)
	if status, fact := readFavoriteAuthority(t, authorityURL, authorityToken, favoriteProducer, retractID); status != 200 || fact.PredecessorEventID != assertID ||
		fact.SourceEventHash != retractReceipt.InputHash ||
		fact.TechnicalReceipt.ReceiptID != retractReceipt.ReceiptID ||
		retractReceipt.Offset != assertReceipt.Offset+1 {
		t.Fatalf("shared retract source not ready: status=%d fact=%+v", status, fact)
	}
	ready := struct {
		AuthorityURL   string                   `json:"authority_url"`
		AuthorityToken string                   `json:"authority_token"`
		DCURL          string                   `json:"dc_url"`
		DCToken        string                   `json:"dc_token"`
		Producer       string                   `json:"producer"`
		AssertEventID  string                   `json:"assert_event_id"`
		RetractEventID string                   `json:"retract_event_id"`
		AssertReceipt  FavoriteTechnicalReceipt `json:"assert_receipt"`
		RetractReceipt FavoriteTechnicalReceipt `json:"retract_receipt"`
	}{authorityURL, authorityToken, dcURL, dcToken, favoriteProducer, assertID, retractID,
		assertReceipt, retractReceipt}
	if err := writeFavoriteSharedReady(readyPath, ready); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(readyPath) })
	deadline := time.NewTimer(4 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("shared favorite acceptance release timed out")
		case <-ticker.C:
			if _, err := os.Stat(releasePath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

// TestFavoriteDeliveryTwoUsersSharedAuthorityFixture exposes three real RTW
// favorite facts in one DC producer stream: u1 assert, u2 assert, u1 retract.
// The cross-repo caller owns the disposable DC and releases this test window.
func TestFavoriteDeliveryTwoUsersSharedAuthorityFixture(t *testing.T) {
	readyPath, releasePath := os.Getenv("FAVORITE_TWO_READY_FILE"), os.Getenv("FAVORITE_TWO_RELEASE_FILE")
	if readyPath == "" && releasePath == "" {
		t.Skip("set both FAVORITE_TWO_READY_FILE and FAVORITE_TWO_RELEASE_FILE for cross-repo test")
	}
	if !filepath.IsAbs(readyPath) || !filepath.IsAbs(releasePath) || readyPath == releasePath ||
		os.Getenv("FAVORITE_DC_URL") == "" || os.Getenv("FAVORITE_DC_TOKEN") == "" ||
		os.Getenv("FAVORITE_AUTHORITY_BIN") == "" || os.Getenv("FAVORITE_DISPATCH_BIN") == "" {
		t.Fatal("two-user shared favorite acceptance configuration incomplete")
	}
	for _, path := range []string{readyPath, releasePath} {
		if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
			t.Fatalf("two-user shared handoff path must be new: %s", path)
		}
	}
	store := favoriteFactStore(t)
	mode := os.Getenv("FAVORITE_TWO_SUBJECTREF_MODE")
	if mode != "" && mode != "v2-mixed" {
		t.Fatalf("unknown two-user SubjectRef test mode %q", mode)
	}
	v2Writer := NewFavoriteModel(store.conn, WithSubjectRefV2Facts())
	authorityURL, authorityToken := startFavoriteAuthorityProcess(t, store)
	ctx := context.Background()
	endpoint, dcURL, dcToken := os.Getenv("FAVORITE_DC_URL")+"/v1/events", os.Getenv("FAVORITE_DC_URL"), os.Getenv("FAVORITE_DC_TOKEN")
	const u1Folder, u1Favorite, u2Folder, u2Favorite int64 = 9007199254741991, 9007199254741993, 9007199254742991, 9007199254742993
	for _, item := range []struct {
		folder, favorite, user int64
		target                 string
	}{
		{u1Folder, u1Favorite, 1001, "article-u1"},
		{u2Folder, u2Favorite, 1002, "article-u2"},
	} {
		if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: item.folder, UserId: item.user, Name: "coverage-two-user"}); err != nil {
			t.Fatal(err)
		}
		revision := item.target + ":r1"
		writer := store
		if mode == "v2-mixed" && item.user == 1001 {
			writer = v2Writer
		}
		if err := writer.InsertFavorite(ctx, &FavoriteItem{FavoriteId: item.favorite, FolderId: item.folder,
			UserId: item.user, TargetType: "article", TargetId: item.target, TargetRevision: &revision}); err != nil {
			t.Fatal(err)
		}
		if err := runFavoriteDispatchProcess(t, store, endpoint, dcToken); err != nil {
			t.Fatalf("two-user assert dispatch: %v", err)
		}
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, u1Favorite, 1001); err != nil {
		t.Fatal(err)
	}
	if err := runFavoriteDispatchProcess(t, store, endpoint, dcToken); err != nil {
		t.Fatalf("u1 retract dispatch: %v", err)
	}
	ids := []string{
		fmt.Sprintf("favorite.%d.v1", u1Favorite),
		fmt.Sprintf("favorite.%d.v1", u2Favorite),
		fmt.Sprintf("favorite.%d.v2", u1Favorite),
	}
	userIDs := []string{"1001", "1002", "1001"}
	schemaVersions := []int{1, 1, 1}
	if mode == "v2-mixed" {
		schemaVersions = []int{2, 1, 2}
	}
	receipts := make([]FavoriteTechnicalReceipt, 0, len(ids))
	for i, id := range ids {
		receipt := readFavoriteDCReceipt(t, &http.Client{Timeout: 3 * time.Second}, dcURL, dcToken, id)
		status := 0
		sourceHash, sourceReceipt, sourceUID, predecessor := "", FavoriteTechnicalReceipt{}, "", ""
		if schemaVersions[i] == 2 {
			var fact FavoriteAuthorityFactV2
			status, fact = readFavoriteAuthorityV2(t, authorityURL, authorityToken, favoriteProducer, id)
			sourceHash, sourceReceipt, sourceUID, predecessor = fact.SourceEventHash,
				fact.TechnicalReceipt, fact.SubjectRef.SubjectID, fact.PredecessorEventID
		} else {
			var fact FavoriteAuthorityFact
			status, fact = readFavoriteAuthority(t, authorityURL, authorityToken, favoriteProducer, id)
			sourceHash, sourceReceipt, sourceUID, predecessor = fact.SourceEventHash,
				fact.TechnicalReceipt, fact.SubjectRef.SubjectID, fact.PredecessorEventID
		}
		if receipt.Offset != int64(i+1) || status != 200 || sourceHash != receipt.InputHash ||
			sourceReceipt.ReceiptID != receipt.ReceiptID || sourceUID != userIDs[i] {
			t.Fatalf("two-user source item %d differs: offset=%d status=%d subject=%s", i, receipt.Offset, status, sourceUID)
		}
		if i == 2 && predecessor != ids[0] {
			t.Fatalf("u1 retract predecessor %q differs", predecessor)
		}
		receipts = append(receipts, receipt)
	}
	ready := struct {
		AuthorityURL   string                     `json:"authority_url"`
		AuthorityToken string                     `json:"authority_token"`
		DCURL          string                     `json:"dc_url"`
		DCToken        string                     `json:"dc_token"`
		Producer       string                     `json:"producer"`
		EventIDs       []string                   `json:"event_ids"`
		Receipts       []FavoriteTechnicalReceipt `json:"receipts"`
		SchemaVersions []int                      `json:"schema_versions,omitempty"`
	}{authorityURL, authorityToken, dcURL, dcToken, favoriteProducer, ids, receipts, nil}
	if mode == "v2-mixed" {
		ready.SchemaVersions = schemaVersions
	}
	if err := writeFavoriteSharedReady(readyPath, ready); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(readyPath) })
	deadline := time.NewTimer(4 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("two-user shared favorite acceptance release timed out")
		case <-ticker.C:
			if _, err := os.Stat(releasePath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func writeFavoriteSharedReady(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".favorite-ready-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}

func startFavoriteAuthorityProcess(t *testing.T, store *FavoriteModel) (string, string) {
	t.Helper()
	bin := os.Getenv("FAVORITE_AUTHORITY_BIN")
	if bin == "" {
		t.Fatal("FAVORITE_AUTHORITY_BIN required for real source HTTP acceptance")
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	listener.Close()
	const authorityToken = "test-only-rtw-favorite-source-token-123456"
	command := exec.Command(bin)
	command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL="+address.String(),
		"FAVORITE_AUTHORITY_LISTEN="+listen, "FAVORITE_AUTHORITY_TOKEN="+authorityToken)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	baseURL := "http://" + listen
	for i := 0; i < 100; i++ {
		request, _ := http.NewRequest(http.MethodGet, baseURL+"/internal/v1/favorite/facts/rtw.community.favorite/readiness", nil)
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 401 {
				return baseURL, authorityToken
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("favorite source HTTP did not become ready")
	return "", ""
}

func readFavoriteAuthority(t *testing.T, baseURL, token, producer, eventID string) (int, FavoriteAuthorityFact) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+"/internal/v1/favorite/facts/"+producer+"/"+eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var fact FavoriteAuthorityFact
	if response.StatusCode == 200 {
		if err := json.NewDecoder(response.Body).Decode(&fact); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, fact
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
