package server_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/favorite/rpc/internal/server"
	"sea-try-go/service/favorite/rpc/internal/svc"
	favoritepb "sea-try-go/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const fullFavoriteTarget = "article-shared-authority"
const fullFavoriteRevision = fullFavoriteTarget + ":r1"

// TestFavoriteArticleWorkerSharedFixture runs only behind the explicit full
// chain switch. The BTW acceptance script owns PG and DC; this test owns real
// Article/Favorite RPCs, business transactions, and RTW source processes.
func TestFavoriteArticleWorkerSharedFixture(t *testing.T) {
	if os.Getenv("FAVORITE_SHARED_FULL_CHAIN") != "1" {
		t.Skip("set FAVORITE_SHARED_FULL_CHAIN=1 through the BTW process acceptance")
	}
	for _, key := range []string{"FAVORITE_TEST_DSN", "FAVORITE_DC_URL", "FAVORITE_DC_TOKEN",
		"FAVORITE_AUTHORITY_BIN", "FAVORITE_DISPATCH_BIN", "FAVORITE_SHARED_READY_FILE", "FAVORITE_SHARED_RELEASE_FILE"} {
		if os.Getenv(key) == "" {
			t.Fatalf("full favorite fixture requires %s", key)
		}
	}
	readyPath, releasePath := os.Getenv("FAVORITE_SHARED_READY_FILE"), os.Getenv("FAVORITE_SHARED_RELEASE_FILE")
	if !filepath.IsAbs(readyPath) || !filepath.IsAbs(releasePath) || readyPath == releasePath {
		t.Fatal("full favorite handoff paths must be distinct absolute paths")
	}
	for _, path := range []string{readyPath, releasePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("full favorite handoff path already exists or cannot be checked: %s", path)
		}
	}
	ctx := context.Background()
	fixture := startFavoriteArticleFixture(t, os.Getenv("FAVORITE_TEST_DSN"))
	articleRPC, err := zrpc.NewClient(zrpc.NewDirectClientConf([]string{fixture.ArticleAddress}, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer articleRPC.Conn().Close()
	article := articleservice.NewArticleService(articleRPC)
	store, db := favoriteRPCStore(t)
	service := &svc.ServiceContext{FavoriteModel: store, UserRpc: activeUsers{}, ArticleRpc: article}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverRPC := grpc.NewServer()
	favoritepb.RegisterFavoriteServiceServer(serverRPC, server.NewFavoriteServiceServer(service))
	go func() { _ = serverRPC.Serve(listener) }()
	t.Cleanup(func() { serverRPC.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := favoritepb.NewFavoriteServiceClient(connection)
	folder, err := client.CreateFavoriteFolder(ctx, &favoritepb.CreateFavoriteFolderReq{UserId: 1001, Name: "full-chain-r1"})
	if err != nil || folder.FolderId <= 9007199254740991 {
		t.Fatalf("business folder did not receive a high ID: %+v %v", folder, err)
	}
	saved, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001,
		FolderId: folder.FolderId, TargetType: "article", TargetId: fullFavoriteTarget,
		Title: "forged draft", Cover: "forged-cover"})
	if err != nil || saved.FavoriteId <= 9007199254740991 {
		t.Fatalf("business favorite did not receive a high ID: %+v %v", saved, err)
	}
	item, err := store.FindFavoriteByFavoriteId(ctx, saved.FavoriteId)
	if err != nil || item.Title != "published r1" || item.TargetRevision == nil || *item.TargetRevision != fullFavoriteRevision {
		t.Fatalf("real public r1 did not become the business snapshot: %+v %v", item, err)
	}
	assertFavoriteRevision(t, db, saved.FavoriteId, 1, fullFavoriteRevision)
	assertID := fmt.Sprintf("favorite.%d.v1", saved.FavoriteId)
	var frozen model.FavoriteFactOutbox
	if err := db.Where("event_id = ?", assertID).Take(&frozen).Error; err != nil {
		t.Fatal(err)
	}
	ownerDSN := fullFavoriteSchemaDSN(t, db)
	endpoint := os.Getenv("FAVORITE_DC_URL")
	fullFavoriteDispatch(t, ownerDSN, endpoint, os.Getenv("FAVORITE_DC_TOKEN"))
	assertReceipt := fullFavoriteReceipt(t, endpoint, os.Getenv("FAVORITE_DC_TOKEN"), assertID)
	if assertReceipt.Offset < 1 || assertReceipt.InputHash == "" {
		t.Fatalf("DC did not commit the assert: %+v", assertReceipt)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fixture.ControlURL+"/publish-r2", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("Article did not publish r2: HTTP %d", response.StatusCode)
	}
	r2, err := article.GetArticle(ctx, &articleservice.GetArticleRequest{ArticleId: fullFavoriteTarget, PublicOnly: true})
	if err != nil || r2 == nil || r2.Article == nil || r2.Article.GetExtInfo()["published_revision_id"] != fullFavoriteTarget+":r2" {
		t.Fatalf("Article public pointer did not move to r2: %+v %v", r2, err)
	}
	if _, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001,
		FolderId: folder.FolderId, TargetType: "article", TargetId: fullFavoriteTarget}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("r2 publication changed old FavoriteId: %v", err)
	}
	var unchanged model.FavoriteFactOutbox
	if err := db.Where("event_id = ?", assertID).Take(&unchanged).Error; err != nil || unchanged.Payload != frozen.Payload {
		t.Fatalf("r2 changed the frozen v1 Outbox: %v", err)
	}
	if _, err := client.DeleteFavorite(ctx, &favoritepb.DeleteFavoriteReq{UserId: 1001, FavoriteId: saved.FavoriteId}); err != nil {
		t.Fatal(err)
	}
	assertFavoriteRevision(t, db, saved.FavoriteId, 2, fullFavoriteRevision)
	retractID := fmt.Sprintf("favorite.%d.v2", saved.FavoriteId)
	fullFavoriteDispatch(t, ownerDSN, endpoint, os.Getenv("FAVORITE_DC_TOKEN"))
	retractReceipt := fullFavoriteReceipt(t, endpoint, os.Getenv("FAVORITE_DC_TOKEN"), retractID)
	if retractReceipt.Offset != assertReceipt.Offset+1 {
		t.Fatalf("DC did not accept same-ID retract after assert: %+v %+v", assertReceipt, retractReceipt)
	}
	authorityURL, authorityToken := fullFavoriteAuthority(t, ownerDSN)
	for _, event := range []struct {
		id      string
		receipt model.FavoriteTechnicalReceipt
	}{{assertID, assertReceipt}, {retractID, retractReceipt}} {
		fact := fullFavoriteAuthorityFact(t, authorityURL, authorityToken, event.id)
		if fact.SourceEventHash != event.receipt.InputHash || fact.TechnicalReceipt.ReceiptID != event.receipt.ReceiptID ||
			fact.TechnicalReceipt.Offset != event.receipt.Offset {
			t.Fatalf("source and DC evidence differ for %s", event.id)
		}
		var payload struct {
			TargetID       string  `json:"target_id"`
			TargetRevision *string `json:"target_revision"`
			FavoriteID     string  `json:"favorite_id"`
		}
		if err := json.Unmarshal(fact.Event.Payload, &payload); err != nil || payload.TargetID != fullFavoriteTarget ||
			payload.TargetRevision == nil || *payload.TargetRevision != fullFavoriteRevision ||
			payload.FavoriteID != strconv.FormatInt(saved.FavoriteId, 10) {
			t.Fatalf("authority did not return the frozen business revision: %+v %v", payload, err)
		}
		if event.id == retractID && fact.PredecessorEventID != assertID {
			t.Fatalf("source retract has wrong predecessor: %s", fact.PredecessorEventID)
		}
	}
	ready := struct {
		AuthorityURL   string                         `json:"authority_url"`
		AuthorityToken string                         `json:"authority_token"`
		DCURL          string                         `json:"dc_url"`
		DCToken        string                         `json:"dc_token"`
		Producer       string                         `json:"producer"`
		AssertEventID  string                         `json:"assert_event_id"`
		RetractEventID string                         `json:"retract_event_id"`
		AssertReceipt  model.FavoriteTechnicalReceipt `json:"assert_receipt"`
		RetractReceipt model.FavoriteTechnicalReceipt `json:"retract_receipt"`
		TargetID       string                         `json:"target_id"`
		TargetRevision string                         `json:"target_revision"`
		ValueRef       string                         `json:"value_ref"`
	}{authorityURL, authorityToken, endpoint, os.Getenv("FAVORITE_DC_TOKEN"),
		"rtw.community.favorite", assertID, retractID, assertReceipt, retractReceipt,
		fullFavoriteTarget, fullFavoriteRevision, "article/" + fullFavoriteTarget + "/revision/" + fullFavoriteRevision}
	fullFavoriteReady(t, readyPath, ready)
	t.Logf("full favorite source ready: article=%s public_r1=%s public_r2=%s favorite_id=%d assert=%s retract=%s dc_offsets=%d,%d source_revision=%s",
		fullFavoriteTarget, fullFavoriteRevision, fullFavoriteTarget+":r2", saved.FavoriteId,
		assertID, retractID, assertReceipt.Offset, retractReceipt.Offset, fullFavoriteRevision)
	t.Cleanup(func() { _ = os.Remove(readyPath) })
	deadline := time.NewTimer(4 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("BTW worker did not release full favorite chain")
		case <-ticker.C:
			info, err := os.Stat(releasePath)
			if err == nil {
				if info.Mode().Perm() != 0600 {
					t.Fatalf("full favorite release mode is %o", info.Mode().Perm())
				}
				return
			}
			if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func fullFavoriteSchemaDSN(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil || schema == "" {
		t.Fatalf("favorite source schema: %q %v", schema, err)
	}
	address, err := url.Parse(os.Getenv("FAVORITE_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	return address.String()
}

func fullFavoriteDispatch(t *testing.T, dsn, dcURL, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Getenv("FAVORITE_DISPATCH_BIN"), "-once")
	command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL="+dsn,
		"DC_PLATFORM_EVENT_URL="+dcURL+"/v1/events", "DC_PLATFORM_SERVICE_TOKEN="+token)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent RTW dispatcher: %v\n%s", err, output)
	}
}

func fullFavoriteReceipt(t *testing.T, dcURL, token, eventID string) model.FavoriteTechnicalReceipt {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		dcURL+"/v1/events/rtw.community.favorite/"+eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var receipt model.FavoriteTechnicalReceipt
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&receipt) != nil || receipt.EventID != eventID {
		t.Fatalf("DC receipt unavailable: event=%s status=%d", eventID, response.StatusCode)
	}
	return receipt
}

func fullFavoriteAuthority(t *testing.T, dsn string) (string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, os.Getenv("FAVORITE_AUTHORITY_BIN"))
	command.Env = append(os.Environ(), "FAVORITE_DATABASE_URL="+dsn,
		"FAVORITE_AUTHORITY_LISTEN="+address, "FAVORITE_AUTHORITY_TOKEN="+token)
	logFile, err := os.OpenFile(filepath.Join(t.TempDir(), "favorite-authority.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait(); _ = logFile.Close() })
	base := "http://" + address
	for range 100 {
		request, _ := http.NewRequest(http.MethodGet, base+"/internal/v1/favorite/facts/rtw.community.favorite/readiness", nil)
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				return base, token
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("private RTW favorite authority process did not become ready")
	return "", ""
}

func fullFavoriteAuthorityFact(t *testing.T, baseURL, token, eventID string) model.FavoriteAuthorityFact {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		baseURL+"/internal/v1/favorite/facts/rtw.community.favorite/"+eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var fact model.FavoriteAuthorityFact
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&fact) != nil {
		t.Fatalf("private source fact unavailable: event=%s status=%d", eventID, response.StatusCode)
	}
	return fact
}

func fullFavoriteReady(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".favorite-full-ready-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file.Name(), path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("full favorite ready mode: %v %v", info, err)
	}
}
